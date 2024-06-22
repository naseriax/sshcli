package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/manifoldco/promptui"
)

var CompileTime = ""

type Host struct {
	Name     string
	HostName string
	User     string
}

type SSHConfigProfile struct {
	Host         string
	HostName     string
	User         string
	Port         string
	IdentityFile string
}

func formatProfile(profile SSHConfigProfile) string {
	config := fmt.Sprintf("Host %s\n", profile.Host)
	config += formatField("HostName", profile.HostName)
	config += formatField("User", profile.User)
	config += formatField("Port", profile.Port)
	config += formatField("IdentityFile", profile.IdentityFile)
	return config
}

func formatField(fieldName, fieldValue string) string {
	if fieldValue != "" {
		return fmt.Sprintf("    %s %s\n", fieldName, fieldValue)
	}
	return ""
}

func getProfile(host string) (SSHConfigProfile, error) {
	configFilePath := getSSHConfigPath()
	input, err := os.ReadFile(configFilePath)
	if err != nil {
		return SSHConfigProfile{}, err
	}

	lines := strings.Split(string(input), "\n")
	profile := SSHConfigProfile{
		Host: host,
	}
	inBlock := false

	for _, line := range lines {
		if strings.HasPrefix(line, "Host ") {
			if strings.TrimSpace(line) == fmt.Sprintf("Host %s", host) {
				inBlock = true
				profile.Host = host
				continue
			}
			inBlock = false
		}
		if inBlock {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "HostName ") {
				profile.HostName = strings.TrimSpace(strings.TrimPrefix(line, "HostName"))
			} else if strings.HasPrefix(line, "User ") {
				profile.User = strings.TrimSpace(strings.TrimPrefix(line, "User"))
			} else if strings.HasPrefix(line, "Port ") {
				profile.Port = strings.TrimSpace(strings.TrimPrefix(line, "Port"))
			} else if strings.HasPrefix(line, "IdentityFile ") {
				profile.IdentityFile = strings.TrimSpace(strings.TrimPrefix(line, "IdentityFile"))
			}
		}
	}
	return profile, nil
}

func processProfile(host string, profileProcessor func([]string, SSHConfigProfile) []string) error {
	configFilePath := getSSHConfigPath()
	input, err := os.ReadFile(configFilePath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(input), "\n")
	profile := SSHConfigProfile{Host: host}
	output := profileProcessor(lines, profile)

	outputString := strings.Join(output, "\n")
	if err := os.WriteFile(configFilePath, []byte(outputString), 0600); err != nil {
		return err
	}

	return nil
}

func removeProfile(host string) error {
	return processProfile(host, func(lines []string, profile SSHConfigProfile) []string {
		var output []string
		inBlock := false

		for _, line := range lines {
			if strings.HasPrefix(line, "Host ") {
				if strings.TrimSpace(line) == fmt.Sprintf("Host %s", host) {
					inBlock = true
					continue
				}
				inBlock = false
			}
			if !inBlock {
				output = append(output, line)
			}
		}
		return output
	})
}

func addOrUpdateProfile(newProfile SSHConfigProfile) error {
	configFilePath := getSSHConfigPath()
	input, err := os.ReadFile(configFilePath)
	if err != nil {
		return err
	}

	existingProfile, _ := getProfile(newProfile.Host)

	if newProfile.HostName != "" {
		existingProfile.HostName = newProfile.HostName
	}
	if newProfile.User != "" {
		existingProfile.User = newProfile.User
	}
	if newProfile.IdentityFile != "" {
		existingProfile.IdentityFile = newProfile.IdentityFile
	}
	if newProfile.Port != "" {
		existingProfile.Port = newProfile.Port
	}

	lines := strings.Split(string(input), "\n")
	var output []string
	inBlock := false
	found := false

	for _, line := range lines {
		if strings.HasPrefix(line, "Host ") {
			if inBlock {
				output = append(output, formatProfile(existingProfile))
				inBlock = false
				found = true
			}
			if strings.TrimSpace(line) == fmt.Sprintf("Host %s", existingProfile.Host) {
				inBlock = true
				continue
			}
		}
		if !inBlock {
			output = append(output, line)
		}
	}
	if !found {
		output = append(output, formatProfile(existingProfile))
	}

	outputString := strings.Join(output, "\n")
	if err := os.WriteFile(configFilePath, []byte(outputString), 0600); err != nil {
		return err
	}

	return nil
}

func getSSHConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".ssh", "config")
}

func processCliArgs() (SSHConfigProfile, *string) {
	action := flag.String("action", "", "Action to perform: add or remove")
	host := flag.String("host", "", "Host alias")
	hostname := flag.String("hostname", "", "HostName")
	user := flag.String("user", "", "User")
	port := flag.String("port", "", "Port")
	identityFile := flag.String("key", "", "IdentityFile path")
	version := flag.Bool("V", false, "prints the compile time")

	flag.Parse()

	if *version {
		fmt.Println("Compile time:", CompileTime)
	}

	profile := SSHConfigProfile{
		Host:         *host,
		HostName:     *hostname,
		User:         *user,
		Port:         *port,
		IdentityFile: *identityFile,
	}

	return profile, action
}

func doConfigBackup(sshConfigPath string) {
	backupCmd := exec.Command(fmt.Sprintf("cp %s %s.backup", sshConfigPath, sshConfigPath))
	backupCmd.Stderr = os.Stderr
	backupCmd.Run()
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

	sshConfigPath := getSSHConfigPath()

	profile, action := processCliArgs()

	if *action != "" {
		if profile.Host == "" {
			fmt.Println("Usage: -action [add|remove] -host HOST [other flags...]")
			return
		} else {
			doConfigBackup(sshConfigPath)
			switch *action {
			case "add":
				if err := addOrUpdateProfile(profile); err != nil {
					fmt.Println("Error adding/updating profile:", err)
				}
			case "remove":
				if err := removeProfile(profile.Host); err != nil {
					fmt.Println("Error removing profile:", err)
				}
			default:
				fmt.Println("Invalid action. Use 'add' or 'remove'.")
			}
		}
	} else {
		UIExec(sshConfigPath)
	}
}
