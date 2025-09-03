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

		case "up":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}

		case "enter", " ":
			m.choice = m.choices[m.cursor]
			return m, tea.Quit

		// Keyboard shortcuts
		case "s":
			m.choice = fmt.Sprintf("%s(s)%s ssh", yellow, reset)
			return m, tea.Quit
		case "n":
			m.choice = fmt.Sprintf("%s(n)%s Notes", yellow, reset)
			return m, tea.Quit
		case "p":
			m.choice = fmt.Sprintf("%s(p)%s Set Password", yellow, reset)
			return m, tea.Quit
		case "t":
			m.choice = fmt.Sprintf("%s(t)%s sftp (text UI)", yellow, reset)
			return m, tea.Quit
		case "o":
			m.choice = fmt.Sprintf("%s(o)%s sftp (os native)", yellow, reset)
			return m, tea.Quit
		case "i":
			m.choice = fmt.Sprintf("%s(i)%s ping", yellow, reset)
			return m, tea.Quit
		case "c":
			m.choice = fmt.Sprintf("%s(c)%s tcping", yellow, reset)
			return m, tea.Quit
		case "d":
			m.choice = fmt.Sprintf("%s(d)%s Duplicate/Edit Profile", yellow, reset)
			return m, tea.Quit
		case "h":
			m.choice = fmt.Sprintf("%s(h)%s Set http proxy", yellow, reset)
			return m, tea.Quit
		case "H":
			m.choice = fmt.Sprintf("%s(H)%s Remove http proxy", yellow, reset)
			return m, tea.Quit
		case "R":
			m.choice = fmt.Sprintf("%s(R)%s Remove Profile", yellow, reset)
			return m, tea.Quit
		case "x":
			m.choice = fmt.Sprintf("%s(x)%s Set SSH Tunnel", yellow, reset)
			return m, tea.Quit
		case "X":
			m.choice = fmt.Sprintf("%s(X)%s Remove SSH Tunnel", yellow, reset)
			return m, tea.Quit
		case "f":
			m.choice = fmt.Sprintf("%s(f)%s Set Folder", yellow, reset)
			return m, tea.Quit
		case "k":
			m.choice = fmt.Sprintf("%s(k)%s ssh-copy-id", yellow, reset)
			return m, tea.Quit
		case "r":
			m.choice = fmt.Sprintf("%s(r)%s Reveal Password", yellow, reset)
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m ssh_model) View() string {
	var s strings.Builder

	for i, choice := range m.choices {
		cursor := " "
		if m.cursor == i {
			cursor = fmt.Sprintf(" %s>%s", green, reset)
		}

		// Highlight based on shortcuts
		var color string
		switch choice {
		case fmt.Sprintf("%s(s)%s ssh", yellow, reset):
			color = green
		case fmt.Sprintf("%s(n)%s Notes", yellow, reset):
			color = blue
		case fmt.Sprintf("%s(p)%s Set Password", yellow, reset):
			color = red
		case fmt.Sprintf("%s(t)%s sftp (text UI)", yellow, reset):
			color = magenta
		case fmt.Sprintf("%s(o)%s sftp (os native)", yellow, reset):
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
			fmt.Sprintf("%s(s)%s ssh", yellow, reset),
			fmt.Sprintf("%s(o)%s sftp (os native)", yellow, reset),
			fmt.Sprintf("%s(t)%s sftp (text UI)", yellow, reset),
			fmt.Sprintf("%s(i)%s ping", yellow, reset),
			fmt.Sprintf("%s(c)%s tcping", yellow, reset),
			fmt.Sprintf("%s(k)%s ssh-copy-id", yellow, reset),
			fmt.Sprintf("%s(d)%s Duplicate/Edit Profile", yellow, reset),
			fmt.Sprintf("%s(p)%s Set Password", yellow, reset),
			fmt.Sprintf("%s(h)%s Set http proxy", yellow, reset),
			fmt.Sprintf("%s(x)%s Set SSH Tunnel", yellow, reset),
			fmt.Sprintf("%s(f)%s Set Folder", yellow, reset),
			fmt.Sprintf("%s(n)%s Notes", yellow, reset),
			fmt.Sprintf("%s(r)%s Reveal Password", yellow, reset),
			fmt.Sprintf("%s(X)%s Remove SSH Tunnel", yellow, reset),
			fmt.Sprintf("%s(H)%s Remove http proxy", yellow, reset),
			fmt.Sprintf("%s(R)%s Remove Profile", yellow, reset),
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
