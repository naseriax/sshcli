package main

import (
	"bufio"
	"bytes"
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

	"github.com/manifoldco/promptui"
)

const (
	red         = "\033[31m"
	green       = "\033[32m"
	reset       = "\033[0m"
	pinkonbrown = "\033[38;2;255;82;197;48;2;155;106;0m"
)

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
	if runtime.GOOS == "windows" {
		// Check for common CLI editors first
		editors := []string{"nano", "vim", "notepad"}
		for _, editor := range editors {
			if _, err := exec.LookPath(editor); err == nil {
				return editor
			}
		}
	}

	// Fall back to OS-specific defaults
	switch runtime.GOOS {
	case "windows":
		return "notepad"
	default:
		return "vi"
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
	// Create a temporary file
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

func processCliArgs() (SSHConfig, *string) {
	action := flag.String("action", "", "Action to perform: add, remove")

	host := flag.String("host", "", "Host alias")

	hostname := flag.String("hostname", "", "HostName or IP address")

	password := flag.String("password", "", "SSH password")

	username := flag.String("username", "", "Username")

	port := flag.String("port", "", "Port Number")

	identityFile := flag.String("key", "", "IdentityFile path")

	version := flag.Bool("version", false, "prints the compile time (version)")

	flag.Parse()

	if *version {
		fmt.Println("Compile time:", CompileTime)
	}

	profile := SSHConfig{
		Host:         *host,
		HostName:     *hostname,
		Password:     *password,
		User:         *username,
		Port:         *port,
		IdentityFile: *identityFile,
	}

	if len(profile.IdentityFile) > 0 {
		cleanPath := fixKeyPath(profile.IdentityFile)
		profile.IdentityFile = cleanPath
	}

	return profile, action
}

type FileNode struct {
	Name     string
	IsDir    bool
	Children []*FileNode
	Parent   *FileNode // Add this line
}

func ExecTheUI(configPath string) {

	hosts := getHosts(configPath)
	items := getItems(hosts)

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

	// fmt.Printf("You chose %q\n", chosen)

	chosenParts := strings.Split(chosen, " ")
	if len(chosenParts) < 1 {
		fmt.Println("Invalid item")
		return
	}

	hostName := chosenParts[0]

	promptCommand := promptui.Select{
		Label: "Select Command",
		Items: []string{"ssh", "sftp (os native)", "sftp (text UI)", "ping", "Edit Profile", "Remove Profile"},
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

	} else if strings.EqualFold(command, "ping") {
		h, err := extractHost(hostName, configPath)
		if err != nil {
			log.Fatalln(err)
		}
		cmd := *exec.Command(command, h.HostName)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	} else if strings.EqualFold(command, "sftp (text UI)") {
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

		sftp_ui.INIT_SFTP(h.Host, h.HostName, h.User, h.Password, h.Port, h.IdentityFile)

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
				if p.Host != hostName {
					continue
				}

				// Decrypt the password if ecnrypted and store it in password variable
				password, err = EncryptOrDecryptPassword(p.Host, key, "dec")
				if err != nil {
					log.Println(err.Error())
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

func main() {

	defer customPanicHandler()

	defer writeUpdatedPassDbToFile()

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
		log.Fatalln(err.Error())
	}

	profile, action := processCliArgs()

	if *action != "" {
		if profile.Host == "" {
			fmt.Println("Usage: -action [add|remove] -host HOST [other flags...]")
			return
		} else {
			doConfigBackup()
			switch *action {
			case "add":
				if err := updateSSHConfig(configPath, profile); err != nil {
					fmt.Println("Error adding/updating profile:", err)
					os.Exit(1)
				}

				if profile.Password != "" {
					updatePasswordDB(profile)
				}

			case "remove":
				if err := deleteSSHProfile(profile.Host); err != nil {
					fmt.Println("Error removing profile:", err)
				}

				hostPasswords = removeValue(hostPasswords, profile.Host)

			default:
				fmt.Println("Invalid action. Use '-action add' or '-action remove'.")
			}
		}
	} else {
		ExecTheUI(configPath)
	}

}
