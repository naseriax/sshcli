package main

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

type main_model struct {
	allChoices   []string
	choices      []string
	selected     map[int]string
	cursor       int
	choice       string
	searchQuery  string
	message      string
	inSearchMode bool
}

type ssh_model struct {
	allChoices   []string
	choices      []string
	selected     map[int]string
	cursor       int
	choice       string
	searchQuery  string
	inSearchMode bool
}

func getSubMenuContent() []string {
	return []string{
		fmt.Sprintf("%s(s)%s %sssh%s", yellow, reset, BOLD, reset),
		fmt.Sprintf("%s(o)%s sftp (os native)", yellow, reset),
		fmt.Sprintf("%s(t)%s %ssftp (text UI)%s", yellow, reset, BOLD, reset),
		fmt.Sprintf("%s(i)%s %sping%s", yellow, reset, BOLD, reset),
		fmt.Sprintf("%s(c)%s %stcping%s", yellow, reset, BOLD, reset),
		fmt.Sprintf("%s(k)%s ssh-copy-id", yellow, reset),
		fmt.Sprintf("%s(d)%s %sDuplicate/Edit Profile%s", yellow, reset, BOLD, reset),
		fmt.Sprintf("%s(p)%s %sSet Password%s", yellow, reset, BOLD, reset),
		fmt.Sprintf("%s(h)%s Set http proxy", yellow, reset),
		fmt.Sprintf("%s(x)%s Set SSH Tunnel", yellow, reset),
		fmt.Sprintf("%s(f)%s Set Folder", yellow, reset),
		fmt.Sprintf("%s(n)%s %sNotes%s", yellow, reset, BOLD, reset),
		fmt.Sprintf("%s(r)%s Reveal Password", yellow, reset),
		fmt.Sprintf("%s(X)%s Remove SSH Tunnel", yellow, reset),
		fmt.Sprintf("%s(H)%s Remove http proxy", yellow, reset),
		fmt.Sprintf("%s(R)%s Remove Profile", yellow, reset),
	}
}

func (m *ssh_model) filterChoices() {
	if m.searchQuery == "" || !m.inSearchMode {
		m.choices = make([]string, len(m.allChoices))
		copy(m.choices, m.allChoices)
		return
	}

	m.choices = nil
	query := strings.ToLower(m.searchQuery)
	for _, choice := range m.allChoices {
		cleanChoice := strings.ReplaceAll(strings.ReplaceAll(choice, yellow, ""), reset, "")
		if strings.Contains(strings.ToLower(cleanChoice), query) {
			m.choices = append(m.choices, choice)
		}
	}
}

func (m *main_model) filterChoices() {
	if m.searchQuery == "" || !m.inSearchMode {
		m.choices = make([]string, len(m.allChoices))
		copy(m.choices, m.allChoices)
		return
	}

	m.choices = nil
	query := strings.ToLower(m.searchQuery)
	for _, choice := range m.allChoices {
		cleanChoice := strings.ReplaceAll(strings.ReplaceAll(choice, yellow, ""), reset, "")
		if strings.Contains(strings.ToLower(cleanChoice), query) {
			m.choices = append(m.choices, choice)
		}
	}
}

func (m ssh_model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "/":
			if !m.inSearchMode {
				m.inSearchMode = true
				m.searchQuery = ""
				m.filterChoices()
				if m.cursor >= len(m.choices) {
					m.cursor = max(0, len(m.choices)-1)
				}
			}
			return m, nil

		case "ctrl+c", "q":
			m.choice = ""
			m.searchQuery = ""
			m.inSearchMode = false
			m.filterChoices()
			return m, tea.Quit

		case "esc":
			if m.inSearchMode {
				m.inSearchMode = false
				m.searchQuery = ""
				m.filterChoices()
				if m.cursor >= len(m.choices) {
					m.cursor = max(0, len(m.choices)-1)
				}
			} else {
				m.choice = ""
				return m, tea.Quit
			}
			return m, nil

		case "up":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}

		case "enter", " ":
			if len(m.choices) > 0 {
				m.choice = m.choices[m.cursor]
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			}
		}

		if m.inSearchMode {
			switch msg.Type {
			case tea.KeyBackspace:
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
					m.filterChoices()
					if m.cursor >= len(m.choices) {
						m.cursor = max(0, len(m.choices)-1)
					}
				}
			case tea.KeyRunes:
				if len(msg.Runes) == 1 && (unicode.IsLetter(msg.Runes[0]) || unicode.IsDigit(msg.Runes[0])) {
					m.searchQuery += string(msg.Runes)
					m.filterChoices()
					if m.cursor >= len(m.choices) {
						m.cursor = max(0, len(m.choices)-1)
					}
				}
			}
		} else {
			// Handle keyboard shortcuts only when not in search mode
			switch msg.String() {
			case "s":
				m.choice = fmt.Sprintf("%s(s)%s ssh", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "n":
				m.choice = fmt.Sprintf("%s(n)%s Notes", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "p":
				m.choice = fmt.Sprintf("%s(p)%s Set Password", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "t":
				m.choice = fmt.Sprintf("%s(t)%s sftp (text UI)", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "o":
				m.choice = fmt.Sprintf("%s(o)%s sftp (os native)", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "i":
				m.choice = fmt.Sprintf("%s(i)%s ping", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "c":
				m.choice = fmt.Sprintf("%s(c)%s tcping", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "d":
				m.choice = fmt.Sprintf("%s(d)%s Duplicate/Edit Profile", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "h":
				m.choice = fmt.Sprintf("%s(h)%s Set http proxy", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "H":
				m.choice = fmt.Sprintf("%s(H)%s Remove http proxy", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "R":
				m.choice = fmt.Sprintf("%s(R)%s Remove Profile", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "x":
				m.choice = fmt.Sprintf("%s(x)%s Set SSH Tunnel", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "X":
				m.choice = fmt.Sprintf("%s(X)%s Remove SSH Tunnel", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "f":
				m.choice = fmt.Sprintf("%s(f)%s Set Folder", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "k":
				m.choice = fmt.Sprintf("%s(k)%s ssh-copy-id", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			case "r":
				m.choice = fmt.Sprintf("%s(r)%s Reveal Password", yellow, reset)
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			}
		}
	}

	return m, nil
}

func (m main_model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "/":
			if !m.inSearchMode {
				m.inSearchMode = true
				m.searchQuery = ""
				m.filterChoices()
				if m.cursor >= len(m.choices) {
					m.cursor = max(0, len(m.choices)-1)
				}
			}
			return m, nil

		case "ctrl+c", "q":
			m.choice = ""
			m.searchQuery = ""
			m.inSearchMode = false
			m.filterChoices()
			return m, tea.Quit

		case "esc":
			if m.inSearchMode {
				m.inSearchMode = false
				m.searchQuery = ""
				m.filterChoices()
				if m.cursor >= len(m.choices) {
					m.cursor = max(0, len(m.choices)-1)
				}
			} else {
				m.choice = ""
				return m, tea.Quit
			}
			return m, nil

		case "up":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}

		case "enter", " ":
			if len(m.choices) > 0 {
				m.choice = m.choices[m.cursor]
				m.searchQuery = ""
				m.inSearchMode = false
				m.filterChoices()
				return m, tea.Quit
			}
		}

		if m.inSearchMode {
			switch msg.Type {
			case tea.KeyBackspace:
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
					m.filterChoices()
					if m.cursor >= len(m.choices) {
						m.cursor = max(0, len(m.choices)-1)
					}
				}
			case tea.KeyRunes:
				if len(msg.Runes) == 1 && (unicode.IsLetter(msg.Runes[0]) || unicode.IsDigit(msg.Runes[0])) {
					m.searchQuery += string(msg.Runes)
					m.filterChoices()
					if m.cursor >= len(m.choices) {
						m.cursor = max(0, len(m.choices)-1)
					}
				}
			}
		}
	}

	return m, nil
}

func (m ssh_model) View() string {
	var s strings.Builder

	if m.inSearchMode {
		s.WriteString(fmt.Sprintf("Search mode: %s\n\n", m.searchQuery))
	}

	if len(m.choices) == 0 {
		s.WriteString("No matches found.\n")
	} else {
		for i, choice := range m.choices {
			cursor := " "
			if m.cursor == i {
				cursor = fmt.Sprintf(" %s>%s", green, reset)
			}
			s.WriteString(fmt.Sprintf("%s %s\n", cursor, choice))
		}
	}

	s.WriteString(fmt.Sprintf("\n%sPress shortcut key, / to search, arrows+Enter to select, or q to quit.%s\n", yellow, reset))

	return s.String()
}

func (m main_model) View() string {
	var s strings.Builder

	if m.inSearchMode {
		s.WriteString(fmt.Sprintf("Search mode: %s\n\n", m.searchQuery))
	} else {
		s.WriteString(m.message)
	}

	if len(m.choices) == 0 {
		s.WriteString("No matches found.\n")
	} else {
		for i, choice := range m.choices {
			cursor := " "
			if m.cursor == i {
				cursor = fmt.Sprintf(" %s>%s", green, reset)
			}
			s.WriteString(fmt.Sprintf("%s %s\n", cursor, choice))
		}
	}

	s.WriteString(fmt.Sprintf("\n%sPress shortcut key, / to search, arrows+Enter to select, or q to quit.%s\n", yellow, reset))

	return s.String()
}

func (m ssh_model) Init() tea.Cmd {
	return nil
}

func (m main_model) Init() tea.Cmd {
	return nil
}

func main_ui(items []string, message string, isSshContextMenu bool) (string, error) {

	var p *tea.Program

	if isSshContextMenu {
		p = tea.NewProgram(ssh_model{
			allChoices: getSubMenuContent(),
			choices:    getSubMenuContent(),
			selected:   make(map[int]string),
		}, tea.WithAltScreen())
	} else {
		p = tea.NewProgram(main_model{
			allChoices: items,
			choices:    items,
			selected:   make(map[int]string),
			message:    message,
		}, tea.WithAltScreen())
	}

	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	if isSshContextMenu {
		if m, ok := finalModel.(ssh_model); ok && m.choice != "" {
			return m.choice, nil
		}
	} else {
		if m, ok := finalModel.(main_model); ok && m.choice != "" {
			return m.choice, nil
		}
	}

	return "", fmt.Errorf("unexpected model type")
}
