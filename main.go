package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/manifoldco/promptui"
)

var CompileTime = ""

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

// SSHConfig represents the configuration for an SSH host entry
type SSHConfig struct {
	Host         string
	HostName     string
	User         string
	Port         string
	IdentityFile string
}

type Host struct {
	Name     string
	HostName string
	User     string
}

// updateSSHConfig updates or adds an SSH host configuration in the ~/.ssh/config file
// It modifies only the fields provided in the input config, preserving other existing fields
func updateSSHConfig(config SSHConfig) error {
	// Get the user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".ssh", "config")

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

	return nil
}

func getSSHConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	return filepath.Join(homeDir, ".ssh", "config"), nil
}

func processCliArgs() (SSHConfig, *string) {
	action := flag.String("action", "add", "Action to perform: add or remove. default: add")
	// flag.StringVar(action, "A", "add", "Action to perform: add or remove. default: add")

	host := flag.String("host", "", "Host alias")
	// flag.StringVar(host, "H", "add", "Action to perform: add or remove. default: add")

	hostname := flag.String("hostname", "", "HostName or the IP address")
	// flag.StringVar(hostname, "I", "", "HostName or the IP address")

	username := flag.String("username", "root", "Username. default: root")
	// flag.StringVar(username, "U", "root", "Username. default: root")

	port := flag.String("port", "22", "Port number. default: 22")
	// flag.StringVar(port, "P", "22", "Port number. default: 22")

	identityFile := flag.String("key", "~/.ssh/id_rsa", "IdentityFile path. default: ~/.ssh/id_rsa")
	// flag.StringVar(identityFile, "K", "~/.ssh/id_rsa", "IdentityFile path. default: ~/.ssh/id_rsa")

	version := flag.Bool("V", false, "prints the compile time")

	flag.Parse()

	if *version {
		fmt.Println("Compile time:", CompileTime)
	}

	profile := SSHConfig{
		Host:         *host,
		HostName:     *hostname,
		User:         *username,
		Port:         *port,
		IdentityFile: *identityFile,
	}

	return profile, action
}

func UIExec(sshConfigPath string) {
	hosts := getHosts(sshConfigPath)
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
		fmt.Printf("Prompt failed %v\n", err)
		return
	}

	fmt.Printf("You chose %q\n", chosen)

	chosenParts := strings.Split(chosen, " ")
	if len(chosenParts) < 1 {
		fmt.Println("Invalid chosen item")
		return
	}
	hostName := chosenParts[0]

	promptCommand := promptui.Select{
		Label: "Select Command",
		Items: []string{"ssh", "sftp"},
		Templates: &promptui.SelectTemplates{
			Label:    "{{ . }}?",
			Active:   "\U0001F534 {{ . | cyan }} (press enter to select)",
			Inactive: "  {{ . | cyan }}",
			Selected: "\U0001F7E2 {{ . | red | cyan }}",
		},
	}

	_, command, err := promptCommand.Run()
	if err != nil {
		fmt.Printf("Prompt failed %v\n", err)
		return
	}

	cmd := exec.Command(command, hostName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

func getHosts(sshConfigPath string) []Host {
	file, err := os.Open(sshConfigPath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	var hosts []Host
	var currentHost *Host

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Host ") {
			hostName := strings.TrimPrefix(line, "Host ")
			if hostName == "*" {
				continue
			}
			if currentHost != nil {
				hosts = append(hosts, *currentHost)
			}
			currentHost = &Host{Name: hostName}
		} else if currentHost != nil {
			if strings.HasPrefix(line, "HostName ") {
				ip := strings.TrimPrefix(line, "HostName ")
				currentHost.HostName = ip
			} else if strings.HasPrefix(line, "User ") {
				user := strings.TrimPrefix(line, "User ")
				currentHost.User = user
			}
		}
	}

	if currentHost != nil && currentHost.Name != "*" {
		hosts = append(hosts, *currentHost)
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Name < hosts[j].Name
	})

	return hosts
}

func getItems(hosts []Host) []string {
	items := make([]string, len(hosts))
	for i, host := range hosts {
		items[i] = fmt.Sprintf("%s --> %s@%s", host.Name, host.User, host.HostName)
	}
	return items
}

func main() {

	sshConfigPath, err := getSSHConfigPath()
	if err != nil {
		log.Fatal(err)
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
				if err := updateSSHConfig(profile); err != nil {
					fmt.Println("Error adding/updating profile:", err)
					os.Exit(1)
				}
			case "remove":
				if err := deleteSSHProfile(profile.Host); err != nil {
					fmt.Println("Error removing profile:", err)
				}
			default:
				fmt.Println("Invalid action. Use '-action add' or '-action remove'.")
			}
		}
	} else {
		UIExec(sshConfigPath)
	}
}
