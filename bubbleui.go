package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type ssh_model struct {
	choices  []string
	selected map[int]string
	cursor   int
	choice   string
}

func (m ssh_model) Init() tea.Cmd {
	return nil
}

func (m ssh_model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}

		case "enter", " ":
			m.choice = m.choices[m.cursor]
			return m, tea.Quit

		// Keyboard shortcuts
		case "s":
			m.choice = "SSH"
			return m, tea.Quit
		case "n":
			m.choice = "notes"
			return m, tea.Quit
		case "p":
			m.choice = "Set Password"
			return m, tea.Quit
		case "t":
			m.choice = "SFTP (text UI)"
			return m, tea.Quit
		case "o":
			m.choice = "SFTP (os native)"
			return m, tea.Quit
		case "i":
			m.choice = "Ping"
			return m, tea.Quit
		case "c":
			m.choice = "TCPing"
			return m, tea.Quit
		case "d":
			m.choice = "Duplicate/Edit Profile"
			return m, tea.Quit
		case "h":
			m.choice = "Set http proxy"
			return m, tea.Quit
		case "f":
			m.choice = "Set Folder"
			return m, tea.Quit
		case "r":
			m.choice = "Reveal Password"
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m ssh_model) View() string {
	var s strings.Builder

	s.WriteString(fmt.Sprintf("%sShortcuts:\n%ss%s=SSH %sn%s=notes %sp%s=Set Password %st%s=SFTP(text UI) %so%s=SFTP(os native) %si%s=ping\n%sc%s=TCPing %sd%s=Duplicate/Edit Profile %sh%s=Set http proxy %sf%s=Set Folder %sr%s=Reveal Password%s\n\n",
		yellow,
		yellow, reset,
		yellow, reset,
		yellow, reset,
		yellow, reset,
		yellow, reset,
		yellow, reset,
		yellow, reset,
		yellow, reset,
		yellow, reset,
		yellow, reset,
		yellow, reset,
		reset))

	for i, choice := range m.choices {
		cursor := " "
		if m.cursor == i {
			cursor = fmt.Sprintf(" %s>%s", green, reset)
		}

		// Highlight based on shortcuts
		var color string
		switch choice {
		case "SSH":
			color = green
		case "notes":
			color = blue
		case "Set Password":
			color = red
		case "SFTP (text UI)":
			color = magenta
		case "SFTP (os native)":
			color = orange
		default:
			color = reset
		}

		s.WriteString(fmt.Sprintf("%s %s%s%s\n", cursor, color, choice, reset))
	}

	s.WriteString(fmt.Sprintf("\n%sPress shortcut key, arrows+Enter, or q to quit.%s\n", yellow, reset))

	return s.String()
}

func runBubbleTeaMenu() (string, error) {
	p := tea.NewProgram(ssh_model{
		choices: []string{
			"SSH",
			"SFTP (os native)",
			"SFTP (text UI)",
			"Ping",
			"TCPing",
			"ssh-copy-id",
			"Duplicate/Edit Profile",
			"Set Password",
			"Set http proxy",
			"Set SSH Tunnel",
			"Set Folder",
			"notes",
			"Reveal Password",
			"Remove SSH Tunnel",
			"Remove http proxy",
			"Remove Profile",
		},
		selected: make(map[int]string),
	})
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	if m, ok := finalModel.(ssh_model); ok {
		return m.choice, nil
	}
	return "", fmt.Errorf("unexpected model type")
}

func b_ui() (string, error) {
	choice, err := runBubbleTeaMenu()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if choice != "" {
		return choice, nil
	} else {
		return "", fmt.Errorf("empty choice error")
	}
}
