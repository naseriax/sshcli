package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"sshcli/pkgs/sftp_ui"
	"strings"
	"syscall"

	"github.com/manifoldco/promptui"
	"golang.org/x/term"
)

const (
	red         = "\033[31m"
	green       = "\033[32m"
	reset       = "\033[0m"
	pinkonbrown = "\033[38;2;255;82;197;48;2;155;106;0m"
)

var console_file_path = "console.json"
var consoleConfigs ConsoleConfigs

var CompileTime = ""
var passAuthSupported = true
var key []byte

// SSHConfig represents the configuration for an SSH host entry
type SSHConfig struct {
	Host         string
	HostName     string
	User         string
	Port         string
	IdentityFile string
	Password     string
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
}

type ConsoleConfigs []ConsoleConfig

func doConfigBackup() error {
	configFilePath, err := getSSHConfigPath()
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
	tmpfile, err := os.CreateTemp("", "ssh-profile-*.txt")
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
			log.Fatalf("the file has no valid ssh/sftp profile")
		}

		if newHost.Host != "" {
			deleteSSHProfile(newHost.Host)
			updateSSHConfig(configPath, newHost)

			if newHost.Host == config.Host {
				fmt.Printf("Modified profile for %s\n", profileName)
			} else {
				fmt.Printf("A new profile with a the name:%s has been added\n", newHost.Host)
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
				log.Fatalf("error unmarshalling JSON for consoleConfigs: %v", err)
			}
		}

		fmt.Println(c)

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
			// Parse and potentially update each field in the host block
			field, value := parseField(trimmedLine)
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
	configPath, err := getSSHConfigPath()
	if err != nil {
		log.Fatal(err)
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

	hostPasswords = removeValue(hostPasswords, host)

	return nil
}

func createFile(filePath string) error {
	file, err := os.Create(filePath)
	if err != nil {
		fmt.Println("Error creating file:", err)
		return err
	}
	defer file.Close()

	fmt.Printf("The %v file created successfully\n", filePath)
	return nil
}

func handleExitSignal(err error) {
	if strings.EqualFold(strings.TrimSpace(err.Error()), "^C") {
		fmt.Println("Closed the prompt.")
	} else {
		fmt.Printf("Prompt failed %v\n", err)
	}
}

func getSSHConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	d := filepath.Join(homeDir, ".ssh")
	p := filepath.Join(d, "config")
	c := filepath.Join(d, "console.json")

	if _, err := os.Stat(d); err != nil {
		log.Fatalf("failed to find/access the config file path: %v", d)
	}

	if _, err := os.Stat(p); err != nil {
		fmt.Printf("failed to find/access the config file path: %v. Trying to create it...\n", d)

		if err := createFile(p); err != nil {
			log.Fatalf("failed to create/access the config file path: %v", d)
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

	if runtime.GOOS == "darwin" && checkShellCommands("cu") == nil {
		if _, err := os.Stat(c); err != nil {
			fmt.Printf("failed to find/access the console file path: %v. Trying to create it...\n", c)

			if err := createFile(c); err != nil {
				log.Fatalf("failed to create/access the console file path: %v", c)
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

	profileType := flag.String("type", "ssh", "profile type, can be ssh or console, default is ssh")

	flag.Parse()

	if *version {
		fmt.Println(CompileTime[1:])
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

	consoleProfile := ConsoleConfig{
		Host:     *host,
		BaudRate: *BaudRate,
		Device:   *device,
		Parity:   *Parity,
		StopBit:  *StopBit,
		DataBits: *DataBits,
	}

	profile := SSHConfig{
		Host:         *host,
		HostName:     *hostname,
		Password:     passString,
		User:         *username,
		Port:         *port,
		IdentityFile: *identityFile,
	}

	if len(profile.IdentityFile) > 0 {
		cleanPath := fixKeyPath(profile.IdentityFile)
		profile.IdentityFile = cleanPath
	}

	return consoleProfile, profile, action, *profileType
}

func ExecTheUI(configPath string) {

	hosts := getHosts(configPath)
	items := getItems(hosts)
	if len(consoleConfigs) > 0 {
		for _, c := range consoleConfigs {
			items = append(items, fmt.Sprintf("%s %v>%v %s", c.Host, green, reset, c.BaudRate))
		}
	}

	searcher := func(input string, index int) bool {
		item := items[index]
		name := strings.Replace(strings.ToLower(item), " ", "", -1)
		input = strings.Replace(strings.ToLower(input), " ", "", -1)

		return strings.Contains(name, input)
	}

	prompt := promptui.Select{
		Label:    "Select Host",
		Searcher: searcher,
		Items:    items,
		Size:     35,
		Templates: &promptui.SelectTemplates{
			Label:    "{{ . }}?",
			Active:   "\U0001F534 {{ . | cyan }} (press enter to select)",
			Inactive: "  {{ . | cyan }}",
			Selected: "\U0001F7E2 {{ . | red | cyan }}",
		},
	}

	_, chosen, err := prompt.Run()

	if err != nil {
		handleExitSignal(err)
		return
	}

	chosen_type := "ssh"

	chosenParts := strings.Split(chosen, " ")
	if len(chosenParts) < 1 {
		fmt.Println("Invalid item")
		return
	}

	if !strings.Contains(chosen, "@") {
		chosen_type = "console"
	}

	hostName := chosenParts[0]

	if chosen_type == "ssh" {
		promptCommand := promptui.Select{
			Label: "Select Command",
			Size:  35,
			Items: []string{"ssh", "sftp (os native)", "sftp (text UI)", "Ping", "TCPing", "Edit Profile", "Set Password", "Reveal Password", "Remove Profile"},
			Templates: &promptui.SelectTemplates{
				Label:    "{{ . }}?",
				Active:   "\U0001F534 {{ . | cyan }} (press enter to select)",
				Inactive: "  {{ . | cyan }}",
				Selected: "\U0001F7E2 {{ . | red | cyan }}",
			},
		}

		_, command, err := promptCommand.Run()
		if err != nil {
			handleExitSignal(err)
			return
		}

		if strings.EqualFold(command, "Edit profile") {
			editProfile(hostName, configPath)

		} else if strings.EqualFold(command, "Remove profile") {
			deleteSSHProfile(hostName)

		} else if strings.EqualFold(command, "Reveal Password") {
			password, err := EncryptOrDecryptPassword(hostName, key, "dec")
			if err != nil {
				log.Println(err.Error())
			}
			fmt.Println("Password for", hostName, ":", password)
			os.Exit(0)

		} else if strings.EqualFold(command, "Set Password") {

			fmt.Print("\nEnter password: ")
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				fmt.Println("Error reading password:", err)
				os.Exit(1)
			}

			passString := string(bytePassword)

			profile := SSHConfig{
				Host:     hostName,
				Password: passString,
			}

			updatePasswordDB(profile)
			writeUpdatedPassDbToFile()
			fmt.Println("\nPassword has been successfully added to the password database!")

		} else if strings.EqualFold(command, "ping") {
			h, err := extractHost(hostName, configPath)
			if err != nil {
				log.Fatalln(err)
			}
			cmd := *exec.Command(strings.ToLower(command), h.HostName)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()
		} else if strings.EqualFold(command, "tcping") {
			if err := checkShellCommands(strings.ToLower(command)); err != nil {
				log.Println("tcping is not installed. install by checking https://github.com/pouriyajamshidi/tcping")

			} else {
				port := "22"
				h, err := extractHost(hostName, configPath)
				if err != nil {
					log.Fatalln(err)
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
		} else if strings.EqualFold(strings.ToLower(command), "sftp (text UI)") {

			h, err := extractHost(hostName, configPath)
			if err != nil {
				log.Fatalln(err)
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
						password, err = EncryptOrDecryptPassword(p.Host, key, "dec")
						if err != nil {
							log.Println(err.Error())
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
				log.Fatalln(err)
			}

		} else {
			if strings.EqualFold(command, "sftp (os native)") {
				command = "sftp"
			}

			// make sure sftp or ssh commands are availble in the shell
			if err := checkShellCommands(command); err != nil {
				log.Fatalln(err.Error())
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
						password, err = EncryptOrDecryptPassword(p.Host, key, "dec")
						if err != nil {
							log.Println(err.Error())
						}
						break
					}
				}

				if password != `''` {
					cmd = *exec.Command("sshpass", "-p", password, command, hostName)
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

		promptCommand := promptui.Select{
			Label: "Select Command",
			Size:  35,
			Items: []string{"Connect via cu", "Edit Profile", "Remove Profile"},
			Templates: &promptui.SelectTemplates{
				Label:    "{{ . }}?",
				Active:   "\U0001F534 {{ . | cyan }} (press enter to select)",
				Inactive: "  {{ . | cyan }}",
				Selected: "\U0001F7E2 {{ . | red | cyan }}",
			},
		}

		_, command, err := promptCommand.Run()
		if err != nil {
			handleExitSignal(err)
			return
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

			editConsoleProfile(hostName)

		} else if strings.ToLower(command) == "remove profile" {
			for _, c := range consoleConfigs {
				if c.Host == hostName {
					removeConsoleConfig(hostName)
				}
			}

		}

	}

}

func getHosts(sshConfigPath string) []SSHConfig {
	file, err := os.Open(sshConfigPath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	var hosts []SSHConfig
	var currentHost *SSHConfig

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Host ") {
			host := strings.TrimPrefix(line, "Host ")
			if host == "*" {
				continue
			}
			if currentHost != nil {
				hosts = append(hosts, *currentHost)
			}
			currentHost = &SSHConfig{Host: host}
		} else if currentHost != nil {
			if strings.HasPrefix(line, "HostName ") {
				currentHost.HostName = strings.TrimPrefix(line, "HostName ")
			} else if strings.HasPrefix(line, "User ") {
				currentHost.User = strings.TrimPrefix(line, "User ")
			} else if strings.HasPrefix(line, "Port ") {
				currentHost.Port = strings.TrimPrefix(line, "Port ")
			} else if strings.HasPrefix(line, "IdentityFile ") {
				currentHost.IdentityFile = strings.TrimPrefix(line, "IdentityFile ")
			}
		}
	}

	if currentHost != nil && currentHost.Host != "*" {
		hosts = append(hosts, *currentHost)
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Host < hosts[j].Host
	})

	return hosts
}

func getItems(hosts []SSHConfig) []string {
	items := make([]string, len(hosts))

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

	for i, host := range hosts {

		if len(host.HostName) > 0 && len(host.User) > 0 && len(host.Port) > 0 && host.Port != "22" {
			items[i] = fmt.Sprintf("%-*s %v>%v %-*s%v@%v %s -p %v", maxHostLen, host.Host, green, reset, maxUserLen, host.User, red, reset, host.HostName, host.Port)
		} else if len(host.HostName) > 0 && len(host.User) > 0 {
			items[i] = fmt.Sprintf("%-*s %v>%v %-*s%v@%v %s", maxHostLen, host.Host, green, reset, maxUserLen, host.User, red, reset, host.HostName)
		} else if len(host.HostName) > 0 {
			items[i] = fmt.Sprintf("%-*s %v>%v %s", maxHostLen, host.Host, green, reset, host.HostName)
		} else {
			items[i] = host.Host
		}

	}

	return items
}

func customPanicHandler() {
	if r := recover(); r != nil {
		// Get the stack trace
		stack := debug.Stack()

		// Remove file paths from the stack trace
		sanitizedStack := removePaths(stack)

		// Log or print the sanitized stack trace
		fmt.Printf("Panic: %v\n%s", r, sanitizedStack)

		// Optionally, exit the program
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

		log.Fatalf("failed to create/access the console file: %v", console_file_path)
	}

	data, err := os.ReadFile(console_file_path)
	if err != nil {
		return consoleConfigs, fmt.Errorf("error reading the console file: %v", err)
	}

	if len(data) != 0 {
		err = json.Unmarshal(data, &consoleConfigs)
		if err != nil {
			log.Fatalf("error unmarshalling JSON: %v", err)
			return consoleConfigs, fmt.Errorf("error unmarshalling JSON: %v", err)
		}
	}

	return consoleConfigs, nil
}

// func addConsoleProfileToJsonProfiles(c ConsoleConfig, consoleConfigs *ConsoleConfigs) {

// 	*consoleConfigs = append(*consoleConfigs, c)
// }

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

func main() {

	defer customPanicHandler()

	defer writeUpdatedPassDbToFile()
	homeDir, _ := os.UserHomeDir()
	file, err := os.OpenFile(homeDir+"/sshcli.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	log.SetOutput(file)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	configPath, err := getSSHConfigPath()
	if err != nil {
		log.Fatal(err)
	}

	// Load or generate the encryption key
	key, err = loadOrGenerateKey()
	if err != nil {
		log.Fatalln(err.Error())
	}

	// Findout and set the full path for the passwords.json file
	if dataFile, err = updateFilePath(dataFile); err != nil {
		fmt.Println(err.Error(), "only key authentication is supported.")
		passAuthSupported = false
	}

	// Read the JSON file
	hostPasswords, err = readPassFile()
	if err != nil {
		log.Fatalln(err)
	}

	if console_file_path, err = updateFilePath(console_file_path); err != nil {
		fmt.Println(err)
	}

	if runtime.GOOS == "darwin" && checkShellCommands("cu") == nil {

		consoleConfigs, err = readConsoleProfile()
		if err != nil {
			log.Fatalln(err)
		}
	}

	consoleProfile, sshProfile, action, profileType := processCliArgs()
	defer writeUpdatedConsoleDBToFile()

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
						os.Exit(1)
					}

					if sshProfile.Password != "" {
						updatePasswordDB(sshProfile)
						writeUpdatedPassDbToFile()
						fmt.Println("\nPassword has been successfully added to the password database!")
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

					hostPasswords = removeValue(hostPasswords, sshProfile.Host)

				case "console":
					removeConsoleConfig(consoleProfile.Host)
				}
			default:
				fmt.Println("Invalid action. Use '-action add' or '-action remove'.")
			}
		}
	} else {
		ExecTheUI(configPath)
	}
}
