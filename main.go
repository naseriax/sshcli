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
	"regexp"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/charmbracelet/x/term"
	_ "modernc.org/sqlite"
)

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
			url TEXT,
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

	// Add url column
	rows, err := db.Query("PRAGMA table_info(sshprofiles)")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var columnExists bool
	for rows.Next() {
		var (
			cid      int
			name     string
			dataType string
			notNull  int
			dfltVal  sql.NullString
			pk       int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &dfltVal, &pk); err != nil {
			log.Fatal(err)
		}
		if name == "url" {
			columnExists = true
			break
		}
	}

	if !columnExists {
		_, err := db.Exec("ALTER TABLE sshprofiles ADD COLUMN url TEXT")
		if err != nil {
			log.Fatalf("failed to add url column: %v", err)
		}
		fmt.Println("Successfully added the 'url' column.")
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

func readFolderForHostFromDB(host string) (string, error) {
	var folder string

	query := "SELECT folder FROM sshprofiles WHERE host = ? AND folder IS NOT NULL"

	row := db.QueryRow(query, host)
	err := row.Scan(&folder)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("host not found in folder query: %s", host)
	} else if err != nil {
		return "", fmt.Errorf("read folder query failed: %w", err)
	}

	return folder, nil
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

func removeConsoleConfig(h string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the db transaction for console profile deletion: %v", err)
	}

	stmt, err := tx.Prepare("DELETE FROM console_profiles WHERE host = ?;")
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(h)
	if err != nil {
		return err
	}

	fmt.Printf("Console profile for %s has been removed successfully.\n", h)

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

func doConfigBackup(mode string) {

	if mode == "all" || mode == "config" {

		configFilePath, err := setupFilesFolders()
		if err != nil {
			log.Println("failed to get the config file path")
		}

		backupConfigFilePath := configFilePath + "_backup"
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
		backupDatabaseFile := databaseFile + "_backup"
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

func openBrowser(url string) error {
	var err error = nil
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}

	return err
}

func checkShellCommands(c string) error {
	if _, err := exec.LookPath(c); err != nil {
		return fmt.Errorf("%v command is not available in the default system shell", c)
	}
	return nil
}

func getDefaultEditor() string {
	editors := []string{"vim", "nvim", "vi", "nano"}

	if runtime.GOOS == "windows" {
		return "notepad"

	} else {
		for _, editor := range editors {
			if _, err := exec.LookPath(editor); err == nil {
				return editor
			}
		}
	}

	return "vi"
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

		for _, env := range os.Environ() {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) != 2 {
				continue
			}
			name, value := parts[0], parts[1]
			keyPath = strings.ReplaceAll(keyPath, "%"+name+"%", value)
		}
	}

	return keyPath
}

func cleanTheString(s, mode string) string {

	// remove colors
	s = strings.ReplaceAll(s, magenta, "")
	s = strings.ReplaceAll(s, green, "")
	s = strings.ReplaceAll(s, red, "")
	s = strings.ReplaceAll(s, yellow, "")
	s = strings.ReplaceAll(s, reset, "")
	s = strings.ReplaceAll(s, BOLD, "")

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

func cleanupDatabase() error {

	allHosts, err := getHosts()
	if err != nil {
		fmt.Println(err)
	}

	allHosts.removeItemFromStruct("*")
	placeholders := make([]string, len(*allHosts))
	for i := range *allHosts {
		placeholders[i] = "?"
	}

	args := make([]any, len(*allHosts))
	for i, v := range *allHosts {
		args[i] = v.Host
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(fmt.Sprintf("DELETE FROM sshprofiles WHERE host NOT IN (%s)", strings.Join(placeholders, ",")))
	if err != nil {
		return err
	}
	defer stmt.Close()

	result, err := stmt.Exec(args...)
	if err != nil {
		return err
	}

	rowsDeleted, _ := result.RowsAffected()
	fmt.Printf("Deleted %d rows.\n", rowsDeleted)

	return tx.Commit()
}

func processCliArgs() (ConsoleConfig, *string) {
	host := flag.String("host", "", "Host alias")
	action := flag.String("action", "", "Action to perform: add, remove")
	BaudRate := flag.String("baudrate", "9600", "BaudRate, default is 9600")
	DataBits := flag.String("data_bits", "8", "databits, default is 8")
	StopBit := flag.String("stop_bit", "1", "stop bit, default is 1")
	Parity := flag.String("parity", "none", "parity, default is none")
	device := flag.String("device", "/dev/tty.usbserial-1140", "device path, default is /dev/tty.usbserial-1140")
	version := flag.Bool("version", false, "prints the compile time (version)")
	sanitizeDB := flag.Bool("cleanup", false, "delete sqlite records that are not in the ssh config file")
	secure := flag.Bool("secure", false, "Masks the sensitive data")
	sql := flag.Bool("sql", false, "Direct access to the sshcli.db file to run sql queries")

	flag.Parse()

	if *version {
		fmt.Println(CompileTime[1:])
		os.Exit(0)
	}

	if *sanitizeDB {
		var err error
		configPath, err = setupFilesFolders()
		if err != nil {
			log.Fatalln(err.Error())
		}
		doConfigBackup("all")
		if err := cleanupDatabase(); err != nil {
			fmt.Println(err)
		}
		os.Exit(0)
	}

	if *sql {
		OpenSqlCli()
		db.Close()
		os.Exit(0)
	}

	if *secure {
		isSecure = true
	}

	var consoleProfile ConsoleConfig

	consoleProfile = ConsoleConfig{
		Host:     *host,
		BaudRate: *BaudRate,
		Device:   *device,
		Parity:   *Parity,
		StopBit:  *StopBit,
		DataBits: *DataBits,
	}

	return consoleProfile, action
}

func customPanicHandler() {
	if r := recover(); r != nil {
		stack := debug.Stack()

		lines := bytes.Split(stack, []byte("\n"))
		for i, line := range lines {
			if idx := bytes.LastIndex(line, []byte("/go/")); idx != -1 {
				lines[i] = line[idx+4:]
			} else if idx := bytes.Index(line, []byte(":")); idx != -1 {
				lines[i] = line[idx:]
			}
		}
		sanitizedStack := bytes.Join(lines, []byte("\n"))
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
		if err := createFile(p); err != nil {
			fmt.Printf("%sfailed to create/access the config file : %v%s", red, p, reset)
			return "", fmt.Errorf("failed to create/access the config file: %w", err)
		}

		file, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			log.Println(err)
			return "", fmt.Errorf("Failed to read the config file: %w", err)
		}
		defer file.Close()

		writer := bufio.NewWriter(file)
		defaultHosts := AllConfigs{
			SSHConfig{
				Host:         "vm1",
				HostName:     "172.16.0.1",
				User:         "root",
				Port:         "22",
				IdentityFile: filepath.Join(homeDir, ".ssh", "id_rsa"),
			}}

		textBlock := defaultHosts.constructConfigContent()
		for _, line := range textBlock {
			fmt.Fprintln(writer, line)
		}

		if err := writer.Flush(); err != nil {
			return "", fmt.Errorf("failed to write SSH config file: %w", err)
		}
	}

	return p, nil
}

func (s *AllConfigs) removeItemFromStruct(hostToRemove string) {
	for i, c := range *s {
		if c.Host == hostToRemove {
			*s = append((*s)[:i], (*s)[i+1:]...)
			return
		}
	}
}

func (s *AllConfigs) AddSocksToProfile(hostName string) error {
	var socksPort string
	fmt.Print("Enter Socks5 port # (eg. 1080) or press enter to assign automatically: ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		socksPort = scanner.Text()
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "Error reading the socks port from stdin:", err)
	}

	if len(socksPort) == 0 {
		//read https_proxy value and use use it
		p, err := findAvailablePort()
		if err != nil {
			return err
		}
		socksPort = strconv.Itoa(p)
	} else {
		p, err := strconv.Atoi(socksPort)
		if err != nil {
			return fmt.Errorf("wrong port number: %s", socksPort)
		}
		if p < 0 || p > 65535 {
			return fmt.Errorf("port number out of range: %s", socksPort)
		}
	}

	if h := s.extractHost(hostName); h != nil {
		h.DynamicSocks = append(h.DynamicSocks, "DynamicForward "+socksPort)
		if err := s.pushConfigToFile(); err != nil {
			return fmt.Errorf("error adding/updating profile: %w", err)
		}
		return nil
	} else {
		return fmt.Errorf("error extracting host: %s", hostName)
	}
}

func (s *AllConfigs) constructConfigContent() []string {
	configLines := []string{}
	for _, c := range *s {
		configLines = append(configLines, "Host "+c.Host)

		if len(c.HostName) != 0 {
			configLines = append(configLines, "    HostName "+c.HostName)
		}
		if len(c.User) != 0 {
			configLines = append(configLines, "    User "+c.User)
		}
		if len(c.Port) != 0 {
			configLines = append(configLines, "    Port "+c.Port)
		}
		if len(c.IdentityFile) != 0 {
			configLines = append(configLines, "    IdentityFile "+c.IdentityFile)
		}
		if len(c.Proxy) != 0 {
			configLines = append(configLines, "    ProxyCommand "+c.Proxy)
		}
		if len(c.Sockets) != 0 {
			for _, socket := range c.Sockets {
				configLines = append(configLines, "    "+socket)
			}
		}

		if len(c.DynamicSocks) != 0 {
			for _, s := range c.DynamicSocks {
				configLines = append(configLines, "    "+s)
			}
		}
		if len(c.OtherAttribs) != 0 {
			for _, o := range c.OtherAttribs {
				configLines = append(configLines, "    "+o)
			}
		}
		configLines = append(configLines, "")
	}

	return configLines
}

func (s *AllConfigs) pushConfigToFile() error {
	for _, newC := range *s {
		if strings.Contains(newC.Host, " ") {
			log.Printf("can't use whitespace in ssh profile name: %s", newC.Host)
			os.Exit(1)
		}
	}

	file, err := os.OpenFile(configPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("failed to create SSH config file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, line := range s.constructConfigContent() {
		fmt.Fprintln(writer, line)
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to write SSH config file: %w", err)
	}

	return nil
}

func getHosts() (*AllConfigs, error) {

	allHosts := &AllConfigs{}

	var currentHost *SSHConfig

	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("Failed to read the config file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if value, ok := strings.CutPrefix(line, "Host "); ok {
			if strings.Contains(value, " ") {
				continue
			}

			host := value

			// This means the host was already filled with content and now we are at the new host line
			if currentHost != nil {
				*allHosts = append(*allHosts, *currentHost)
			}

			// So we Generate a new host
			currentHost = &SSHConfig{Host: host}

			if fName, err := readFolderForHostFromDB(host); err == nil && fName != "NULL" && fName != "" {
				currentHost.Folder = fName
			}

		} else if currentHost != nil {
			if value, ok := strings.CutPrefix(line, "HostName "); ok {
				currentHost.HostName = value
			} else if value, ok := strings.CutPrefix(line, "User "); ok {
				currentHost.User = value
			} else if value, ok := strings.CutPrefix(line, "Port "); ok {
				currentHost.Port = value
			} else if value, ok := strings.CutPrefix(line, "ProxyCommand "); ok {
				currentHost.Proxy = value
			} else if value, ok := strings.CutPrefix(line, "IdentityFile "); ok {
				currentHost.IdentityFile = value
			} else if strings.Contains(line, "RemoteForward ") || strings.Contains(line, "LocalForward ") {
				currentHost.Sockets = append(currentHost.Sockets, line)
			} else if strings.Contains(line, "DynamicForward ") {
				currentHost.DynamicSocks = append(currentHost.DynamicSocks, line)
			} else {
				if len(strings.TrimSpace(line)) > 0 {
					currentHost.OtherAttribs = append(currentHost.OtherAttribs, value)
				}
			}
		}
	}

	if currentHost != nil {
		*allHosts = append(*allHosts, *currentHost)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("getHosts Scanner error: %w", err)
	}

	sort.Slice(*allHosts, func(i, j int) bool {
		return (*allHosts)[i].Host < (*allHosts)[j].Host
	})

	return allHosts, nil
}

func (s *AllConfigs) getFolderList() ([]string, error) {

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

func (s *AllConfigs) readUrlFromDb(host string) (string, error) {
	var currentUrl sql.NullString
	query := "SELECT url FROM sshprofiles WHERE host = ?"
	row := db.QueryRow(query, host)

	err := row.Scan(&currentUrl)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("READ_URL host: %s not found", host)
		}

		return "", fmt.Errorf("error reading url for profile %v from db: %v", host, err)
	}

	if currentUrl.Valid {
		return currentUrl.String, nil
	} else {
		return "", nil
	}
}

func (s *AllConfigs) readNoteforHost(host string, needString bool) (string, error) {
	var currentNotes sql.NullString
	query := "SELECT note FROM sshprofiles WHERE host = ?"
	row := db.QueryRow(query, host)

	err := row.Scan(&currentNotes)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("READ-NOTE host: %s not found", host)
		}

		return "", fmt.Errorf("error reading note for profile %v from db: %v", host, err)
	}

	if currentNotes.Valid {
		if needString {
			decryptedNote, err := decrypt(currentNotes.String)
			if err != nil {
				if strings.Contains(err.Error(), "illegal base64 data at input byte") {
					decryptedNote = currentNotes.String
				} else {
					log.Println(err)
				}
			}

			return decryptedNote, nil
		} else {
			return "ok", nil
		}
	} else {
		return "", nil
	}
}

func (s *AllConfigs) getItems(folder string) []string {

	items := make([]string, 0)

	maxHostLen := 1
	maxUserLen := 1

	for _, host := range *s {
		if !strings.EqualFold(host.Folder, folder) {
			continue
		}

		if len(host.Host) > maxHostLen {
			maxHostLen = len(host.Host)
		}

		if len(host.User) > maxUserLen {
			maxUserLen = len(host.User)
		}
	}

	maxHostLen += 1
	maxUserLen += 1

	if folder == "" {

		folderlist, err := s.getFolderList()
		if err != nil {
			log.Println("Error getting folder list:", err)
		}

		for _, f := range folderlist {
			for _, host := range *s {
				if host.Folder == f {
					items = append(items, fmt.Sprintf("%s %s %s%s", folderIcon, magenta, f, reset))
					break
				}
			}
		}

		sort.Strings(items)
	}

	connectionItems := map[string]SSHConfig{}
	connectionItemsNewFormat := make([]string, 0)

	maxRowLength := 1

	for i, host := range *s {
		if host.Host == "*" {
			continue
		}

		if folder != "" && host.Folder != folder {
			continue
		}

		if folder == "" {
			if f, err := readFolderForHostFromDB(host.Host); (err == nil) && (f != "NULL" && f != "") {
				(*s)[i].Folder = f
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
		if len(item) > maxRowLength {
			maxRowLength = len(item)
		}
		connectionItems[item] = host

	}

	for item, host := range connectionItems {

		if host.Host == "*" {
			continue
		}

		item += fmt.Sprintf(" %s ", strings.Repeat(" ", maxRowLength-len(item)))

		if len(host.Proxy) > 0 {
			item += "(" + hasProxy + ")"
		} else {
			item += "(  )"
		}

		if len(host.Sockets) > 0 {
			item += "(" + hasTun + ")"
		} else {
			item += "(  )"
		}

		if len(host.DynamicSocks) > 0 {
			item += "(" + hasSocks + ")"
		} else {
			item += "(  )"
		}

		if url, err := s.readUrlFromDb(host.Host); err == nil && len(url) > 0 {
			item += "(" + hasUrl + ")"
		} else {
			item += "(  )"
		}

		if val, _ := s.readNoteforHost(host.Host, false); val == "ok" {
			item += "(" + hasNote + " )"
		} else {
			item += "(  )"
		}

		if val, _ := s.readAndDecryptPassFromDB(host.Host, false); val == "ok" {
			item += "(" + hasPassword + ")"
		} else {
			item += "(  )"
		}

		connectionItemsNewFormat = append(connectionItemsNewFormat, item)
	}

	sort.Strings(connectionItemsNewFormat)
	items = append(items, connectionItemsNewFormat...)

	consoleItems := make([]string, 0)
	if checkShellCommands("cu") == nil && folder == "" {

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

	if folder == "" {
		items = append([]string{sshIcon + " New SSH Profile"}, items...)
	}

	if folder != "" {
		items = append([]string{goback}, items...)
	}

	return items
}

func (s *AllConfigs) navigateToNext(chosen string) error {

	if chosen == goback {
		if err := s.InitUi(""); err != nil {
			if !strings.Contains(err.Error(), "^C") {
				return fmt.Errorf("failed to back to the parent ui")
			} else {
				return fmt.Errorf(`
      (o o)
--oOO--(_)--OOo--
 Have a nice day!
                    			`)
			}
		}
	}

	chosen_type := ""

	if strings.Contains(chosen, folderIcon) {
		chosen_type = "folder"
	} else if strings.Contains(chosen, consoleIcon) {
		chosen_type = "console"
	} else if strings.Contains(chosen, sshIcon) {
		chosen_type = "ssh"
	} else {
		return fmt.Errorf("unknown entry")
	}

	if chosen_type == "folder" {
		CleanedChosen := cleanTheString(chosen, "all")

		if err := s.InitUi(CleanedChosen); err != nil {
			if !strings.Contains(err.Error(), "^C") {
				return fmt.Errorf(`
      (o o)
--oOO--(_)--OOo--
 Have a nice day!
                    			`)
			}
			return err
		}
	} else {
		if chosen == sshIcon+" New SSH Profile" {
			if err := s.editProfile("", "new"); err != nil {
				return fmt.Errorf("failed to create a new profile: %w", err)
			}

		} else {
			if err := s.Connect(chosen); err != nil {
				chosenParts := strings.Split(chosen, " ")
				return fmt.Errorf("'%s': %w", chosenParts[1], err)
			}
		}
	}

	return nil
}

func (s *AllConfigs) moveToFolder(host string) error {

	FolderList, err := s.getFolderList()
	if err != nil {
		log.Println("failed to retrieve the folder list:%w", err)
	}

	FolderList = append(FolderList, "New Folder")
	FolderList = append(FolderList, "Rename Folder")
	FolderList = append(FolderList, "Remove from folder")
	folderName := ""

	folderName, err = main_ui(FolderList, "Select the folder to move to:\n\n", false)
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
			return fmt.Errorf("invalid folder name")
		}

		if slices.Contains(FolderList, name) {
			return fmt.Errorf("folder with name %s already exists", name)
		}

		new_folder_name = name

		if strings.EqualFold(folderName, "Rename folder") {
			currentFolder, err = readFolderForHostFromDB(host)
			if err != nil {
				return fmt.Errorf("error reading folder for host from db: %w", err)
			}
		}

	} else if strings.EqualFold(folderName, "Remove from folder") {
		new_folder_name = "NULL"
	} else {
		new_folder_name = folderName
	}

	if err := s.updateProfileFolder(host, new_folder_name, currentFolder); err != nil {
		return err
	}

	return nil
}

func (s *AllConfigs) updateProfileFolder(hostname, newFolderName, currentFolderName string) error {

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

func (s *AllConfigs) updateNotesAndPushToDb(host string) error {

	tmpfile, err := os.CreateTemp("", "ssh-profile-note-*.md")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}

	defer os.Remove(tmpfile.Name())

	writer := bufio.NewWriter(tmpfile)

	currentNote, err := s.readNoteforHost(host, true)
	if err != nil {
		if strings.Contains(err.Error(), "READ-NOTE") {
			currentNote = ""
		} else {
			return err
		}
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

	updateQuery := "INSERT INTO sshprofiles (host, note) VALUES (?, ?) ON CONFLICT(host) DO UPDATE SET note = excluded.note;"

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the db transaction for note update:%v", err)
	}

	stmt, err := tx.Prepare(updateQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare update statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(host, note)
	if err != nil {
		return fmt.Errorf("failed to update note for host %s: %w", host, err)
	}

	return tx.Commit()
}

func (s *AllConfigs) writeUrlDb(hostname string) error {
	var url string

	if len(hostname) == 0 {
		return fmt.Errorf("host is empty")
	}

	if h := s.extractHost(hostname); h != nil {
		suggestion := "https://" + h.HostName + ":443"

		fmt.Printf("Enter the url (eg: %s ): ", suggestion)
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			url = scanner.Text()
		}

		if err := scanner.Err(); err != nil {
			fmt.Fprintln(os.Stderr, "Error reading the url from stdin:", err)
		}

		if len(url) == 0 {
			return fmt.Errorf("url is empty")
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin the db transaction for url insertion:%v", err)
		}

		stmt, err := tx.Prepare("INSERT INTO sshprofiles(host,url) VALUES(?,?) ON CONFLICT(host) DO UPDATE SET url = excluded.url;")
		if err != nil {
			return fmt.Errorf("failed to prepare the db for url insertion:%v", err)
		}
		defer stmt.Close()

		_, err = stmt.Exec(hostname, url)
		if err != nil {
			return fmt.Errorf("failed to insert the url for the host %v: %v", hostname, err)
		}

		log.Println("\nurl has been successfully added to the url database!")
		return tx.Commit()
	} else {
		return fmt.Errorf("failed to extract host %s", hostname)
	}
}

func (s *AllConfigs) extractHost(host string) *SSHConfig {
	for i, h := range *s {
		if h.Host == host {
			return &(*s)[i]
		}
	}

	return nil
}

func (s *AllConfigs) AddForwardSocketToProfile(hostName string) error {
	var err error
	var target_socket string
	_, err = findAvailablePort()
	if err != nil {
		return err
	}
	fmt.Println("\n- Syntax: \"<FowardType> <localport> <remotehost>:<remoteport>\"")
	fmt.Println("- Acceptable ForwardTypes: local, remote")
	fmt.Printf("- Enter the tunnel config (eg. %s\"remote 57001 172.16.0.45:22\"%s): ", green, reset)
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
	// if s, ok := strings.CutPrefix(strings.TrimSpace(target_socket), "remote"); ok {
	// 	target_socket = "RemoteForward " + s
	// } else if s, ok := strings.CutPrefix(strings.TrimSpace(target_socket), "local"); ok {
	// 	target_socket = "LocalForward " + s
	// }

	target_socket = strings.Replace(strings.Replace(strings.ToLower(target_socket), "remote", "RemoteForward", 1), "remoteforward", "RemoteForward", 1)
	target_socket = strings.Replace(strings.Replace(strings.ToLower(target_socket), "local", "LocalForward", 1), "localforward", "LocalForward", 1)

	if !isSocketValid(target_socket) {
		return fmt.Errorf("provided socket is not valid: %s", target_socket)
	}

	if h := s.extractHost(hostName); h != nil {
		h.Sockets = append(h.Sockets, target_socket)

		if err := s.pushConfigToFile(); err != nil {
			return fmt.Errorf("error adding/updating profile: %w", err)
		}
		return nil

	} else {
		return fmt.Errorf("error extracting host: %w", err)
	}
}

func findAvailablePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	fmt.Printf("\n - Found available local port: %s%s%d%s.\n", BOLD, blue, port, reset)
	return port, nil
}

func isSocketValid(s string) bool {

	pattern := `^(?i)(RemoteForward|LocalForward)\s+((?:[a-zA-Z0-9.-]+|\[[a-fA-F0-9:]+\]):)?(\d+)\s+([a-zA-Z0-9][a-zA-Z0-9.-]*|\[[a-fA-F0-9:]+\]):(\d+)$`
	re := regexp.MustCompile(pattern)
	if re.MatchString(s) {
		return true
	}

	return false
}

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

func (s *AllConfigs) AddProxyToProfile(hostName string) error {
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
		return err
	}

	if runtime.GOOS == "darwin" {
		proxy = "nc -X connect -x " + cleanProxy + " %h %p"
	} else {
		proxy = "ncat --proxy " + cleanProxy + " --proxy-type http %h %p"
	}

	if h := s.extractHost(hostName); h != nil {
		h.Proxy = proxy
		if err := s.pushConfigToFile(); err != nil {
			return fmt.Errorf("error adding/updating profile: %w", err)
		}
		return nil
	} else {
		return fmt.Errorf("error extracting host: %s", hostName)
	}
}

func (s *AllConfigs) DeleteProxyFromProfile(hostName string) error {

	if h := s.extractHost(hostName); h != nil {
		h.Proxy = ""

		if err := s.pushConfigToFile(); err != nil {
			return fmt.Errorf("error adding/updating profile: %w", err)
		}
		return nil
	} else {
		return fmt.Errorf("error extracting host %s: ", hostName)
	}

}

func (s *AllConfigs) DeleteFordwardSocketFromProfile(hostName string) error {

	if h := s.extractHost(hostName); h != nil {
		h.Sockets = []string{}

		if err := s.pushConfigToFile(); err != nil {
			return fmt.Errorf("error adding/updating profile: %w", err)
		}

		return nil
	} else {
		return fmt.Errorf("error extracting host %s: ", hostName)
	}
}

func (s *AllConfigs) DeleteSocksFromProfile(hostName string) error {

	if h := s.extractHost(hostName); h != nil {
		h.DynamicSocks = []string{}

		if err := s.pushConfigToFile(); err != nil {
			return fmt.Errorf("error adding/updating profile: %w", err)
		}

		return nil
	} else {
		return fmt.Errorf("error extracting host %s: ", hostName)
	}
}

func (s *AllConfigs) updateHostNameInDatabase(oldhostname, newhostname string) error {
	updateQuery := "UPDATE sshprofiles SET host = ? WHERE host = ?"

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the db transaction for note update:%v", err)
	}
	stmt, err := tx.Prepare(updateQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare update statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(newhostname, oldhostname)
	if err != nil {
		return fmt.Errorf("failed to update hostname for host %s: %w", oldhostname, err)
	}

	return tx.Commit()
}

func (s *AllConfigs) editProfile(profileName, mode string) error {

	// ############### Let's create a temp file to host our config to be presented to the user
	tmpfile, err := os.CreateTemp("", "ssh-profile-*.md")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpfile.Name())

	// ############### config is the one user will see in the editor to modify.
	// If it's a new creation, the content is like below.
	// If it's a modify operation, the content comes from the s.
	config := &SSHConfig{}

	if mode == "new" {
		config = &SSHConfig{
			Host:         "new-profile",
			HostName:     "10.10.10.10",
			User:         "root",
			Port:         "22",
			IdentityFile: "~/.ssh/id_rsa",
		}
	} else {
		if config = s.extractHost(profileName); config == nil {
			return fmt.Errorf("Host %s not found", profileName)
		}
	}
	// ############### Let's create the text block from our config profile.
	newAllProfile := AllConfigs{
		*config,
	}
	profileContent := newAllProfile.constructConfigContent()

	// ############### Let's write the profile to the temp file.
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

	// ############### Now that we have the temp file, let's open it via the available editors
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

	// ############### Let's wait for the user to exit from the editor to see if there was any edit or not.
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("editor exited with error: %v", err)
	}

	// ############### Let's get the temp file status to check if there was any modification
	fileInfoAfter, err := os.Stat(tmpfile.Name())
	if err != nil {
		return fmt.Errorf("failed to get file info after editing: %v", err)
	}

	// ############### If the file was modified, do these:
	if fileInfoAfter.ModTime().After(fileInfoBefore.ModTime()) {
		// Read the modified file and create a sshconfig from it
		originalConfigPath := configPath
		configPath = tmpfile.Name()
		newHosts, err := getHosts()
		if err != nil {
			return fmt.Errorf("failed to read temp config file: %w", err)
		}
		configPath = originalConfigPath

		newHost := SSHConfig{}

		if len(*newHosts) > 0 {
			newHost = (*newHosts)[0]
		} else {
			return fmt.Errorf("the profile is not valid")
		}

		//newHost is the sshconfig after modifiation
		//config is the sshconfig before modification

		if newHost.Host == config.Host && mode != "new" {
			// This means it's modification of an existing profile. (Host name is not changed)
			fmt.Printf("The profile %s has been altered!\n", profileName)
			fmt.Println("- Details:")
			if !slices.Equal(config.Sockets, newHost.Sockets) {
				fmt.Printf("    + Sockets\n      %s%+v%s \n      to\n      %s%+v%s\n", red, config.Sockets, reset, green, newHost.Sockets, reset)
			}
			if !slices.Equal(config.DynamicSocks, newHost.DynamicSocks) {
				fmt.Printf("    + DynamicForward\n      %s%+v%s \n      to\n      %s%+v%s\n", red, config.DynamicSocks, reset, green, newHost.DynamicSocks, reset)
			}
			if !slices.Equal(config.OtherAttribs, newHost.OtherAttribs) {
				fmt.Printf("    + Other Attributes\n      %s%+v%s \n      to\n      %s%+v%s\n", red, config.OtherAttribs, reset, green, newHost.OtherAttribs, reset)
			}
			if config.HostName != newHost.HostName {
				fmt.Printf("    + HostName %s%s%s to %s%s%s\n", red, config.HostName, reset, green, newHost.HostName, reset)
			}
			if config.User != newHost.User {
				fmt.Printf("    + User %s%s%s to %s%s%s\n", red, config.User, reset, green, newHost.User, reset)
			}
			if config.Port != newHost.Port {
				fmt.Printf("    + Port from %s%s%s to %s%s%s\n", red, config.Port, reset, green, newHost.Port, reset)
			}
			if config.Proxy != newHost.Proxy {
				fmt.Printf("    + Proxy %s%s%s to %s%s%s\n", red, config.Proxy, reset, green, newHost.Proxy, reset)
			}
			if config.IdentityFile != newHost.IdentityFile {
				fmt.Printf("    + IdentityFile %s%s%s to %s%s%s\n", red, config.IdentityFile, reset, green, newHost.IdentityFile, reset)
			}

			// delete the old host from the config file
			s.removeItemFromStruct(config.Host)
			*s = append(*s, newHost)

		} else {

			// This means it's the Host name has changed, so we need decide if we need overwrite the old one or create a new one with the new name.
			whatToDo := "new"
			items := []string{"Overwrite Host", fmt.Sprintf("Save and duplicate as %s%s%s", green, newHost.Host, reset)}

			// If it's not a new profile creation (NEW SSH PROFILE in the menu)
			if mode != "new" {
				//Find the folder of the modifed profile
				foldername, err := readFolderForHostFromDB(config.Host)
				if err != nil {
					if !strings.Contains(err.Error(), "host not found in folder query") {
						return fmt.Errorf("failed to read folder for %v:%w", config.Host, err)
					}
				} else {
					newHost.Folder = foldername
					if err := s.updateProfileFolder(newHost.Host, newHost.Folder, ""); err != nil {
						log.Printf("failed to push the new host's (%v) folder to the database:%v", newHost.Host, err.Error())
					}
				}

				whatToDo, err = main_ui(items, "Overwrite or Duplicate?\n\n", false)
				// fmt.Println(whatToDo)
				if err != nil {
					handleExitSignal(err)
					return fmt.Errorf("error selecting option: %w", err)
				}
			}
			// If Overwrite is chosen by the user
			if whatToDo == items[0] {
				if err := s.updateHostNameInDatabase(config.Host, newHost.Host); err != nil {
					return fmt.Errorf("failed to change the host column from %s to %s: %w", config.Host, newHost.Host, err)
				}

				detailsPrinted := false
				fmt.Printf("The profile %s%s%s has been renamed to %s%s%s\n", red, config.Host, reset, green, newHost.Host, reset)

				if !slices.Equal(config.Sockets, newHost.Sockets) {
					if !detailsPrinted {
						fmt.Println("- Details:")
						detailsPrinted = true
					}
					fmt.Printf("    + Sockets\n      %s%+v%s \n      to\n      %s%+v%s\n", red, config.Sockets, reset, green, newHost.Sockets, reset)
				}
				if !slices.Equal(config.OtherAttribs, newHost.OtherAttribs) {
					if !detailsPrinted {
						fmt.Println("- Details:")
						detailsPrinted = true
					}
					fmt.Printf("    + Other attributes\n      %s%+v%s \n      to\n      %s%+v%s\n", red, config.OtherAttribs, reset, green, newHost.OtherAttribs, reset)
				}
				if !slices.Equal(config.DynamicSocks, newHost.DynamicSocks) {
					if !detailsPrinted {
						fmt.Println("- Details:")
						detailsPrinted = true
					}
					fmt.Printf("    + DynamicForward\n      %s%+v%s \n      to\n      %s%+v%s\n", red, config.DynamicSocks, reset, green, newHost.DynamicSocks, reset)
				}
				if config.HostName != newHost.HostName {
					if !detailsPrinted {
						fmt.Println("- Details:")
						detailsPrinted = true
					}
					fmt.Printf("    + HostName %s%s%s to %s%s%s\n", red, config.HostName, reset, green, newHost.HostName, reset)
				}
				if config.User != newHost.User {
					if !detailsPrinted {
						fmt.Println("- Details:")
						detailsPrinted = true
					}
					fmt.Printf("    + User %s%s%s to %s%s%s\n", red, config.User, reset, green, newHost.User, reset)
				}
				if config.Port != newHost.Port {
					if !detailsPrinted {
						fmt.Println("- Details:")
						detailsPrinted = true
					}
					fmt.Printf("    + Port from %s%s%s to %s%s%s\n", red, config.Port, reset, green, newHost.Port, reset)
				}
				if config.Proxy != newHost.Proxy {
					if !detailsPrinted {
						fmt.Println("- Details:")
						detailsPrinted = true
					}
					fmt.Printf("    + Proxy %s%s%s to %s%s%s\n", red, config.Proxy, reset, green, newHost.Proxy, reset)
				}
				if config.IdentityFile != newHost.IdentityFile {
					if !detailsPrinted {
						fmt.Println("- Details:")
						detailsPrinted = true
					}
					fmt.Printf("    + IdentityFile %s%s%s to %s%s%s\n", red, config.IdentityFile, reset, green, newHost.IdentityFile, reset)
				}

				// delete the old host from the config file
				s.removeItemFromStruct(config.Host)

			} else {
				fmt.Printf("A new profile:%s has been added\n", newHost.Host)
			}
			*s = append(*s, newHost)
		}
		if err := s.pushConfigToFile(); err != nil {
			return fmt.Errorf("failed to push the updated profile: %w", err)
		}
	} else {
		fmt.Printf("Profile %s was not modified.\n", profileName)
	}

	return nil
}

func (s *AllConfigs) Connect(chosen string) error {
	chosen_type := ""
	chosen = cleanTheString(chosen, "onlyColors")

	chosenParts := strings.Split(chosen, " ")

	if len(chosenParts) < 1 {
		return fmt.Errorf("invalid item selected")
	}

	if strings.Contains(chosen, consoleIcon) {
		chosen_type = "console"
	} else if strings.Contains(chosen, sshIcon) {
		chosen_type = "ssh"
	} else {
		return fmt.Errorf("unknown entry")
	}

	hostName := chosenParts[1]

	switch chosen_type {
	case "ssh":
		command, err := main_ui(getSubMenuContent(), "", true)
		if err != nil {
			return err
		}

		if command == goback {
			if h := s.extractHost(hostName); h != nil {
				if err := s.InitUi(h.Folder); err != nil {
					if !strings.Contains(err.Error(), "^C") {
						return fmt.Errorf("failed to back to the parent ui")
					} else {
						return fmt.Errorf(`
      (o o)
--oOO--(_)--OOo--
 Have a nice day!
                    			`)
					}

				}
			} else {
				return fmt.Errorf("Can't find the host: %s%s%s in the list", red, hostName, reset)
			}
		}

		command = cleanTheString(command, "keyboard")

		if strings.EqualFold(command, "Set Folder") {
			if err := s.moveToFolder(hostName); err != nil {
				return fmt.Errorf("Failed to set folder for %s: %w", hostName, err)
			}
		} else if strings.EqualFold(command, "Notes") {
			if err := s.updateNotesAndPushToDb(hostName); err != nil {
				log.Println(err)
			}
		} else if strings.EqualFold(command, "Duplicate/Edit Profile") {
			if err := s.editProfile(hostName, "edit"); err != nil {
				return err
			}
		} else if strings.EqualFold(command, "Remove Profile") {
			s.removeItemFromStruct(hostName)
			if err := s.removefromDatabase(hostName); err != nil {
				return fmt.Errorf("failed to remove %s from database: %w", hostName, err)
			}
			if err := s.pushConfigToFile(); err != nil {
				return fmt.Errorf("error adding/updating profile: %w", err)
			}
		} else if strings.EqualFold(command, "Set URL") {

			if err := s.writeUrlDb(hostName); err != nil {
				return fmt.Errorf("failed to push the url to db for the host %s:%w", hostName, err)
			}

		} else if strings.EqualFold(command, "Open in Browser") {
			if url, err := s.readUrlFromDb(hostName); err == nil {
				if len(url) > 0 {
					return openBrowser(url)
				} else {
					if h := s.extractHost(hostName); h != nil {
						return openBrowser("https://" + (*h).HostName)
					}
				}
			}
		} else if strings.EqualFold(command, "Reveal Password") {
			password, err := s.readAndDecryptPassFromDB(hostName, true)
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

		} else if strings.EqualFold(command, "Set Socks Tunnel") {
			if err := s.AddSocksToProfile(hostName); err != nil {
				return err
			}
		} else if strings.EqualFold(command, "Set http proxy") {
			if err := s.AddProxyToProfile(hostName); err != nil {
				return err
			}
		} else if strings.EqualFold(command, "Remove http proxy") {
			if err := s.DeleteProxyFromProfile(hostName); err != nil {
				return err
			}
		} else if strings.EqualFold(command, "Remove SSH Tunnel") {
			if err := s.DeleteFordwardSocketFromProfile(hostName); err != nil {
				return err
			}
		} else if strings.EqualFold(command, "Remove Socks Tunnel") {
			if err := s.DeleteSocksFromProfile(hostName); err != nil {
				return err
			}
		} else if strings.EqualFold(command, "Set Password") {
			fmt.Print("\nEnter password: ")
			bytePassword, err := term.ReadPassword(uintptr(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("error reading password: %w", err)
			}

			passString := string(bytePassword)

			if err := encryptAndPushPassToDB(hostName, passString); err != nil {
				return fmt.Errorf("failed to push the password to db in set password for the host %v:%v", hostName, err)
			}
		} else if strings.EqualFold(command, "ping") {
			if h := s.extractHost(hostName); h != nil {
				cmd := *exec.Command(strings.ToLower(command), h.HostName)
				cmd.Stdin = os.Stdin
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				cmd.Run()
			} else {
				return fmt.Errorf("error extracting host: %s", hostName)
			}

		} else if strings.EqualFold(command, "tcping") {
			if err := checkShellCommands(strings.ToLower(command)); err != nil {
				log.Println("tcping is not installed. install by checking https://github.com/pouriyajamshidi/tcping")
				fmt.Println("tcping is not installed. install by checking https://github.com/pouriyajamshidi/tcping")
			} else {
				if h := s.extractHost(hostName); h != nil {

					if h.Port != "" {
						port = h.Port
					}
					cmd := *exec.Command(strings.ToLower(command), h.HostName, port)
					cmd.Stdin = os.Stdin
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					cmd.Run()
				} else {
					return fmt.Errorf("error extracting host: %s", hostName)
				}
			}
		} else if strings.EqualFold(command, "ssh-copy-id") {
			if err := checkShellCommands("ssh-copy-id"); err != nil {
				fmt.Println(err.Error())
				return fmt.Errorf("ssh-copy-id command not found: %w", err)
			}

			if h := s.extractHost(hostName); h != nil {
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

				h.IdentityFile = fixKeyPath(h.IdentityFile)

				password, err := s.readAndDecryptPassFromDB(hostName, true)
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
			} else {
				return fmt.Errorf("error extracting host: %s", hostName)
			}

		} else if strings.EqualFold(command, "sftp (text UI)") {
			if h := s.extractHost(hostName); h != nil {
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

					password, err = s.readAndDecryptPassFromDB(hostName, true)
					if err != nil {
						if strings.Contains(err.Error(), "no password") {
							password = ""
						} else {

							return err
						}
					}
				}

				err = INIT_SFTP(h.Host, h.HostName, h.User, password, h.Port, h.IdentityFile)
				if err != nil {
					if strings.Contains(err.Error(), "methods [none], no supported methods remain") {
						fmt.Printf("\n - Can't authenticate to the server. no password or key provided. \n\n")
					} else if strings.Contains(err.Error(), "methods [none password], no supported methods remain") {
						fmt.Printf("\n - Can't authenticate to the server. the provided password is wrong and no key provided. \n\n")
					} else {
						fmt.Println(err.Error())
					}
					return fmt.Errorf("error initializing SFTP: %w", err)
				}

			} else {
				return fmt.Errorf("error extracting host: %s", hostName)
			}
		} else {
			if strings.EqualFold(command, "set ssh tunnel") {
				if err := s.AddForwardSocketToProfile(hostName); err != nil {
					return fmt.Errorf("failed to add ForwardSocket:, %w", err)
				}

				items := []string{
					"Yes",
					"No",
				}

				userAction, err := main_ui(items, "Connect to the jumpserver now?\n\n", false)
				if err != nil {
					return err
				}

				if userAction == "Yes" {
					command = "ssh"
				} else {
					os.Exit(0)
				}

			}
			if strings.EqualFold(command, "sftp (os native)") {
				command = "sftp"
			}

			if err := checkShellCommands(strings.ToLower(command)); err != nil {
				return fmt.Errorf("command not found: %w", err)
			}

			cmd := *exec.Command(strings.ToLower(command), hostName)
			password := `''`

			if h := s.extractHost(hostName); h != nil {
				if len(h.Proxy) > 0 {
					tool := "nc"
					if runtime.GOOS != "darwin" {
						tool = "ncat"
					}
					if err := checkShellCommands(tool); err != nil {
						return fmt.Errorf("%s is not installed on your machine. remove the http_proxy from the profile to connect", tool)
					}
				}
				if err := checkShellCommands("sshpass"); err != nil {
					log.Println("sshpass is not installed!")
					passAuthSupported = false

				} else if passAuthSupported {
					password, err = s.readAndDecryptPassFromDB(hostName, true)
					if err != nil {
						if strings.Contains(err.Error(), "no password") {
							password = ""
						} else {

							return err
						}
					}

					if password != "" {
						cmd = *exec.Command("sshpass", "-p", password, strings.ToLower(command), "-o", "StrictHostKeyChecking=no", hostName)
					} else {
						cmd = *exec.Command(strings.ToLower(command), "-o", "StrictHostKeyChecking=no", hostName)
					}
				}

				cmd.Stdin = os.Stdin
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				cmd.Run()

			} else {
				return fmt.Errorf("error extracting host: %s", hostName)
			}
		}
	case "console":

		items := []string{"Connect via cu", "Duplicate/Edit Profile", "Remove Profile"}

		command, err := main_ui(items, "", false)
		if err != nil {
			handleExitSignal(err)
			return fmt.Errorf("error running command prompt: %w", err)
		}

		consoleProfile, err := readConsoleProfileFromDb(hostName)
		if err != nil {
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
				return fmt.Errorf("error editing console profile: %w", err)
			}

		} else if strings.ToLower(command) == "remove profile" {
			err := removeConsoleConfig(hostName)
			if err != nil {
				return fmt.Errorf("error deleting console profile: %w", err)
			}
		}
	}

	return nil
}

func (s *AllConfigs) removefromDatabase(hostname string) error {

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
		fmt.Printf("ℹ️ No record found for host in the database'%s'.\n", hostname)
	} else {
		fmt.Printf("🗑️ Successfully deleted %d record(s) '%s'.\n", rowsAffected, hostname)
	}

	return tx.Commit()

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

func generateConsoleHostBlock(config ConsoleConfig) []byte {

	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Printf("error marshalling JSON: %v", err)
	}

	return content
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

func (s *AllConfigs) InitUi(folder string) error {

	items_to_show := s.getItems(folder)
	chosen, err := main_ui(items_to_show, msg, false)
	if err != nil {
		return err
	}

	if err := s.navigateToNext(chosen); err != nil {
		return err
	}

	return nil
}

func main() {

	/* custompanicHandler hides developer's filesystem information from the stack trace
	and only shows the panic message */
	defer customPanicHandler()

	//########################################## LOG #####################################################
	// all log.Printx() operations are written in ~/sshcli.log file instead of being printed on the screen
	homeDir, _ := os.UserHomeDir()
	logFilePath := filepath.Join(homeDir, "sshcli.log")
	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println(err)
	} else {
		defer file.Close()
		log.SetOutput(file)
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	//########################################## DBS #####################################################
	// Here we initialize the database
	d := filepath.Join(homeDir, ".ssh")
	if _, err := os.Stat(d); err != nil {
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
	consoleProfile, action := processCliArgs()

	//########################################## SSH #####################################################

	configPath, err = setupFilesFolders()
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	//########################################## ENC #####################################################
	// Check if there is encryption key inside the sqlite db.
	// If no encryption key file found, generate a new one and store in the sqlite db.
	key, err = loadOrGenerateKey()
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	doConfigBackup("all")

	allHosts, err := getHosts()

	if *action != "" {
		if consoleProfile.Host == "" {
			fmt.Println("Usage: -action [add|remove] -host HOST [other flags...]")
			return
		} else {
			switch strings.ToLower(*action) {
			case "add":
				if err := addConsoleConfig(consoleProfile); err != nil {
					fmt.Println("Error adding console profile:", err)
					return
				}
			case "remove":
				if err := removeConsoleConfig(consoleProfile.Host); err != nil {
					fmt.Println("Error removing console profile:", err)
					return
				}
			default:
				fmt.Println("Invalid action. Use '-action add' or '-action remove'.")
			}
		}
	} else {
		if err := allHosts.InitUi(""); err != nil {
			if !strings.Contains(err.Error(), "^C") {
				fmt.Println(err)
			}
			return
		}
	}
}
