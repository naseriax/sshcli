package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/manifoldco/promptui"
)

type Host struct {
	Name     string
	HostName string
	User     string
}

func main() {

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error getting home directory:", err)
		return
	}

	sshConfigPath := filepath.Join(homeDir, ".ssh", "config")

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
			if currentHost != nil {
				hosts = append(hosts, *currentHost)
			}
			hostName := strings.TrimPrefix(line, "Host ")
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

	if currentHost != nil {
		hosts = append(hosts, *currentHost)
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	sort.Slice(hosts, func(i, j int) bool {
		return hosts[i].Name < hosts[j].Name
	})

	items := make([]string, len(hosts))
	for i, host := range hosts {
		items[i] = fmt.Sprintf("%s --> %s@%s", host.Name, host.User, host.HostName)
	}

	prompt := promptui.Select{
		Label: "Select Host",
		Items: items,
		Size:  35,
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

	// Extract the host name from the chosen item
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
