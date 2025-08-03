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

var console_file_path = "console.json"
var consoleConfigs ConsoleConfigs
var folderdb = "folderdb.json"
var CompileTime = ""
var passAuthSupported = true
var key []byte
var db *sql.DB
var folderIcon = "🗂️"
var sshIcon = "🌐"
var consoleIcon = "📟"

// var gearIcon = "🛠️"

// SSHConfig represents the configuration for an SSH host entry
type SSHConfig struct {
	Host         string
	HostName     string
	User         string
	Port         string
	Proxy        string
	IdentityFile string
	Password     string
	Folder       string
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

type Folders []struct {
	Name     string   `json:"name"`
	Profiles []string `json:"profiles"`
}

type ConsoleConfigs []ConsoleConfig

// initDB opens a connection to the SQLite database and ensures all necessary
// tables are created. It's designed to be idempotent.
func initDB(filepath string) error {
	var err error
	// Open the database file. It will be created if it doesn't exist.
	db, err = sql.Open("sqlite", filepath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// A map of table names to their CREATE statements for clarity and organization.
	tables := map[string]string{
		"credentials": `
		CREATE TABLE IF NOT EXISTS credentials (
			host TEXT PRIMARY KEY,
			password TEXT NOT NULL
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

		// This schema allows for a nested folder structure.
		// A NULL parent_id indicates a root-level folder.
		"folder_structure": `
		CREATE TABLE IF NOT EXISTS folder_structure (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			parent_id INTEGER,
			FOREIGN KEY (parent_id) REFERENCES folder_structure(id) ON DELETE CASCADE
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

func doConfigBackup() error {
	configFilePath, err := setupFilesFolders()
	if err != nil {
		return fmt.Errorf("failed to get the config file path")
	}

	backupConfigFilePath := configFilePath + "_backup"

	srcFile, err := os.Open(configFilePath)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(backupConfigFilePath)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func removeProfileFromStringSlice(slice []string, s string) []string {
	for i, v := range slice {
		if v == s {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

func RemoveProfileFromFolders(data Folders, profileToRemove string) Folders {

	result := make(Folders, len(data))

	for i, item := range data {
		item.Profiles = removeProfileFromStringSlice(item.Profiles, profileToRemove)
		result[i] = item
	}

	return result
}

func overWriteFolderDb(folders Folders) error {
	data, err := json.MarshalIndent(folders, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling folderdb json: %v", err)
	}

	if err := os.WriteFile(folderdb, data, 0644); err != nil {
		return fmt.Errorf("error writing folderdb file: %v", err)
	}

	return nil
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
		sig := <-sigs
		log.Printf("Received signal: %v. Shutting down gracefully...", sig)

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

func readFolderDb() (Folders, error) {

	folder := Folders{}

	if _, err := os.Stat(folderdb); err != nil {
		fmt.Printf(" [!] %sIt seems no folder database file was created before, so here is one: %v\n%s", green, folderdb, reset)

		if err := createFile(folderdb); err != nil {
			fmt.Printf(" [!] %sfailed to create/access the folder database file: %v%s", red, folderdb, reset)
			return folder, fmt.Errorf("failed to create/access the folder database file: %v", err)
		}
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

func extractHost(profileName, configPath string, folders Folders) (SSHConfig, error) {
	c := SSHConfig{}
	hosts := getHosts(configPath, folders)

	for _, h := range hosts {
		if h.Host == profileName {
			return h, nil
		}
	}

	return c, fmt.Errorf("failed to find the profile in the config file")
}

func editProfile(profileName, configPath string, folders Folders) error {
	tmpfile, err := os.CreateTemp("", "ssh-profile-*.txt")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}

	defer os.Remove(tmpfile.Name())

	hosts := getHosts(configPath, folders)

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
		newHosts := getHosts(tmpfile.Name(), folders)
		newHost := SSHConfig{}

		if len(newHosts) > 0 {
			newHost = newHosts[0]
		} else {
			fmt.Println("the file has no valid ssh/sftp profile")
			return fmt.Errorf("the file has no valid ssh/sftp profile")
		}

		if newHost.Host != "" {
			doConfigBackup()
			deleteSSHProfile(newHost.Host)
			updateSSHConfig(configPath, newHost)

			if newHost.Host == config.Host {
				fmt.Printf("Modified profile for %s\n", profileName)
			} else {
				fmt.Printf("A new profile:%s has been added\n", newHost.Host)
			}
		} else {
			fmt.Printf("The edited file is not valid, hence the profile %s was not modified.\n", profileName)
		}

	} else {
		fmt.Printf("Profile %s was not modified.\n", profileName)
	}

	return nil
}

func editConsoleProfile(profileName string) error {
	tmpfile, err := os.CreateTemp("", "console-profile-*.txt")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}

	defer os.Remove(tmpfile.Name())

	config := ConsoleConfig{}

	for _, h := range consoleConfigs {
		if h.Host == profileName {
			config = h
		}
	}

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
		// Read the modified content

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
			addConsoleConfig(c)

			if c.Host == config.Host {
				fmt.Printf("Modified profile for %s\n", profileName)
			} else {
				fmt.Printf("A new profile with a the name:%s has been added\n", c.Host)
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
			// Parse and potentially update each field in the host block
			field, value := parseField(trimmedLine)
			if strings.Contains(field, "Proxy") {
				value = config.Proxy
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
	// Add other fields as needed
	return block
}

// generateHostBlock creates a complete host block for a new host
func generateConsoleHostBlock(config ConsoleConfig) []byte {

	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Printf("error marshalling JSON: %v", err)
	}

	return content
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

func createFile(filePath string) error {
	file, err := os.Create(filePath)
	if err != nil {
		fmt.Println("Error creating file:", err)
		return err
	}
	defer file.Close()

	fmt.Printf(" [>] The %v file has been created successfully\n", filePath)
	return nil
}

func handleExitSignal(err error) {
	if strings.EqualFold(strings.TrimSpace(err.Error()), "^C") {

	} else {
		fmt.Printf("Prompt failed %v\n", err)
	}
}

func setupFilesFolders() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	d := filepath.Join(homeDir, ".ssh")
	if _, err := os.Stat(d); err != nil {
		fmt.Printf(" [!] %sCould not find/access the ssh config file path: %s. Creating it...\n%s", green, d, reset)
		err := os.Mkdir(d, 0755)
		if err != nil {
			fmt.Printf(" [!] %sError creating directory '%s': %v\n%s", red, d, err, reset)
			return "", fmt.Errorf("failed to create/access the .ssh directory: %w", err)
		} else {
			fmt.Printf(" [>] %sSuccessfully created the .ssh directory '%s'.\n%s", green, d, reset)
		}
	}

	p := filepath.Join(d, "config")
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
			IdentityFile: filepath.Join(d, "id_rsa"),
		}

		updateSSHConfig(p, defaultConfig)
	}

	c := filepath.Join(d, "console.json")

	if runtime.GOOS == "darwin" && checkShellCommands("cu") == nil {
		if _, err := os.Stat(c); err != nil {
			fmt.Printf(" [!] %sCould not find the console profile database: %v. Creating it...\n%s", green, c, reset)

			if err := createFile(c); err != nil {
				fmt.Printf("%sfailed to create/access the console file: %v%s", red, c, reset)
				return "", fmt.Errorf("failed to create/access the console file: %w", err)
			}
		}
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
	sql := flag.Bool("sql", false, "Access SQL file")
	profileType := flag.String("type", "ssh", "profile type, can be ssh or console, default is ssh")
	proxy := flag.String("httpproxy", "", "http proxy to be used for the ssh/sftp over http connectivity (optional),eg. 10.10.10.10:8000")

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
			Host:         *host,
			HostName:     *hostname,
			Password:     passString,
			User:         *username,
			Port:         *port,
			IdentityFile: *identityFile,
		}

		if len(sshProfile.IdentityFile) > 0 {
			cleanPath := fixKeyPath(sshProfile.IdentityFile)
			sshProfile.IdentityFile = cleanPath
		}

		if len(*proxy) > 0 {
			sshProfile.Proxy = "nc -X connect -x " + *proxy + " %h %p"
		}
	}

	return consoleProfile, sshProfile, action, *profileType
}

func containsProfile(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func AddProfileToFolder(data Folders, folderName string, newProfile string) Folders {

	for i, item := range data {
		if strings.EqualFold(item.Name, folderName) {
			if !containsProfile(item.Profiles, newProfile) {
				data[i].Profiles = append(data[i].Profiles, newProfile)
				fmt.Printf("Profile %q added to folder %q.\n", newProfile, folderName)
			} else {
				fmt.Printf("Profile %q already exists in folder %q. No changes made.\n", newProfile, folderName)
			}
			return data
		}
	}

	fmt.Printf("Folder with name %q not found.\n", folderName)
	return data
}

func IsProxyValid(s string) bool {

	parts := strings.Split(s, ":")

	if len(parts) < 2 {
		return false
	}

	portStr := parts[len(parts)-1]
	ipStr := strings.Join(parts[:len(parts)-1], ":")

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return false
	}

	if port < 1 || port > 65535 {
		return false
	}

	return true
}

func AddProxyToProfile(hostName, configPath string, folders Folders) error {
	var proxy string
	fmt.Print("Enter http proxy IP:Port (eg. 10.10.10.10:8080): ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		proxy = scanner.Text()
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Error reading the proxy from stdin:", err)
	}

	if !IsProxyValid(proxy) {
		fmt.Println("Proxy string was not fromatted properly!")
		return fmt.Errorf("invalid proxy format")
	}

	proxy = "nc -X connect -x " + proxy + " %h %p"

	h, err := extractHost(hostName, configPath, folders)
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

func DeleteProxyFromProfile(hostName, configPath string, folders Folders) error {

	h, err := extractHost(hostName, configPath, folders)
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

func moveToFolder(hostName string, folders Folders) error {

	FolderList := make([]string, 0, len(folders))
	for _, folder := range folders {
		FolderList = append(FolderList, folder.Name)
	}

	FolderList = append(FolderList, "New Folder")
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

	_, folderName, err := prompt.Run()
	if err != nil {
		handleExitSignal(err)
		return fmt.Errorf("error selecting folder: %w", err)
	}

	if strings.EqualFold(folderName, "New Folder") {
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

		for _, p := range folders {
			if p.Name == name {
				fmt.Println("Folder with name", name, "already exists. Doing nothing!")
				return fmt.Errorf("folder with name %s already exists", name)
			}
		}

		folders = RemoveProfileFromFolders(folders, hostName)

		folders = append(folders, struct {
			Name     string   "json:\"name\""
			Profiles []string "json:\"profiles\""
		}{Name: name, Profiles: []string{hostName}})

	} else if strings.EqualFold(folderName, "Remove from folder") {
		folders = RemoveProfileFromFolders(folders, hostName)
	} else {
		folders = RemoveProfileFromFolders(folders, hostName)
		folders = AddProfileToFolder(folders, folderName, hostName)
	}

	if err := overWriteFolderDb(folders); err != nil {
		return fmt.Errorf("error overwriting folder db: %w", err)
	}

	return nil
}

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

func cleanTheString(s, mode string) string {

	// remove colors
	s = strings.ReplaceAll(s, magenta, "")
	s = strings.ReplaceAll(s, green, "")
	s = strings.ReplaceAll(s, red, "")
	s = strings.ReplaceAll(s, yellow, "")
	s = strings.ReplaceAll(s, reset, "")

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

func Connect(chosen string, configPath string, folders Folders, hosts []SSHConfig) error {

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

	if chosen_type == "ssh" {
		promptCommand := promptui.Select{
			Label: "Select Command",
			Size:  35,
			Items: []string{"SSH", "SFTP (os native)", "SFTP (text UI)", "Ping", "TCPing", "ssh-copy-id", "Edit Profile", "Set Password", "Set http proxy", "Set Folder", "Reveal Password", "Remove http proxy", "Remove Profile"},
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
		if strings.EqualFold(command, "Set Folder") {
			moveToFolder(hostName, folders)

		} else if strings.EqualFold(command, "Edit Profile") {
			editProfile(hostName, configPath, folders)

		} else if strings.EqualFold(command, "Remove Profile") {
			deleteSSHProfile(hostName)

		} else if strings.EqualFold(command, "Reveal Password") {
			password, err := readAndDecryptPassFromDB(hostName)

			if err != nil {
				return err
			}

			if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
				err := runSudoCommand(password)
				if err != nil {
					fmt.Printf("Error executing sudo command: %v\n", err)
				}
				fmt.Println("Password for", hostName, "has been copied to the clipboard.")
			} else {
				fmt.Println("Password for", hostName, ":", password)
			}

		} else if strings.EqualFold(command, "Set http proxy") {
			AddProxyToProfile(hostName, configPath, folders)
		} else if strings.EqualFold(command, "Remove http proxy") {
			DeleteProxyFromProfile(hostName, configPath, folders)
		} else if strings.EqualFold(command, "Set Password") {

			fmt.Print("\nEnter password: ")
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				fmt.Println("Error reading password:", err)
				return fmt.Errorf("error reading password: %w", err)
			}

			passString := string(bytePassword)

			profile := SSHConfig{
				Host:     hostName,
				Password: passString,
			}

			updatePasswordDB(profile)
			if err := encryptAndPushPassToDB(hostName, passString); err != nil {
				fmt.Println("FAILED!")
				return fmt.Errorf("failed to push the password to db in set password for the host %v:%v", hostName, err)
			}

		} else if strings.EqualFold(command, "ping") {
			h, err := extractHost(hostName, configPath, folders)
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
				h, err := extractHost(hostName, configPath, folders)
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
		} else if strings.EqualFold(strings.ToLower(command), "ssh-copy-id") {
			// make sure ssh-copy-id is availble in the shell
			if err := checkShellCommands("ssh-copy-id"); err != nil {
				fmt.Println(err.Error())
				return fmt.Errorf("ssh-copy-id command not found: %w", err)
			}

			h, err := extractHost(hostName, configPath, folders)
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

			password, err := decrypt(hostName)
			if err != nil {
				return err
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
		} else if strings.EqualFold(strings.ToLower(command), "sftp (text UI)") {

			h, err := extractHost(hostName, configPath, folders)
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

				// Find the password in the passwords.json file for the hostname
				for _, p := range hostPasswords {

					if p.Host == hostName {
						// Decrypt the password if ecnrypted and store it in password variable
						password, err = readAndDecryptPassFromDB(hostName)
						if err != nil {
							return err
						}
						break
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
			if err := checkShellCommands(command); err != nil {
				fmt.Println(err.Error())
				return fmt.Errorf("command not found: %w", err)
			}

			cmd := *exec.Command(command, hostName)
			method := "key"
			password := `''`

			// Check if sshpass command is availble in the shell, needed for passsword authentication
			if err := checkShellCommands("sshpass"); err != nil {
				log.Println("sshpass is not installed, only key authentication is supported")
				passAuthSupported = false

			} else if passAuthSupported {

				// Find the password in the passwords.json file for the hostname
				for _, p := range hostPasswords {
					if p.Host == hostName {
						// Decrypt the password if ecnrypted and store it in password variable
						password, err = readAndDecryptPassFromDB(hostName)
						if err != nil {
							return err
						}
						break
					}
				}

				if password != `''` {
					cmd = *exec.Command("sshpass", "-p", password, strings.ToLower(command), "-o", "StrictHostKeyChecking=no", hostName)
					method = "password"
				}
			}

			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			fmt.Println("Trying:", method, "authentication")
			cmd.Run()
		}

	} else if chosen_type == "console" {
		fmt.Println("console")
		promptCommand := promptui.Select{
			Label: "Select Command",
			Size:  35,
			Items: []string{"Connect via cu", "Edit Profile", "Remove Profile"},
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

		if strings.ToLower(command) == "connect" {
			for _, c := range consoleConfigs {
				if c.Host == hostName {

					cmd := *exec.Command("sudo", "cu", "-s", c.BaudRate, "-l", c.Device)
					cmd.Stdin = os.Stdin
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					cmd.Run()
				}
			}
		} else if strings.ToLower(command) == "edit profile" {

			err := editConsoleProfile(hostName)
			if err != nil {
				fmt.Println(err)
				return fmt.Errorf("error editing console profile: %w", err)
			}

		} else if strings.ToLower(command) == "remove profile" {
			for _, c := range consoleConfigs {
				if c.Host == hostName {
					removeConsoleConfig(hostName)
				}
			}
		}
	} else if chosen_type == "folder" {
		if err := navigateToNext(chosen, folders, hosts, configPath); err != nil {
			return fmt.Errorf("error navigating to next folder: %w", err)
		}
	}

	return nil
}

func ExecTheUI(configPath string, folders Folders) error {

	hosts := getHosts(configPath, folders)
	items_to_show := getItems(hosts, folders, false)

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

	if err := navigateToNext(chosen, folders, hosts, configPath); err != nil {
		return fmt.Errorf("error navigating to next folder: %w", err)
	}

	return nil

}

func navigateToNext(chosen string, folders Folders, hosts []SSHConfig, configPath string) error {
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

		for _, f := range folders {
			if f.Name == CleanedChosen {
				for _, sshconfig := range hosts {
					if sshconfig.Folder == CleanedChosen {
						sshconfigInFolder = append(sshconfigInFolder, sshconfig)
					}
				}
			}
		}

		submenu_items := getItems(sshconfigInFolder, Folders{}, true)

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
		if err := Connect(submenu_chosen, configPath, folders, hosts); err != nil {
			return fmt.Errorf("error connecting to host '%s': %w", submenu_chosen, err)
		}
	} else {
		if err := Connect(chosen, configPath, folders, hosts); err != nil {
			return fmt.Errorf("error connecting to host '%s': %w", chosen, err)
		}
	}

	return nil
}

func getFolderNameFromDB(name string, folders Folders) string {

	for _, p := range folders {
		if slices.Contains(p.Profiles, name) {
			return p.Name
		}
	}
	return ""
}

func getHosts(sshConfigPath string, folders Folders) []SSHConfig {

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

			if fName := getFolderNameFromDB(host, folders); fName != "" {
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

func getItems(hosts []SSHConfig, folders Folders, isSubmenu bool) []string {

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

	for _, f := range folders {
		for _, host := range hosts {
			if host.Folder == f.Name {
				items = append(items, fmt.Sprintf("%s %s %s%s", folderIcon, magenta, f.Name, reset))
				break
			}
		}
	}

	sort.Strings(items)

	for _, host := range hosts {

		if len(host.HostName) == 0 {
			continue
		}

		isFlat := true
		for _, p := range folders {
			if host.Folder == p.Name {
				isFlat = false
			}
		}

		if !isFlat {
			continue
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
			item += " (HTTP PROXY)"
		}

		items = append(items, item)
	}

	if !isSubmenu {
		if len(consoleConfigs) > 0 {
			for _, c := range consoleConfigs {
				items = append(items, fmt.Sprintf("%s %v%-*s >%v %v", consoleIcon, yellow, maxHostLen, c.Host, reset, c.BaudRate))
			}
		}
	}

	return items
}

func customPanicHandler() {
	if r := recover(); r != nil {
		stack := debug.Stack()
		sanitizedStack := removePaths(stack)
		fmt.Printf("Panic: %v\n%s", r, sanitizedStack)
		os.Exit(1)
	}
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

func readConsoleProfile() (ConsoleConfigs, error) {

	if _, err := os.Stat(console_file_path); err != nil {
		fmt.Printf("failed to create/access the console file: %v\n", console_file_path)
		return consoleConfigs, fmt.Errorf("failed to create/access the console file: %v", console_file_path)
	}

	data, err := os.ReadFile(console_file_path)
	if err != nil {
		return consoleConfigs, fmt.Errorf("error reading the console file: %v", err)
	}

	if len(data) != 0 {
		err = json.Unmarshal(data, &consoleConfigs)
		if err != nil {
			fmt.Printf("error unmarshalling JSON: %v\n", err)
			return consoleConfigs, fmt.Errorf("error unmarshalling JSON: %v", err)
		}
	}

	return consoleConfigs, nil
}

func removeConsoleConfig(h string) {
	for i, v := range consoleConfigs {
		if v.Host == h {
			consoleConfigs = append(consoleConfigs[:i], consoleConfigs[i+1:]...)
		}
	}
}

func addConsoleConfig(profile ConsoleConfig) {

	f := consoleConfigs

	for i, v := range consoleConfigs {
		if v.Host == profile.Host {
			f = append(consoleConfigs[:i], consoleConfigs[i+1:]...)
		}
	}

	consoleConfigs = f
	consoleConfigs = append(consoleConfigs, profile)
}

func writeUpdatedConsoleDBToFile() error {

	content, err := json.MarshalIndent(consoleConfigs, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling JSON: %v", err)
	}

	err = os.WriteFile(console_file_path, content, 0644)
	if err != nil {
		return fmt.Errorf("error writing file: %v", err)
	}

	return nil
}

func encryptAndPushPassToDB(hostname, password string) error {
	if len(password) == 0 {
		return fmt.Errorf("password is empty")
	}

	if len(hostname) == 0 {
		return fmt.Errorf("host is empty")
	}

	encryptedString, err := encrypt([]byte(password))
	if err != nil {
		err = fmt.Errorf("error encrypting password: %v", err)
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the db transaction for password insertion:%v", err)
	}

	stmt, err := tx.Prepare("INSERT INTO credentials(host,password) VALUES(?,?) ON CONFLICT(host) DO UPDATE SET password = excluded.password;")
	if err != nil {
		return fmt.Errorf("failed to prepare the db for password insertion:%v", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(hostname, encryptedString)
	if err != nil {
		return fmt.Errorf("failed to insert the password for the host %v: %v", hostname, err)
	}

	fmt.Println("\nPassword has been successfully added to the password database!")
	return tx.Commit()
}

func readAndDecryptPassFromDB(host string) (string, error) {
	var password string

	query := "SELECT password FROM credentials WHERE host = ?"

	row := db.QueryRow(query, host)
	err := row.Scan(&password)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no password found for host: %s", host)
	} else if err != nil {
		return "", fmt.Errorf("read password query failed: %w", err)
	}

	clearTextPassword, err := decrypt(password)
	if err != nil {
		return `''`, fmt.Errorf("error decrypting password: %v", err)
	}

	return clearTextPassword, nil
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
	configPath, err := setupFilesFolders()
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	if err := initDB(filepath.Join(homeDir, ".ssh", "sshcli.db")); err != nil {
		fmt.Printf("Fatal error during database initialization: %v\n", err)
		return
	}
	defer db.Close()

	//########################################## CLI #####################################################
	// Here we read the cli aruguments
	consoleProfile, sshProfile, action, profileType := processCliArgs()
	//########################################## ENC #####################################################
	// Load or generate the encryption key in the db
	key, err = loadOrGenerateKey()
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	// Findout and set the full path for the folderdb.json file
	if folderdb, err = updateFilePath(folderdb); err != nil {
		fmt.Println(err.Error(), "Folder database not found, showing flat entries.")
	}

	// Read the passwords.json file
	// hostPasswords, err = readPassFile()
	err = loadCredentials()
	if err != nil {
		log.Printf("Can't read the password file: %v", err)
	}

	// Read the folderdb.json file
	folders, err := readFolderDb()
	if err != nil {
		fmt.Println("Can't read the folder database")
		folders = Folders{}
	}

	if runtime.GOOS == "darwin" && checkShellCommands("cu") == nil {
		if console_file_path, err = updateFilePath(console_file_path); err != nil {
			fmt.Println(err)
		}
		consoleConfigs, err = readConsoleProfile()
		if err != nil {
			fmt.Println("Can't read/access the console config file")
		}

		defer writeUpdatedConsoleDBToFile()
	}

	if *action != "" {
		if sshProfile.Host == "" {
			fmt.Println("Usage: -action [add|remove] -host HOST [other flags...]")
			return
		} else {
			doConfigBackup()
			switch strings.ToLower(*action) {
			case "add":
				switch strings.ToLower(profileType) {
				case "ssh":
					if err := updateSSHConfig(configPath, sshProfile); err != nil {
						fmt.Println("Error adding/updating profile:", err)
						return
					}

					if sshProfile.Password != "" {
						updatePasswordDB(sshProfile)
						if err := encryptAndPushPassToDB(sshProfile.Host, sshProfile.Password); err != nil {
							fmt.Println(err)
							return
						}
					}
				case "console":
					addConsoleConfig(consoleProfile)
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
					removeConsoleConfig(consoleProfile.Host)
				}
			default:
				fmt.Println("Invalid action. Use '-action add' or '-action remove'.")
			}
		}
	} else {
		if err := ExecTheUI(configPath, folders); err != nil {
			if !strings.Contains(err.Error(), "^C") {
				fmt.Println(err)
			}

			return
		}
	}
}
