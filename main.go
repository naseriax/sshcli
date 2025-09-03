package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"sshcli/pkgs/sftp_ui"
	"strconv"
	"strings"
	"syscall"

	_ "modernc.org/sqlite"

	"github.com/manifoldco/promptui"
	"golang.org/x/term"
)

const (
	red         = "\033[31m"
	green       = "\033[32m"
	reset       = "\033[0m"
	pinkonbrown = "\033[38;2;255;82;197;48;2;155;106;0m"
	yellow      = "\033[33m"
	blue        = "\033[34m"
	magenta     = "\033[35m"
	cyan        = "\033[36m"
	orange      = "\033[38;2;255;165;0m"
)

var CompileTime = ""
var passAuthSupported = true
var key []byte
var db *sql.DB
var folderIcon = "🗂️"
var sshIcon = "🌐"
var consoleIcon = "📟"

// SSHConfig represents the configuration for an SSH host entry
type SSHConfig struct {
	Host          string
	HostName      string
	User          string
	Port          string
	Proxy         string
	ForwardSocket string
	IdentityFile  string
	Password      string
	Folder        string
}

type FileNode struct {
	Name     string
	IsDir    bool
	Children []*FileNode
	Parent   *FileNode
}

// ConsoleConfig represents the configuration for the Console host entry
type ConsoleConfig struct {
	Host     string
	BaudRate string
	Device   string
	Parity   string
	StopBit  string
	DataBits string
	Folder   string
}

type ConsoleConfigs []ConsoleConfig

// ##################### LEGACY functions to be removed #####################

// lagace password management
var keyFile = "encryption.key"
var dataFile = "passwords.json"
var hostPasswords HostPasswords

type HostPasswords []HostPassword
type HostPassword struct {
	Host     string `json:"host"`
	Password string `json:"password"`
}

func readPassFile() error {

	if _, err := os.Stat(dataFile); err != nil {

		return fmt.Errorf(" [!] %sIt seems no password database file was created before%s", green, reset)
	}

	data, err := os.ReadFile(dataFile)
	if err != nil {
		return fmt.Errorf("error reading the Password database file: %v", err)
	}

	if len(data) != 0 {
		err = json.Unmarshal(data, &hostPasswords)
		if err != nil {
			fmt.Printf("error unmarshalling JSON: %v\n", err)
			os.Exit(1)
		}
	}

	return nil
}

func loadCredentials() error {
	rows, err := db.Query("SELECT host, password FROM sshprofiles WHERE password IS NOT NULL ORDER BY host")
	if err != nil {
		return fmt.Errorf("failed to query sshprofiles: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var hp HostPassword
		if err := rows.Scan(&hp.Host, &hp.Password); err != nil {
			return fmt.Errorf("failed to scan credential row: %w", err)
		}
		hostPasswords = append(hostPasswords, hp)
	}

	// Check for errors that may have occurred during iteration.
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error during credential rows iteration: %w", err)
	}
	if len(hostPasswords) == 0 {
		err := readPassFile()
		if err != nil {
			return fmt.Errorf("error reading password file: %v", err)
		}
		if err := PushPasswordsToDB(); err != nil {
			return fmt.Errorf("error pushing passwords to database: %v", err)
		}
		fmt.Println("✅ Data Migration Event: Filled the password db from the passwords.json file. The json file can be removed.")
	}

	return nil
}

func PushPasswordsToDB() error {

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	stmt, err := tx.Prepare("INSERT INTO sshprofiles(host, password) VALUES(?, ?) ON CONFLICT(host) DO UPDATE SET password = excluded.password;")
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to prepare statement: %w", err)
	}

	defer stmt.Close()

	for _, s := range hostPasswords {
		if _, err := stmt.Exec(s.Host, s.Password); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to insert '%s': %w", s.Host, err)
		}
	}

	return tx.Commit()
}

// Legacy folder management
type Folders []struct {
	Name     string   `json:"name"`
	Profiles []string `json:"profiles"`
}

func readFolderDb(folderdb string) (Folders, error) {

	folder := Folders{}

	if _, err := os.Stat(folderdb); err != nil {
		return folder, fmt.Errorf("folderdb.json not found. details:%w", err)
	}

	data, err := os.ReadFile(folderdb)
	if err != nil {
		return folder, fmt.Errorf("error reading the folder database file: %v", err)
	}

	if len(data) != 0 {
		err = json.Unmarshal(data, &folder)
		if err != nil {
			fmt.Printf("error unmarshalling folderdb json: %v\n", err)
			return folder, fmt.Errorf("error unmarshalling folder JSON: %v", err)
		}
	}

	return folder, nil
}

// check if any folder configurations exist in the sqlite db
func isFolderConfiguredinDB() bool {

	output := false

	query := "SELECT DISTINCT folder FROM sshprofiles WHERE folder is not NULL"
	rows, err := db.Query(query)
	if err != nil {
		log.Println("error querying folders:", err)
		return output
	}
	defer rows.Close()

	for rows.Next() {
		var folder string
		if err := rows.Scan(&folder); err != nil {
			log.Println("error reading folder:", err)
			continue
		}
		output = true
		return output
	}
	if err := rows.Err(); err != nil {
		log.Printf("error iterating over folders: %v", err.Error())
		return output
	}
	return output
}

//############################################################################

// initDB opens a connection to the SQLite database and ensures all necessary
// It's designed to be idempotent.
func initDB(filepath string) error {
	var err error
	// Open the database file. It will be created if it doesn't exist.
	db, err = sql.Open("sqlite", filepath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// A map of table names to their CREATE statements for clarity and organization.
	tables := map[string]string{
		"sshprofiles": `
		CREATE TABLE IF NOT EXISTS sshprofiles (
			host TEXT PRIMARY KEY,
			password TEXT,
			note TEXT,
			folder TEXT
		);`,

		"encryption_key": `
		CREATE TABLE IF NOT EXISTS encryption_key (
			id INTEGER PRIMARY KEY CHECK (id = 1), -- Ensures only one row can exist
			key BLOB NOT NULL
		);`,

		"console_profiles": `
		CREATE TABLE IF NOT EXISTS console_profiles (
			host TEXT PRIMARY KEY,
			baud_rate INTEGER NOT NULL,
			device TEXT NOT NULL,
			parity TEXT,
			stop_bit TEXT,
			data_bits INTEGER,
			folder TEXT
		);`,
	}

	// Execute each CREATE TABLE statement.
	for name, stmt := range tables {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to create table '%s': %w", name, err)
		}
	}

	return nil
}

func doConfigBackup(mode string) {

	if mode == "all" || mode == "config" {

		configFilePath, err := setupFilesFolders()
		if err != nil {
			log.Println("failed to get the config file path")
		}

		backupConfigFilePath := configFilePath + "_lastbackup"
		srcConfigFile, err := os.Open(configFilePath)
		if err != nil {
			log.Println(err)
		}

		defer srcConfigFile.Close()

		dstConfigFile, err := os.Create(backupConfigFilePath)
		if err != nil {
			log.Println(err)
		}
		defer dstConfigFile.Close()

		_, err = io.Copy(dstConfigFile, srcConfigFile)
		if err != nil {
			log.Println(err)
		}
	}

	if mode == "all" || mode == "db" {

		homeDir, _ := os.UserHomeDir()
		databaseFile := filepath.Join(homeDir, ".ssh", "sshcli.db")
		backupDatabaseFile := databaseFile + "_lastbackup"
		srcDatabase, err := os.Open(databaseFile)
		if err != nil {
			log.Println(err)
		}
		defer srcDatabase.Close()

		dstDatabse, err := os.Create(backupDatabaseFile)
		if err != nil {
			log.Println(err)
		}
		defer dstDatabse.Close()

		_, err = io.Copy(dstDatabse, srcDatabase)
		if err != nil {
			log.Println(err)
		}
	}
}

func OpenSqlCli() {

	// Ping the database to verify the connection is alive.
	err := db.Ping()
	if err != nil {
		log.Fatalf("Error connecting to database: %v", err)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		// Blocks until a signal is received.
		<-sigs

		// 5. Perform cleanup actions here.
		if db != nil {
			db.Close()
			fmt.Println("\nDatabase connection closed.")
		}

		os.Exit(0)
	}()

	fmt.Printf("Connected to SQLite database\n")
	fmt.Println("Enter SQL commands. Type 'exit' or 'quit' to close the CLI.")
	fmt.Println("Type '.tables' to list all tables.")

	scanner := bufio.NewScanner(os.Stdin) // Create a new scanner to read input from stdin.

	// Start an infinite loop to continuously prompt for SQL commands.
	for {
		fmt.Print("SQL> ") // Prompt the user for input.
		if !scanner.Scan() {
			break // Break the loop if there's no more input (e.g., EOF).
		}

		command := strings.TrimSpace(scanner.Text()) // Read the line and trim leading/trailing whitespace.
		lowerCommand := strings.ToLower(command)

		// Check for exit commands.
		if lowerCommand == "exit" || lowerCommand == "quit" {
			fmt.Println("Exiting SQL CLI.")
			break // Exit the loop and the function.
		}

		// Check for exit commands.
		if lowerCommand == "exit" || lowerCommand == "quit" {
			fmt.Println("Exiting SQL CLI.")
			break // Exit the loop and the function.
		}
		if lowerCommand == ".tables" {
			rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name;")
			if err != nil {
				fmt.Printf("Error listing tables: %v\n", err)
				continue
			}
			defer rows.Close()

			fmt.Println("Tables:")
			var tableName string
			for rows.Next() {
				if err := rows.Scan(&tableName); err != nil {
					fmt.Printf("Error scanning table name: %v\n", err)
					continue
				}
				fmt.Printf("- %s\n", tableName)
			}
			if err = rows.Err(); err != nil {
				fmt.Printf("Error iterating table rows: %v\n", err)
			}
			continue // Continue to the next prompt after listing tables.
		}
		if command == "" {
			continue // If the command is empty, just prompt again.
		}

		// Determine if the command is a SELECT query.
		// This is a simple check; a more robust solution might parse the SQL.
		if strings.HasPrefix(strings.ToLower(command), "select") {
			// Execute SELECT queries.
			rows, err := db.Query(command)
			if err != nil {
				fmt.Printf("Error executing query: %v\n", err)
				continue // Continue to the next iteration of the loop.
			}
			defer rows.Close() // Ensure rows are closed after processing.

			// Get column names from the query result.
			columns, err := rows.Columns()
			if err != nil {
				fmt.Printf("Error getting columns: %v\n", err)
				continue
			}

			// Prepare a slice to hold column values.
			// Each element is an interface{} because we don't know the exact type beforehand.
			values := make([]any, len(columns))
			// Prepare a slice of pointers to interface{} for scanning.
			// This allows Scan to write to the values slice.
			scanArgs := make([]any, len(columns))
			for i := range values {
				scanArgs[i] = &values[i]
			}

			// Calculate maximum column widths for pretty printing.
			colWidths := make(map[string]int)
			for _, col := range columns {
				colWidths[col] = len(col) // Initialize with column name length.
			}

			// Store all rows to calculate max widths before printing.
			var allRows [][]string
			for rows.Next() {
				err = rows.Scan(scanArgs...) // Scan the row into the scanArgs (which point to values).
				if err != nil {
					fmt.Printf("Error scanning row: %v\n", err)
					continue
				}

				rowValues := make([]string, len(columns))
				for i, val := range values {
					// Handle potential nil values from the database.
					if val == nil {
						rowValues[i] = "NULL"
					} else {
						rowValues[i] = fmt.Sprintf("%v", val) // Convert value to string.
					}
					// Update max width for the current column.
					if len(rowValues[i]) > colWidths[columns[i]] {
						colWidths[columns[i]] = len(rowValues[i])
					}
				}
				allRows = append(allRows, rowValues)
			}

			// Print header.
			for _, col := range columns {
				fmt.Printf("%-*s ", colWidths[col], col) // Left-align column name.
			}
			fmt.Println()

			// Print separator line.
			for _, col := range columns {
				fmt.Printf("%s ", strings.Repeat("-", colWidths[col])) // Print dashes for separator.
			}
			fmt.Println()

			// Print data rows.
			for _, row := range allRows {
				for i, val := range row {
					fmt.Printf("%-*s ", colWidths[columns[i]], val) // Left-align data.
				}
				fmt.Println()
			}

			if err = rows.Err(); err != nil {
				fmt.Printf("Error iterating rows: %v\n", err)
			}

		} else {
			// Execute DML (INSERT, UPDATE, DELETE) and DDL queries.
			result, err := db.Exec(command)
			if err != nil {
				fmt.Printf("Error executing command: %v\n", err)
				continue
			}

			// Print results for DML operations.
			rowsAffected, err := result.RowsAffected()
			if err != nil {
				fmt.Printf("Error getting rows affected: %v\n", err)
			} else {
				fmt.Printf("%d row(s) affected.\n", rowsAffected)
			}
		}

		if scanner.Err() != nil {
			fmt.Printf("Error reading input: %v\n", scanner.Err())
			break
		}
	}
}

func extractHost(profileName, configPath string) (SSHConfig, error) {
	c := SSHConfig{}
	hosts := getHosts(configPath)

	for _, h := range hosts {
		if h.Host == profileName {
			return h, nil
		}
	}

	return c, fmt.Errorf("failed to find the profile in the config file")
}

func editProfile(profileName, configPath string) error {
	tmpfile, err := os.CreateTemp("", "ssh-profile-*.md")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}

	defer os.Remove(tmpfile.Name())

	hosts := getHosts(configPath)

	config := SSHConfig{}

	for _, h := range hosts {
		if h.Host == profileName {
			config = h
		}
	}

	profileContent := generateHostBlock(config)

	writer := bufio.NewWriter(tmpfile)
	for _, line := range profileContent {
		fmt.Fprintln(writer, line)
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to write SSH config file: %w", err)
	}

	if err := tmpfile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %v", err)
	}

	fileInfoBefore, err := os.Stat(tmpfile.Name())
	if err != nil {
		return fmt.Errorf("failed to get file info: %v", err)
	}

	// Determine the editor to use
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = getDefaultEditor()
	}

	// Open the editor
	cmd := exec.Command(editor, tmpfile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start editor: %v", err)
	}

	// Wait for the editor to close
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("editor exited with error: %v", err)
	}

	fileInfoAfter, err := os.Stat(tmpfile.Name())
	if err != nil {
		return fmt.Errorf("failed to get file info after editing: %v", err)
	}

	if fileInfoAfter.ModTime().After(fileInfoBefore.ModTime()) {
		// Read the modified content
		newHosts := getHosts(tmpfile.Name())
		newHost := SSHConfig{}

		if len(newHosts) > 0 {
			newHost = newHosts[0]
		} else {
			fmt.Println("the file has no valid ssh/sftp profile")
			return fmt.Errorf("the file has no valid ssh/sftp profile")
		}

		if newHost.Host != "" {
			if newHost.Host == config.Host {
				fmt.Printf("Modified profile for %s\n", profileName)
			} else {
				foldername, err := readFolderForHostFromDB(config.Host)
				if err != nil {
					if !strings.Contains(err.Error(), "host not found in folder query") {
						return fmt.Errorf("failed to read folder for %v:%w", config.Host, err)
					}
				} else {
					newHost.Folder = foldername
					if err := updateProfileFolder(newHost.Host, newHost.Folder, ""); err != nil {
						log.Printf("failed to push the new host's (%v) folder to the database:%v", newHost.Host, err.Error())
					}
				}

				prompt := promptui.Select{
					Label: "Select Option",
					Items: []string{"Rename host", fmt.Sprintf("Save and duplicate as %s", newHost.Host)},
					Size:  2,
					Templates: &promptui.SelectTemplates{
						Label:    "{{ . }}?",
						Active:   "\U0001F534 {{ . | cyan }}",
						Inactive: "  {{ . | cyan }}",
						Selected: "\U0001F7E2 {{ . | red | cyan }}",
					},
				}

				_, whatToDo, err := prompt.Run()
				if err != nil {
					handleExitSignal(err)
					return fmt.Errorf("error selecting option: %w", err)
				}
				if whatToDo == "Rename host" {
					oldPass, err := readAndDecryptPassFromDB(config.Host)
					if err != nil {
						if !strings.Contains(err.Error(), "no password found") {
							return fmt.Errorf("failed to read old password from DB: %w", err)
						}
					}
					if len(oldPass) > 0 {
						if err := encryptAndPushPassToDB(newHost.Host, oldPass); err != nil {
							return fmt.Errorf("failed to push old password to DB: %w", err)
						}
					}

					oldNote, err := readNoteforHost(config.Host)
					if err != nil {
						return err
					}
					if len(oldNote) > 0 {
						encryptedContent, err := encrypt([]byte(oldNote))
						if err != nil {
							return err
						}

						if err := WriteNoteToDb(encryptedContent, newHost.Host); err != nil {
							return err
						}
					}

					if err := deleteSSHProfile(config.Host); err != nil {
						return fmt.Errorf("failed to delete old SSH profile: %w", err)
					}

					fmt.Printf("Modified the profile name from %s to %s\n", config.Host, newHost.Host)

				} else {
					fmt.Printf("A new profile:%s has been added\n", newHost.Host)
				}
			}

			if err := updateSSHConfig(configPath, newHost); err != nil {
				return fmt.Errorf("failed to update SSH config: %w", err)
			}

		} else {
			fmt.Printf("The edited file is not valid, hence the profile %s was not modified.\n", profileName)
		}

	} else {
		fmt.Printf("Profile %s was not modified.\n", profileName)
	}

	return nil
}

// updateSSHConfig updates or adds an SSH host configuration in the ~/.ssh/config file
// It modifies only the fields provided in the input config, preserving other existing fields
func updateSSHConfig(configPath string, config SSHConfig) error {

	// Read existing config file
	content, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read SSH config file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	inHostBlock := false
	hostUpdated := false
	updatedFields := make(map[string]bool)

	// Process existing config line by line
	for i, line := range lines {

		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "Host ") {
			if inHostBlock {
				// Add any fields that weren't in the existing config
				newLines = append(newLines, addMissingFields(config, updatedFields)...)
				inHostBlock = false
			}
			if trimmedLine == fmt.Sprintf("Host %s", config.Host) {
				inHostBlock = true
				hostUpdated = true
			}
		}

		if inHostBlock {
			if config.Proxy == "" && strings.Contains(trimmedLine, "Proxy") {
				continue
			}

			if config.ForwardSocket == "" && strings.Contains(trimmedLine, "Forward") {
				continue
			}
			// Parse and potentially update each field in the host block
			field, value := parseField(trimmedLine)
			if strings.Contains(field, "Proxy") {
				value = config.Proxy
			}

			if strings.Contains(field, "Forward") {
				value = config.ForwardSocket
			}
			if newValue := getUpdatedValue(config, field); newValue != "" {
				newLines = append(newLines, fmt.Sprintf("    %s %s", field, newValue))
				updatedFields[field] = true
			} else if value != "" {
				newLines = append(newLines, line) // Keep existing value
			}
		} else {
			newLines = append(newLines, line)
		}

		// Handle the case where the host block is at the end of the file
		if i == len(lines)-1 && inHostBlock {
			newLines = append(newLines, addMissingFields(config, updatedFields)...)
		}
	}

	// Add new host block if not updated
	if !hostUpdated {
		if len(newLines) > 0 && newLines[len(newLines)-1] != "" {
			newLines = append(newLines, "")
		}
		newLines = append(newLines, generateHostBlock(config)...)
	}

	// Write updated config back to file
	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("failed to create SSH config file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, line := range newLines {
		fmt.Fprintln(writer, line)
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to write SSH config file: %w", err)
	}

	return nil
}

// parseField extracts the field name and value from a config line
func parseField(line string) (string, string) {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", ""
}

// getUpdatedValue returns the new value for a field if it's provided in the input config
// Returns an empty string if the field is not to be updated
func getUpdatedValue(config SSHConfig, field string) string {
	switch strings.ToLower(field) {
	case "hostname":
		return config.HostName
	case "user":
		return config.User
	case "proxycommand":
		return config.Proxy
	case "forwardsocket":
		return config.ForwardSocket
	case "port":
		return config.Port
	case "identityfile":
		return config.IdentityFile
	// Add other fields as needed
	default:
		return ""
	}
}

// addMissingFields generates config lines for fields that are in the input config
// but were not present or updated in the existing config
func addMissingFields(config SSHConfig, updatedFields map[string]bool) []string {
	var missingFields []string
	if config.HostName != "" && !updatedFields["HostName"] {
		missingFields = append(missingFields, fmt.Sprintf("    HostName %s", config.HostName))
	}

	if config.User != "" && !updatedFields["User"] {
		missingFields = append(missingFields, fmt.Sprintf("    User %s", config.User))
	}

	if config.Port != "" && !updatedFields["Port"] {
		missingFields = append(missingFields, fmt.Sprintf("    Port %s", config.Port))
	}

	if config.Proxy != "" && !updatedFields["ProxyCommand"] {
		missingFields = append(missingFields, fmt.Sprintf("    ProxyCommand %s", config.Proxy))
	}

	if config.ForwardSocket != "" && !updatedFields["LocalForward"] {
		missingFields = append(missingFields, fmt.Sprintf("    LocalForward %s", config.ForwardSocket))
	}

	if config.IdentityFile != "" && !updatedFields["IdentityFile"] {
		missingFields = append(missingFields, fmt.Sprintf("    IdentityFile %s", config.IdentityFile))
	}

	// Add other fields as needed
	return missingFields
}

// generateHostBlock creates a complete host block for a new host
func generateHostBlock(config SSHConfig) []string {
	var block []string
	block = append(block, fmt.Sprintf("Host %s", config.Host))
	if config.HostName != "" {
		block = append(block, fmt.Sprintf("    HostName %s", config.HostName))
	}
	if config.User != "" {
		block = append(block, fmt.Sprintf("    User %s", config.User))
	}
	if config.Port != "" {
		block = append(block, fmt.Sprintf("    Port %s", config.Port))
	}
	if config.IdentityFile != "" {
		block = append(block, fmt.Sprintf("    IdentityFile %s", config.IdentityFile))
	}
	if config.Proxy != "" {
		block = append(block, fmt.Sprintf("    ProxyCommand %s", config.Proxy))
	}

	if config.ForwardSocket != "" {
		block = append(block, fmt.Sprintf("    LocalForward %s", config.ForwardSocket))
	}
	// Add other fields as needed
	return block
}

// deleteSSHProfile removes a specified host and its parameters from the SSH config file
func deleteSSHProfile(host string) error {
	// Get the user's home directory
	configPath, err := setupFilesFolders()
	if err != nil {
		fmt.Println(err)
		return fmt.Errorf("failed to get the config file path: %w", err)
	}
	// Read existing config file
	content, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, nothing to delete
		}
		return fmt.Errorf("failed to read SSH config file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	inHostBlock := false
	hostFound := false

	// Process existing config line by line
	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "Host ") {
			if inHostBlock {
				inHostBlock = false
			}
			if trimmedLine == fmt.Sprintf("Host %s", host) {
				inHostBlock = true
				hostFound = true
				continue
			}
		}

		if !inHostBlock {
			newLines = append(newLines, line)
		}

		// Handle the case where the host block is at the end of the file
		if i == len(lines)-1 && inHostBlock {
			inHostBlock = false
		}
	}

	if !hostFound {
		return fmt.Errorf("host %s not found in SSH config", host)
	}

	// Write updated config back to file
	file, err := os.Create(configPath)
	if err != nil {
		return fmt.Errorf("failed to create SSH config file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, line := range newLines {
		fmt.Fprintln(writer, line)
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to write SSH config file: %w", err)
	}

	if err := removeValue(host); err != nil {
		fmt.Println(err)
		return fmt.Errorf("failed to remove the %v from the password database:%v", host, err)
	}

	return nil
}

func removeValue(hostname string) error {

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	stmt, err := tx.Prepare("DELETE FROM sshprofiles WHERE host = ?;")
	if err != nil {
		return fmt.Errorf("failed to prepare delete statement: %w", err)
	}
	defer stmt.Close()

	// Execute the DELETE statement with the provided hostname.
	result, err := stmt.Exec(hostname)
	if err != nil {
		return fmt.Errorf("failed to delete record for host '%s': %w", hostname, err)
	}

	// Check how many rows were affected by the delete operation.
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected after deletion: %w", err)
	}

	if rowsAffected == 0 {
		log.Printf("ℹ️ No record found for host '%s'.\n", hostname)
	} else {
		log.Printf("🗑️ Successfully deleted %d record(s) '%s'.\n", rowsAffected, hostname)
	}

	return tx.Commit()

}

func updateFilePath(fileName string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	d := filepath.Join(homeDir, ".ssh")
	absolutepath := filepath.Join(d, fileName)
	return absolutepath, nil
}

func fixKeyPath(keyPath string) string {
	if runtime.GOOS == "windows" {
		if strings.Contains(keyPath, "/") && strings.Contains(keyPath, `\`) {
			keyPath = strings.ReplaceAll(keyPath, "/", "")
		} else if strings.Contains(keyPath, "/") {
			keyPath = strings.ReplaceAll(keyPath, "/", `\`)
		}

		keyPath = strings.ReplaceAll(keyPath, "~", "%USERPROFILE%")

	} else {
		if strings.Contains(keyPath, "/") && strings.Contains(keyPath, `\`) {
			keyPath = strings.ReplaceAll(keyPath, `\`, "")
		} else if strings.Contains(keyPath, `\`) {
			keyPath = strings.ReplaceAll(keyPath, `\`, `/`)
		}

	}

	keyPath = filepath.Clean(keyPath)

	if runtime.GOOS == "windows" {
		keyPath = expandWindowsPath(keyPath)
	}

	return keyPath

}

func processCliArgs() (ConsoleConfig, SSHConfig, *string, string) {

	action := flag.String("action", "", "Action to perform: add, remove")
	host := flag.String("host", "", "Host alias")
	hostname := flag.String("hostname", "", "HostName or IP address")
	password := flag.Bool("askpass", false, "Password prompt")
	username := flag.String("username", "", "Username")
	port := flag.String("port", "", "Port Number")
	identityFile := flag.String("key", "", "IdentityFile path")
	BaudRate := flag.String("baudrate", "9600", "BaudRate, default is 9600")
	DataBits := flag.String("data_bits", "8", "databits, default is 8")
	StopBit := flag.String("stop_bit", "1", "stop bit, default is 1")
	Parity := flag.String("parity", "none", "parity, default is none")
	device := flag.String("device", "/dev/tty.usbserial-1140", "device path, default is /dev/tty.usbserial-1140")
	version := flag.Bool("version", false, "prints the compile time (version)")
	sql := flag.Bool("sql", false, "Direct access to the sshcli.db file to run sql queries")
	profileType := flag.String("type", "ssh", "profile type, can be ssh or console, default is ssh")
	proxy := flag.String("http_proxy", "", "http proxy to be used for the ssh/sftp over http connectivity (optional),eg. 10.10.10.10:8000")
	forwardsocket := flag.String("forward_socket", "", "The ssh tunnel param to create a ssh tunnel using \"ssh -L <localPort>:<TargetMachine>:<TargetPort> JumpServerProfile\" command (optional),eg. 10.10.10.10:8000")

	flag.Parse()

	if *version {
		fmt.Println(CompileTime[1:])
		os.Exit(0)
	}

	if *sql {
		OpenSqlCli()
		db.Close()
		os.Exit(0)
	}

	var passString string

	if *password {
		fmt.Print("Enter password: ")
		bytePassword, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			fmt.Println("Error reading password:", err)
			os.Exit(1)
		}
		passString = string(bytePassword)
	}

	var consoleProfile ConsoleConfig
	var sshProfile SSHConfig

	switch strings.ToLower(*profileType) {
	case "console":
		consoleProfile = ConsoleConfig{
			Host:     *host,
			BaudRate: *BaudRate,
			Device:   *device,
			Parity:   *Parity,
			StopBit:  *StopBit,
			DataBits: *DataBits,
		}
	case "ssh":
		sshProfile = SSHConfig{
			Host:          *host,
			HostName:      *hostname,
			ForwardSocket: *forwardsocket,
			Password:      passString,
			User:          *username,
			Port:          *port,
			IdentityFile:  *identityFile,
		}

		if len(sshProfile.IdentityFile) > 0 {
			cleanPath := fixKeyPath(sshProfile.IdentityFile)
			sshProfile.IdentityFile = cleanPath
		}

		if len(*proxy) > 0 {
			sshProfile.Proxy = "nc -X connect -x " + *proxy + " %h %p"
		}
		if len(*forwardsocket) > 0 {
			localport, err := findAvailablePort()
			if err != nil {
				fmt.Println("Error finding an empty local port:", err)
				os.Exit(1)
			}
			sshProfile.ForwardSocket = fmt.Sprintf("%d %s", localport, *forwardsocket)
		}
	}

	return consoleProfile, sshProfile, action, *profileType
}

func cleanTheString(s, mode string) string {

	// remove colors
	s = strings.ReplaceAll(s, magenta, "")
	s = strings.ReplaceAll(s, green, "")
	s = strings.ReplaceAll(s, red, "")
	s = strings.ReplaceAll(s, yellow, "")
	s = strings.ReplaceAll(s, reset, "")

	if strings.ToLower(mode) == "keyboard" {
		s = s[4:]
	}

	// remove icons
	if strings.ToLower(mode) == "all" {
		s = strings.TrimPrefix(s, sshIcon+" ")
		s = strings.TrimPrefix(s, consoleIcon+" ")
		s = strings.TrimPrefix(s, folderIcon+" ")
	}

	// remove spaces
	s = strings.TrimSpace(s)

	return s
}

func ExecTheUI(configPath string) error {

	hosts := getHosts(configPath)
	items_to_show := getItems(hosts, false)

	searcher := func(input string, index int) bool {
		item := items_to_show[index]
		name := strings.ReplaceAll(strings.ToLower(item), " ", "")
		input = strings.ReplaceAll(strings.ToLower(input), " ", "")
		return strings.Contains(name, input)
	}

	prompt := promptui.Select{
		Label:    "Select Host",
		Searcher: searcher,
		Items:    items_to_show,
		Size:     35,
		Templates: &promptui.SelectTemplates{
			Label:    "{{ . }}?",
			Active:   "\U0001F534 {{ . | cyan }}",
			Inactive: "  {{ . | cyan }}",
			Selected: "\U0001F7E2 {{ . | red | cyan }}",
		},
	}

	_, chosen, err := prompt.Run()
	if err != nil {
		handleExitSignal(err)
		return fmt.Errorf("error running host selection prompt: %w", err)
	}

	if err := navigateToNext(chosen, hosts, configPath); err != nil {
		return fmt.Errorf("error navigating to next folder: %w", err)
	}

	return nil

}

func navigateToNext(chosen string, hosts []SSHConfig, configPath string) error {
	chosen_type := ""

	if strings.Contains(chosen, folderIcon) {
		chosen_type = "folder"
	} else if strings.Contains(chosen, consoleIcon) {
		chosen_type = "console"
	} else if strings.Contains(chosen, sshIcon) {
		chosen_type = "ssh"
	} else {
		fmt.Println("Entry not implemented1")
		return fmt.Errorf("unknown entery")
	}

	if chosen_type == "folder" {
		sshconfigInFolder := []SSHConfig{}
		CleanedChosen := cleanTheString(chosen, "all")

		profilesInFolder, err := getHostsInAFolder(CleanedChosen)
		if err != nil {
			return fmt.Errorf("error getting hosts in folder '%s': %w", CleanedChosen, err)
		}

		for _, f := range profilesInFolder {
			for _, sshconfig := range hosts {
				if sshconfig.Host == f {
					sshconfigInFolder = append(sshconfigInFolder, sshconfig)
				}
			}
		}

		submenu_items := getItems(sshconfigInFolder, true)

		submenu_searcher := func(input string, index int) bool {
			item := submenu_items[index]
			name := strings.ReplaceAll(strings.ToLower(item), " ", "")
			input = strings.ReplaceAll(strings.ToLower(input), " ", "")

			return strings.Contains(name, input)
		}

		submenu_prompt := promptui.Select{
			Label:    "Select Host",
			Searcher: submenu_searcher,
			Items:    submenu_items,
			Size:     35,
			Templates: &promptui.SelectTemplates{
				Label:    "{{ . }}?",
				Active:   "\U0001F534 {{ . | cyan }}",
				Inactive: "  {{ . | cyan }}",
				Selected: "\U0001F7E2 {{ . | red | cyan }}",
			},
		}

		_, submenu_chosen, err := submenu_prompt.Run()
		if err != nil {
			return fmt.Errorf("error running submenu prompt: %w", err)
		}

		if err := Connect(submenu_chosen, configPath, hosts); err != nil {
			chosenParts := strings.Split(submenu_chosen, " ")
			return fmt.Errorf("error connecting to host '%s': %w", chosenParts[1], err)
		}
	} else {
		if err := Connect(chosen, configPath, hosts); err != nil {
			chosenParts := strings.Split(chosen, " ")
			return fmt.Errorf("error connecting to host '%s': %w", chosenParts, err)
		}
	}

	return nil
}

func readNoteforHost(host string) (string, error) {
	var currentNotes sql.NullString
	query := "SELECT note FROM sshprofiles WHERE host = ?"
	row := db.QueryRow(query, host)

	err := row.Scan(&currentNotes)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("host: %s not found", host)
		}

		return "", fmt.Errorf("error reading note for profile %v from db: %v", host, err)
	}

	if currentNotes.Valid {
		decryptedNote, err := decrypt(currentNotes.String)
		if err != nil {
			if strings.Contains(err.Error(), "illegal base64 data at input byte") {
				decryptedNote = currentNotes.String
			} else {
				log.Println(err)
			}
		}

		// If the value is not NULL, use the .String field
		return decryptedNote, nil
	} else {
		// Handle the NULL case, for example, by writing an empty string or a placeholder
		return "", nil
	}
}

func updateNotesAndPushToDb(host string) error {

	tmpfile, err := os.CreateTemp("", "ssh-profile-note-*.md")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}

	defer os.Remove(tmpfile.Name())

	writer := bufio.NewWriter(tmpfile)

	currentNote, err := readNoteforHost(host)
	if err != nil {
		return err
	}
	fmt.Fprintln(writer, currentNote)

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to write current note to the temp file for profile %v: %w", host, err)
	}

	if err := tmpfile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %v", err)
	}

	fileInfoBefore, err := os.Stat(tmpfile.Name())
	if err != nil {
		return fmt.Errorf("failed to get file info: %v", err)
	}

	// Determine the editor to use
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = getDefaultEditor()
	}

	// Open the editor
	cmd := exec.Command(editor, tmpfile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start editor: %v", err)
	}

	// Wait for the editor to close
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("editor exited with error: %v", err)
	}

	fileInfoAfter, err := os.Stat(tmpfile.Name())
	if err != nil {
		return fmt.Errorf("failed to get file info after editing: %v", err)
	}

	if fileInfoAfter.ModTime().After(fileInfoBefore.ModTime()) {

		content, err := os.ReadFile(tmpfile.Name())
		if err != nil {
			return fmt.Errorf("failed to read from temp file: %w", err)
		}

		encryptedContent, err := encrypt(content)
		if err != nil {
			log.Println(err)
		}

		return WriteNoteToDb(encryptedContent, host)

	} else {
		fmt.Printf("Note for profile %s was not modified.\n", host)
	}

	return nil
}

func WriteNoteToDb(note, host string) error {
	updateQuery := "UPDATE sshprofiles SET note = ? WHERE host = ?"

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the db transaction for note update:%v", err)
	}
	stmt, err := tx.Prepare(updateQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare update statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(note, host)
	if err != nil {
		return fmt.Errorf("failed to update note for host %s: %w", host, err)
	}

	return tx.Commit()
}

func findAvailablePort() (int, error) {
	// Listen on port 0, which lets the OS choose a free port.
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	// Get the address of the listener and return the port.
	port := listener.Addr().(*net.TCPAddr).Port
	fmt.Println("Found available port:", port)
	return port, nil
}

func Connect(chosen string, configPath string, hosts []SSHConfig) error {

	chosen_type := ""
	chosen = cleanTheString(chosen, "onlyColors")

	chosenParts := strings.Split(chosen, " ")

	if len(chosenParts) < 1 {
		fmt.Println("Invalid item")
		return fmt.Errorf("invalid item selected")
	}

	if strings.Contains(chosen, folderIcon) {
		chosen_type = "folder"
	} else if strings.Contains(chosen, consoleIcon) {
		chosen_type = "console"
	} else if strings.Contains(chosen, sshIcon) {
		chosen_type = "ssh"
	} else {
		fmt.Println("entry not implemented2")
		return fmt.Errorf("unknown entry")
	}

	hostName := chosenParts[1]

	switch chosen_type {
	case "ssh":
		command, err := b_ui()
		if err != nil {
			return err
		}

		command = cleanTheString(command, "keyboard")

		if strings.EqualFold(command, "Set Folder") {
			moveToFolder(hostName)
		} else if strings.EqualFold(command, "Notes") {
			if err := updateNotesAndPushToDb(hostName); err != nil {
				log.Println(err)
			}
		} else if strings.EqualFold(command, "Duplicate/Edit Profile") {
			if err := editProfile(hostName, configPath); err != nil {
				return err
			}
		} else if strings.EqualFold(command, "Remove Profile") {
			deleteSSHProfile(hostName)

		} else if strings.EqualFold(command, "Reveal Password") {
			password, err := readAndDecryptPassFromDB(hostName)
			if err != nil {
				if strings.Contains(err.Error(), "no password") {
					password = ""
				} else {

					return err
				}
			}
			if len(password) == 0 {
				fmt.Println("No password found.")
			} else {

				if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
					err := runSudoCommand(password)
					if err != nil {
						fmt.Printf("Error executing sudo command: %v\n", err)
					}
					fmt.Println("Password for", hostName, "has been copied to the clipboard.")
				} else {
					fmt.Println("Password for", hostName, ":", password)
				}
			}

		} else if strings.EqualFold(command, "Set http proxy") {
			AddProxyToProfile(hostName, configPath)
		} else if strings.EqualFold(command, "Set SSH Tunnel") {
			AddForwardSocketToProfile(hostName, configPath)
		} else if strings.EqualFold(command, "Remove http proxy") {
			DeleteProxyFromProfile(hostName, configPath)
		} else if strings.EqualFold(command, "Remove SSH Tunnel") {
			DeleteFordwardSocketFromProfile(hostName, configPath)
		} else if strings.EqualFold(command, "Set Password") {
			fmt.Print("\nEnter password: ")
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				fmt.Println("Error reading password:", err)
				return fmt.Errorf("error reading password: %w", err)
			}

			passString := string(bytePassword)

			if err := encryptAndPushPassToDB(hostName, passString); err != nil {
				fmt.Println("FAILED!")
				return fmt.Errorf("failed to push the password to db in set password for the host %v:%v", hostName, err)
			}
		} else if strings.EqualFold(command, "ping") {
			h, err := extractHost(hostName, configPath)
			if err != nil {
				fmt.Println(err)
				return fmt.Errorf("error extracting host: %w", err)
			}
			cmd := *exec.Command(strings.ToLower(command), h.HostName)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
		} else if strings.EqualFold(command, "tcping") {
			if err := checkShellCommands(strings.ToLower(command)); err != nil {
				log.Println("tcping is not installed. install by checking https://github.com/pouriyajamshidi/tcping")
				fmt.Println("tcping is not installed. install by checking https://github.com/pouriyajamshidi/tcping")

			} else {
				port := "22"
				h, err := extractHost(hostName, configPath)
				if err != nil {
					fmt.Println(err)
					return fmt.Errorf("error extracting host: %w", err)
				}
				if h.Port != "" {
					port = h.Port
				}
				cmd := *exec.Command(strings.ToLower(command), h.HostName, port)
				cmd.Stdin = os.Stdin
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				cmd.Run()
			}
		} else if strings.EqualFold(command, "ssh-copy-id") {
			// make sure ssh-copy-id is availble in the shell
			if err := checkShellCommands("ssh-copy-id"); err != nil {
				fmt.Println(err.Error())
				return fmt.Errorf("ssh-copy-id command not found: %w", err)
			}

			h, err := extractHost(hostName, configPath)
			if err != nil {
				fmt.Println(err)
				return fmt.Errorf("error extracting host: %w", err)
			}

			if len(h.IdentityFile) == 0 {
				fmt.Println("No identity file specified.")
				fmt.Println("using the default ~/.ssh/id_rsa file")
				home, _ := os.UserHomeDir()
				h.IdentityFile = filepath.Join(home, ".ssh", "id_rsa")
			}

			if h.Port == "" {
				h.Port = "22"
			}

			if strings.HasPrefix(h.IdentityFile, "~") {
				homeDir, _ := os.UserHomeDir()
				h.IdentityFile = strings.ReplaceAll(h.IdentityFile, "~", homeDir)
			}

			password, err := readAndDecryptPassFromDB(hostName)
			if err != nil {
				if strings.Contains(err.Error(), "no password") {
					password = ""
				} else {

					return err
				}
			}

			cmd := *exec.Command("sshpass", "-p", password, "ssh-copy-id", "-i", h.IdentityFile, "-p", h.Port, h.User+"@"+h.HostName)

			if len(password) == 0 {
				fmt.Printf("There is no password stored for this profile\n")
				cmd = *exec.Command("ssh-copy-id", "-i", h.IdentityFile, "-p", h.Port, h.User+"@"+h.HostName)
			}

			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
		} else if strings.EqualFold(command, "sftp (text UI)") {

			h, err := extractHost(hostName, configPath)
			if err != nil {
				fmt.Println(err)
				return fmt.Errorf("error extracting host: %w", err)
			}

			if h.Port == "" {
				h.Port = "22"
			}

			if strings.HasPrefix(h.IdentityFile, "~") {
				homeDir, _ := os.UserHomeDir()
				h.IdentityFile = strings.Replace(h.IdentityFile, "~", homeDir, -1)
			}

			var password string

			if err := checkShellCommands("sshpass"); err != nil {
				log.Println("sshpass is not installed, only key authentication is supported")
			} else {

				password, err = readAndDecryptPassFromDB(hostName)
				if err != nil {
					if strings.Contains(err.Error(), "no password") {
						password = ""
					} else {

						return err
					}
				}
			}

			err = sftp_ui.INIT_SFTP(h.Host, h.HostName, h.User, password, h.Port, h.IdentityFile)
			if err != nil {
				if strings.Contains(err.Error(), "methods [none], no supported methods remain") {
					fmt.Printf("\n - Can't authenticate to the server. no password or key provided. \n\n")
				} else if strings.Contains(err.Error(), "methods [none password], no supported methods remain") {
					fmt.Printf("\n - Can't authenticate to the server. the provided password is wrong and no key provided. \n\n")
				} else {
					fmt.Println(err.Error())
				}
				fmt.Println(err)
				return fmt.Errorf("error initializing SFTP: %w", err)
			}

			//
		} else {
			if strings.EqualFold(command, "sftp (os native)") {
				command = "sftp"
			}

			// make sure sftp or ssh commands are availble in the shell
			if err := checkShellCommands(strings.ToLower(command)); err != nil {
				fmt.Println(err.Error())
				return fmt.Errorf("command not found: %w", err)
			}

			cmd := *exec.Command(strings.ToLower(command), hostName)
			method := "key"
			password := `''`

			// Check if sshpass command is availble in the shell, needed for passsword authentication
			if err := checkShellCommands("sshpass"); err != nil {
				log.Println("sshpass is not installed, only key authentication is supported")
				passAuthSupported = false

			} else if passAuthSupported {
				password, err = readAndDecryptPassFromDB(hostName)
				if err != nil {
					if strings.Contains(err.Error(), "no password") {
						password = ""
					} else {

						return err
					}
				}

				if password != "" {
					cmd = *exec.Command("sshpass", "-p", password, strings.ToLower(command), "-o", "StrictHostKeyChecking=no", hostName)
					method = "password"
				} else {
					cmd = *exec.Command(strings.ToLower(command), "-o", "StrictHostKeyChecking=no", hostName)
					method = "Key, or stdin password"
				}
			}

			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			fmt.Println("Trying:", method, "authentication")
			cmd.Run()
		}
	case "console":
		fmt.Println("console")
		promptCommand := promptui.Select{
			Label: "Select Command",
			Size:  35,
			Items: []string{"Connect via cu", "Duplicate/Edit Profile", "Remove Profile"},
			Templates: &promptui.SelectTemplates{
				Label:    "{{ . }}?",
				Active:   "\U0001F534 {{ . | cyan }}",
				Inactive: "  {{ . | cyan }}",
				Selected: "\U0001F7E2 {{ . | red | cyan }}",
			},
		}

		_, command, err := promptCommand.Run()
		if err != nil {
			handleExitSignal(err)
			return fmt.Errorf("error running command prompt: %w", err)
		}

		consoleProfile, err := readConsoleProfileFromDb(hostName)
		if err != nil {
			fmt.Println("Error reading console profile:", err)
			return fmt.Errorf("error reading console profile: %w", err)
		}

		if strings.ToLower(command) == "connect via cu" {

			cmd := *exec.Command("sudo", "cu", "-s", consoleProfile.BaudRate, "-l", consoleProfile.Device)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()

		} else if strings.ToLower(command) == "duplicate/edit profile" {

			err := editConsoleProfile(consoleProfile)
			if err != nil {
				fmt.Println(err)
				return fmt.Errorf("error editing console profile: %w", err)
			}

		} else if strings.ToLower(command) == "remove profile" {
			err := removeConsoleConfig(hostName)
			if err != nil {
				fmt.Println(err)
				return fmt.Errorf("error deleting console profile: %w", err)
			}
		}
	case "folder":
		if err := navigateToNext(chosen, hosts, configPath); err != nil {
			return fmt.Errorf("error navigating to next folder: %w", err)
		}
	}

	return nil
}

func getHosts(sshConfigPath string) []SSHConfig {

	var hosts []SSHConfig
	var currentHost *SSHConfig
	file, err := os.Open(sshConfigPath)
	if err != nil {
		fmt.Println(err)
		return hosts
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if after, ok := strings.CutPrefix(line, "Host "); ok {
			host := after
			if host == "*" {
				continue
			}
			if currentHost != nil {
				hosts = append(hosts, *currentHost)
			}
			currentHost = &SSHConfig{Host: host}

			if fName, err := readFolderForHostFromDB(host); err == nil && fName != "NULL" && fName != "" {
				currentHost.Folder = fName
			}

		} else if currentHost != nil {
			if after, ok := strings.CutPrefix(line, "HostName "); ok {
				currentHost.HostName = after
			} else if after, ok := strings.CutPrefix(line, "User "); ok {
				currentHost.User = after
			} else if after, ok := strings.CutPrefix(line, "Port "); ok {
				currentHost.Port = after
			} else if after, ok := strings.CutPrefix(line, "ProxyCommand "); ok {
				currentHost.Proxy = after
			} else if after, ok := strings.CutPrefix(line, "LocalForward "); ok {
				currentHost.ForwardSocket = after
			} else if after, ok := strings.CutPrefix(line, "IdentityFile "); ok {
				currentHost.IdentityFile = after
			}
		}
	}

	if currentHost != nil && currentHost.Host != "*" {
		hosts = append(hosts, *currentHost)
	}

	if err := scanner.Err(); err != nil {
		fmt.Println(err)
		return hosts
	}

	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Host < hosts[j].Host
	})

	return hosts
}

func isThereAnyNote(host string) bool {
	var currentNotes string

	query := "SELECT note FROM sshprofiles WHERE host = ? AND note IS NOT NULL"
	row := db.QueryRow(query, host)

	err := row.Scan(&currentNotes)
	if err == nil && len(currentNotes) > 0 {
		return true
	}
	return false
}

func getItems(hosts []SSHConfig, isSubmenu bool) []string {

	items := make([]string, 0)

	maxHostLen := 1
	maxUserLen := 1

	for _, host := range hosts {

		if len(host.Host) > maxHostLen {
			maxHostLen = len(host.Host)
		}

		if len(host.User) > maxUserLen {
			maxUserLen = len(host.User)
		}
	}

	maxHostLen += 1
	maxUserLen += 1

	if !isSubmenu {

		folderlist, err := getFolderList()
		if err != nil {
			log.Println("Error getting folder list:", err)
		}

		for _, f := range folderlist {

			for _, host := range hosts {
				if host.Folder == f {
					items = append(items, fmt.Sprintf("%s %s %s%s", folderIcon, magenta, f, reset))
					break
				}
			}
		}

		sort.Strings(items)
	}

	connectionItems := make([]string, 0)

	for _, host := range hosts {

		if !isSubmenu {
			if folder, err := readFolderForHostFromDB(host.Host); (err == nil) && (folder != "NULL" && folder != "") {
				continue
			}
		}

		item := fmt.Sprintf("%s %v%-*s >%v ", sshIcon, green, maxHostLen, host.Host, reset)

		if len(host.User) > 0 {
			item += fmt.Sprintf("%-*s%s@%s", maxUserLen, host.User, red, reset)
		}

		item += " " + host.HostName

		if len(host.Port) > 0 && host.Port != "22" {
			item += fmt.Sprintf(" -p %v", host.Port)
		}

		if len(host.Proxy) > 0 {
			item += " - 📡"
		}

		if len(host.ForwardSocket) > 0 {
			item += " - 🚇"
		}

		if isThereAnyNote(host.Host) {
			item += " - 🖍️"
		}

		connectionItems = append(connectionItems, item)
	}
	sort.Strings(connectionItems)
	items = append(items, connectionItems...)

	consoleItems := make([]string, 0)
	if runtime.GOOS == "darwin" && checkShellCommands("cu") == nil && !isSubmenu {

		consoleConfigs, err := readAllConsoleProfiles()
		if err != nil {
			log.Println("Error reading console profiles:", err)
		}

		if len(consoleConfigs) > 0 {
			for _, c := range consoleConfigs {
				consoleItems = append(consoleItems, fmt.Sprintf("%s %v%-*s >%v %v", consoleIcon, yellow, maxHostLen, c.Host, reset, c.BaudRate))
			}
		}

		sort.Strings(consoleItems)
	}

	items = append(items, consoleItems...)

	return items
}

func removePaths(stack []byte) []byte {
	lines := bytes.Split(stack, []byte("\n"))
	for i, line := range lines {
		if idx := bytes.LastIndex(line, []byte("/go/")); idx != -1 {
			lines[i] = line[idx+4:]
		} else if idx := bytes.Index(line, []byte(":")); idx != -1 {
			lines[i] = line[idx:]
		}
	}
	return bytes.Join(lines, []byte("\n"))
}

// ###################################### OS level helper functions #######################################
func runSudoCommand(pass string) error {

	cmd := exec.Command("sudo", "pbcopy")
	cmd.Stdin = bytes.NewBufferString(pass)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("command failed: %w", err)
	}

	return nil
}

func createFile(filePath string) error {
	file, err := os.Create(filePath)
	if err != nil {
		fmt.Println("Error creating file:", err)
		return err
	}
	defer file.Close()

	fmt.Printf(" [>] %sThe %v file has been created successfully\n%s", green, filePath, reset)
	return nil
}

func setupFilesFolders() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	p := filepath.Join(homeDir, ".ssh", "config")
	if _, err := os.Stat(p); err != nil {
		fmt.Printf(" [!] %sCould not find the ssh config file: %v. Creating it...\n%s", green, p, reset)

		if err := createFile(p); err != nil {
			fmt.Printf("%sfailed to create/access the config file : %v%s", red, p, reset)
			return "", fmt.Errorf("failed to create/access the config file: %w", err)
		}

		defaultConfig := SSHConfig{
			Host:         "vm1",
			HostName:     "172.16.0.1",
			User:         "root",
			Port:         "22",
			IdentityFile: filepath.Join(homeDir, ".ssh", "id_rsa"),
		}

		updateSSHConfig(p, defaultConfig)
	}

	return filepath.Join(homeDir, ".ssh", "config"), nil
}

func expandWindowsPath(path string) string {
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		name, value := parts[0], parts[1]
		path = strings.ReplaceAll(path, "%"+name+"%", value)
	}
	return path
}

func checkShellCommands(c string) error {
	if _, err := exec.LookPath(c); err != nil {
		return fmt.Errorf("%v command is not available in the default system shell", c)
	}
	return nil
}

func getDefaultEditor() string {
	editors := []string{"vim", "nvim", "vi", "nano"}

	switch runtime.GOOS {
	case "windows":
		return "notepad"

	default:
		for _, editor := range editors {
			if _, err := exec.LookPath(editor); err == nil {
				return editor
			}
		}
	}

	return "vi"
}

// ###################################### Proxy related functions #########################################
func IsProxyValid(s string) (string, error) {

	cleanProxy := strings.ReplaceAll(s, "http://", "")
	cleanProxy = strings.ReplaceAll(cleanProxy, "https://", "")
	cleanProxy = strings.ReplaceAll(cleanProxy, "/", "")

	parts := strings.Split(cleanProxy, ":")

	if len(parts) < 2 {
		return "", fmt.Errorf("proxy %v not formatted properly", s)
	}

	portStr := parts[len(parts)-1]
	ipStr := strings.Join(parts[:len(parts)-1], ":")

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "", fmt.Errorf("ip address not valid:%s", ipStr)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", fmt.Errorf("port is not valid:%s", portStr)
	}

	if port < 1 || port > 65535 {
		return "", fmt.Errorf("port range is not valid:%d", port)
	}

	return cleanProxy, nil
}

func AddProxyToProfile(hostName, configPath string) error {
	var proxy string
	fmt.Print("Enter httpproxy IP:Port (eg. 10.10.10.10:8080) or press enter to read it from https_proxy env variable: ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		proxy = scanner.Text()
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Error reading the proxy from stdin:", err)
	}

	if len(proxy) == 0 {
		//read https_proxy value and use use it
		proxy = os.Getenv("https_proxy")
		if proxy == "" {
			return fmt.Errorf("https_proxy is not set")
		}
	}

	cleanProxy, err := IsProxyValid(proxy)
	if err != nil {
		fmt.Println("Proxy string was not formatted properly!")
		return err
	}

	proxy = "nc -X connect -x " + cleanProxy + " %h %p"

	h, err := extractHost(hostName, configPath)
	if err != nil {
		fmt.Println(err)
		return fmt.Errorf("error extracting host: %w", err)
	}

	h.Proxy = proxy

	if err := updateSSHConfig(configPath, h); err != nil {
		fmt.Println("Error adding/updating profile:", err)
		return fmt.Errorf("error adding/updating profile: %w", err)
	}
	return nil
}

func AddForwardSocketToProfile(hostName, configPath string) error {
	var localport int
	var err error
	var target_socket string
	fmt.Print("Enter socket address (eg. <remotehost>:<remoteport> for random local port, or <localport>:<remotehost>:<remoteport>): ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		target_socket = scanner.Text()
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Error reading the socket address from stdin:", err)
	}

	if len(target_socket) == 0 {
		return fmt.Errorf("socket address is not provided")
	}
	parts := strings.Split(target_socket, ":")
	if len(parts) == 3 {
		localport, err = strconv.Atoi(parts[0])
		if err != nil {
			return err
		}
		target_socket = parts[1] + ":" + parts[2]
	} else if len(parts) == 2 {
		localport, err = findAvailablePort()
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("socket address is not valid")
	}
	cleanTargetSocket, err := IsProxyValid(target_socket)
	if err != nil {
		fmt.Println("Socket address was not formatted properly!")
		return err
	}

	forwardSocket := fmt.Sprintf("%d %s", localport, cleanTargetSocket)
	h, err := extractHost(hostName, configPath)
	if err != nil {
		fmt.Println(err)
		return fmt.Errorf("error extracting host: %w", err)
	}

	h.ForwardSocket = forwardSocket

	if err := updateSSHConfig(configPath, h); err != nil {
		fmt.Println("Error adding/updating profile:", err)
		return fmt.Errorf("error adding/updating profile: %w", err)
	}
	return nil
}

func DeleteProxyFromProfile(hostName, configPath string) error {

	h, err := extractHost(hostName, configPath)
	if err != nil {
		fmt.Println(err)
		return fmt.Errorf("error extracting host: %w", err)
	}

	h.Proxy = ""

	if err := updateSSHConfig(configPath, h); err != nil {
		fmt.Println("Error adding/updating profile:", err)
		return fmt.Errorf("error adding/updating profile: %w", err)
	}

	return nil
}

func DeleteFordwardSocketFromProfile(hostName, configPath string) error {

	h, err := extractHost(hostName, configPath)
	if err != nil {
		fmt.Println(err)
		return fmt.Errorf("error extracting host: %w", err)
	}

	h.ForwardSocket = ""

	if err := updateSSHConfig(configPath, h); err != nil {
		fmt.Println("Error adding/updating profile:", err)
		return fmt.Errorf("error adding/updating profile: %w", err)
	}

	return nil
}

// ###################################### Folder related dunctions ########################################
func moveToFolder(hostName string) error {

	FolderList, err := getFolderList()
	if err != nil {
		log.Println("failed to retrieve the folder list:%w", err)
	}

	FolderList = append(FolderList, "New Folder")
	FolderList = append(FolderList, "Rename Folder")
	FolderList = append(FolderList, "Remove from folder")
	folderName := ""

	prompt := promptui.Select{
		Label: "Select Folder",
		Items: FolderList,
		Size:  20,
		Templates: &promptui.SelectTemplates{
			Label:    "{{ . }}?",
			Active:   "\U0001F534 {{ . | cyan }}",
			Inactive: "  {{ . | cyan }}",
			Selected: "\U0001F7E2 {{ . | red | cyan }}",
		},
	}

	_, folderName, err = prompt.Run()
	if err != nil {
		handleExitSignal(err)
		return fmt.Errorf("error selecting folder: %w", err)
	}

	new_folder_name := ""
	currentFolder := ""
	if strings.EqualFold(folderName, "New Folder") || strings.EqualFold(folderName, "Rename folder") {
		var name string
		fmt.Print("New folder name: ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			name = scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintln(os.Stderr, "Error reading from stdin:", err)
		}

		if len(name) == 0 {
			fmt.Println("Empty name? really? Doing nothing!")
			return fmt.Errorf("invalid folder name")
		}

		if slices.Contains(FolderList, name) {
			fmt.Println("Folder with name", name, "already exists. Doing nothing!")
			return fmt.Errorf("folder with name %s already exists", name)
		}

		new_folder_name = name

		if strings.EqualFold(folderName, "Rename folder") {
			currentFolder, err = readFolderForHostFromDB(hostName)
			if err != nil {
				return fmt.Errorf("error reading folder for host from db: %w", err)
			}
		}

	} else if strings.EqualFold(folderName, "Remove from folder") {
		new_folder_name = "NULL"
	} else {
		new_folder_name = folderName
	}

	if err := updateProfileFolder(hostName, new_folder_name, currentFolder); err != nil {
		return err
	}

	return nil
}

func getHostsInAFolder(foldername string) ([]string, error) {

	var profile_list []string

	query := fmt.Sprintf("SELECT host FROM sshprofiles WHERE folder = \"%s\"", foldername)
	rows, err := db.Query(query, foldername)
	if err != nil {
		log.Println("error querying hosts in folder:", err)
		return profile_list, fmt.Errorf("error querying hosts in folder '%s': %w", foldername, err)
	}
	defer rows.Close()

	for rows.Next() {
		var profile string
		if err := rows.Scan(&profile); err != nil {
			log.Println("error scanning profile:", err)
			continue
		}
		profile_list = append(profile_list, profile)
	}

	if err := rows.Err(); err != nil {
		return profile_list, fmt.Errorf("error iterating over hosts: %w", err)
	}

	return profile_list, nil

}

func readFolderForHostFromDB(host string) (string, error) {
	var folder string

	query := "SELECT folder FROM sshprofiles WHERE host = ?"

	row := db.QueryRow(query, host)
	err := row.Scan(&folder)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("host not found in folder query: %s", host)
	} else if err != nil {
		return "", fmt.Errorf("read folder query failed: %w", err)
	}

	return folder, nil
}

func getFolderList() ([]string, error) {

	var folderlist []string

	query := "SELECT DISTINCT folder FROM sshprofiles WHERE folder IS NOT NULL;"
	rows, err := db.Query(query)
	if err != nil {
		return folderlist, fmt.Errorf("error querying folders: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var folder string
		if err := rows.Scan(&folder); err != nil {
			log.Println("Error scanning folder:", err)
			continue
		}
		folderlist = append(folderlist, folder)
	}

	if err := rows.Err(); err != nil {
		return folderlist, fmt.Errorf("error iterating over folders: %w", err)
	}
	return folderlist, nil
}

func updateProfileFolder(hostname, newFolderName, currentFolderName string) error {

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the db transaction for folder update:%v", err)
	}

	sql_clause := "INSERT INTO sshprofiles(host,folder) VALUES(?,?) ON CONFLICT(host) DO UPDATE SET folder = excluded.folder;"

	if len(currentFolderName) > 0 {
		sql_clause = "UPDATE sshprofiles SET folder = ? WHERE folder = ?;"
	}

	stmt, err := tx.Prepare(sql_clause)
	if err != nil {
		return fmt.Errorf("failed to prepare the db for folder update:%v", err)
	}
	defer stmt.Close()

	if len(currentFolderName) > 0 {
		_, err = stmt.Exec(newFolderName, currentFolderName)
	} else if newFolderName == "NULL" {
		_, err = stmt.Exec(hostname, sql.NullString{String: "", Valid: false})
	} else {
		_, err = stmt.Exec(hostname, newFolderName)
	}
	if err != nil {
		return fmt.Errorf("failed to update the folder for the host %v: %v", hostname, err)
	}
	return tx.Commit()
}

// #######################################  Console related functions ######################################
// generateHostBlock creates a complete host block for a new host
func generateConsoleHostBlock(config ConsoleConfig) []byte {

	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Printf("error marshalling JSON: %v", err)
	}

	return content
}

func removeConsoleConfig(h string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the db transaction for console profile deletion: %v", err)
	}

	stmt, err := tx.Prepare("DELETE FROM console_profiles WHERE host = ?;")
	if err != nil {
		fmt.Printf("failed to prepare the db for console profile deletion:%v\n", err)
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(h)
	if err != nil {
		fmt.Printf("failed to delete the console profile for the host %v: %v\n", h, err)
		return err
	}

	fmt.Printf("Console profile for %s has been removed successfully.\n", h)

	return tx.Commit()

}

func addConsoleConfig(profile ConsoleConfig) error {

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the db transaction for console profile update:%v", err)
	}

	stmt, err := tx.Prepare(`
							INSERT INTO console_profiles(host,baud_rate,device,parity,stop_bit,data_bits,folder) 
							VALUES(?,?,?,?,?,?,?)
							ON CONFLICT(host) DO UPDATE SET folder = excluded.folder, 
							baud_rate = excluded.baud_rate, 
							device = excluded.device, 
							parity = excluded.parity, 
							stop_bit = excluded.stop_bit, 
							data_bits = excluded.data_bits;
							`)
	if err != nil {
		return fmt.Errorf("failed to prepare the db for console profile update:%v", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(profile.Host, profile.BaudRate, profile.Device, profile.Parity, profile.StopBit, profile.DataBits, profile.Folder)
	if err != nil {
		return fmt.Errorf("failed to update the console profile for the host %v: %v", profile.Host, err)
	}
	return tx.Commit()
}

func readAllConsoleProfiles() (ConsoleConfigs, error) {
	var consoleProfiles ConsoleConfigs

	rows, err := db.Query("SELECT host, baud_rate, device, parity, stop_bit, data_bits, folder FROM console_profiles")
	if err != nil {
		log.Println("Error querying console profiles:", err)
		return consoleProfiles, err
	}
	defer rows.Close()

	for rows.Next() {
		var profile ConsoleConfig
		if err := rows.Scan(&profile.Host, &profile.BaudRate, &profile.Device, &profile.Parity, &profile.StopBit, &profile.DataBits, &profile.Folder); err != nil {
			log.Println("Error scanning console profile:", err)
			continue
		}
		consoleProfiles = append(consoleProfiles, profile)
	}

	if err := rows.Err(); err != nil {
		log.Println("Error reading console profiles:", err)
	}

	return consoleProfiles, nil
}

func readConsoleProfileFromDb(host string) (ConsoleConfig, error) {
	var profile ConsoleConfig

	query := "SELECT host, baud_rate, device, parity, stop_bit, data_bits FROM console_profiles WHERE host = ?"
	row := db.QueryRow(query, host)

	err := row.Scan(&profile.Host, &profile.BaudRate, &profile.Device, &profile.Parity, &profile.StopBit, &profile.DataBits)
	if err != nil {
		if err == sql.ErrNoRows {
			return profile, fmt.Errorf("console profile not found for host: %s", host)
		}
		return profile, fmt.Errorf("error reading console profile from db: %v", err)
	}

	return profile, nil
}

func editConsoleProfile(config ConsoleConfig) error {
	tmpfile, err := os.CreateTemp("", "console-profile-*.md")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}

	defer os.Remove(tmpfile.Name())

	profileContent := generateConsoleHostBlock(config)

	_, err = tmpfile.Write(profileContent)
	if err != nil {
		return fmt.Errorf("error writing file: %v", err)
	}

	if err := tmpfile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %v", err)
	}

	fileInfoBefore, err := os.Stat(tmpfile.Name())
	if err != nil {
		return fmt.Errorf("failed to get file info: %v", err)
	}

	// Determine the editor to use
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = getDefaultEditor()
	}

	// Open the editor
	cmd := exec.Command(editor, tmpfile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start editor: %v", err)
	}

	// Wait for the editor to close
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("editor exited with error: %v", err)
	}

	fileInfoAfter, err := os.Stat(tmpfile.Name())
	if err != nil {
		return fmt.Errorf("failed to get file info after editing: %v", err)
	}

	if fileInfoAfter.ModTime().After(fileInfoBefore.ModTime()) {

		c := ConsoleConfig{}
		newHost, _ := os.ReadFile(tmpfile.Name())

		if len(newHost) != 0 {
			err = json.Unmarshal(newHost, &c)
			if err != nil {
				fmt.Printf("error unmarshalling JSON for consoleConfigs: %v\n", err)
				return fmt.Errorf("error unmarshalling JSON for consoleConfigs: %v", err)
			}
		}

		if c.Host != "" {

			if err := addConsoleConfig(c); err != nil {
				return err
			}

			if c.Host == config.Host {
				fmt.Printf("Modified profile for %s\n", c.Host)
			} else {
				fmt.Printf("A new profile:%s has been added\n", c.Host)
			}

		} else {
			fmt.Printf("The edited file is not valid, hence the profile %s was not modified.\n", config.Host)
		}

	} else {
		fmt.Printf("Profile %s was not modified.\n", config.Host)
	}

	return nil
}

// ###################################### administrative functions ########################################
func customPanicHandler() {
	if r := recover(); r != nil {
		stack := debug.Stack()
		sanitizedStack := removePaths(stack)
		fmt.Printf("Panic: %v\n%s", r, sanitizedStack)
		os.Exit(1)
	}
}

func handleExitSignal(err error) {
	if strings.EqualFold(strings.TrimSpace(err.Error()), "^C") {

	} else {
		fmt.Printf("Prompt failed %v\n", err)
	}
}

func main() {

	/* custompanicHandler hides developer's filesystem information from the stack trace
	and only shows the panic message */
	defer customPanicHandler()

	//########################################## LOG #####################################################
	// all log.Printx() operations arw written in ~/sshcli.log file instead of being printed on the screen
	homeDir, _ := os.UserHomeDir()
	logFilePath := filepath.Join(homeDir, "sshcli.log")
	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer file.Close()

	log.SetOutput(file)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	//########################################## DBS #####################################################
	// Here we initialize the database

	d := filepath.Join(homeDir, ".ssh")
	if _, err := os.Stat(d); err != nil {
		fmt.Printf(" [!] %sCould not find/access the ssh config file path: %s. Creating it...\n%s", green, d, reset)
		err := os.Mkdir(d, 0755)
		if err != nil {
			fmt.Printf(" [!] %sError creating directory '%s': %v\n%s", red, d, err, reset)
			log.Printf("failed to create/access the .ssh directory: %v", err)
		} else {
			fmt.Printf(" [>] %sSuccessfully created the .ssh directory '%s'.\n%s", green, d, reset)
		}
	}

	databaseFile := filepath.Join(d, "sshcli.db")
	if err := initDB(databaseFile); err != nil {
		fmt.Printf("fatal error during database initialization: %v\n", err)
		return
	}
	defer db.Close()

	//########################################## CLI #####################################################
	// Here we read the cli aruguments
	consoleProfile, sshProfile, action, profileType := processCliArgs()

	//########################################## SSH #####################################################

	configPath, err := setupFilesFolders()
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	//########################################## ENC #####################################################
	// Check if there is encryption key inside the sqlite db, if no, check if encryption.key file is available
	// If yes, push it into the db and use the one in the db moving forward.
	// if not encryption key file found, generate a new one and store in the sqlite db.
	key, err = loadOrGenerateKey()
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	// ###################################### LEGACY MIGRATION CODE ##########################################
	// These can be removed after the migration was not required for encryption key,password and folder from json

	// loadCredentials reads the encrypted passwords from passwords.json and imports them into the sqlite db
	// only if sqlite db has no password for any profile.
	err = loadCredentials()
	if err != nil {
		log.Printf("Can't read the password file: %v", err)
	}

	if !isFolderConfiguredinDB() {

		folderdb := "folderdb.json"
		if folderdb, err = updateFilePath(folderdb); err != nil {
			fmt.Println(err.Error(), "Folder database not found, showing flat entries.")
		}

		// Read the folderdb.json file
		func() {
			folders, err := readFolderDb(folderdb)
			if err != nil {
				log.Println("Can't read the folder json file")
				return
			}
			tx, err := db.Begin()
			if err != nil {
				log.Printf("failed to begin transaction: %v\n", err)
			}
			stmt, err := tx.Prepare("INSERT INTO sshprofiles(host, folder) VALUES(?, ?) ON CONFLICT(host) DO UPDATE SET folder = excluded.folder;")
			if err != nil {
				tx.Rollback()
				log.Printf("failed to prepare statement: %v\n", err)
			}
			defer stmt.Close()

			for _, f := range folders {
				for _, host := range f.Profiles {
					if _, err := stmt.Exec(host, f.Name); err != nil {
						tx.Rollback()
						log.Printf("failed to insert folder for '%s': %v\n", host, err)
					}
				}

			}
			if err := tx.Commit(); err != nil {
				fmt.Println(err)
			}
			fmt.Println("✅ Data Migration Event: Filled the folder db from the folderdb.json file. The json file can be removed")
		}()
	}

	//########################################################################################################

	doConfigBackup("all")

	if *action != "" {
		if sshProfile.Host == "" && consoleProfile.Host == "" {
			fmt.Println("Usage: -action [add|remove] -host HOST [other flags...]")
			return
		} else {
			switch strings.ToLower(*action) {
			case "add":
				switch strings.ToLower(profileType) {
				case "ssh":
					if err := updateSSHConfig(configPath, sshProfile); err != nil {
						fmt.Println("Error adding/updating profile:", err)
						return
					}

					if sshProfile.Password != "" {
						if err := encryptAndPushPassToDB(sshProfile.Host, sshProfile.Password); err != nil {
							fmt.Println(err)
							return
						}
					}
				case "console":
					if err := addConsoleConfig(consoleProfile); err != nil {
						fmt.Println("Error adding console profile:", err)
						return
					}
				}
			case "remove":
				switch strings.ToLower(profileType) {
				case "ssh":
					if err := deleteSSHProfile(sshProfile.Host); err != nil {
						fmt.Println("Error removing profile:", err)
					}

					if err := removeValue(sshProfile.Host); err != nil {
						fmt.Println(err)
						return
					}

				case "console":
					if err := removeConsoleConfig(consoleProfile.Host); err != nil {
						fmt.Println("Error removing console profile:", err)
						return
					}
				}
			default:
				fmt.Println("Invalid action. Use '-action add' or '-action remove'.")
			}
		}
	} else {
		if err := ExecTheUI(configPath); err != nil {
			if !strings.Contains(err.Error(), "^C") {
				fmt.Println(err)
			}
			return
		}
	}
}
