package main

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// Base model that contains all common functionality
type baseModel struct {
	allChoices   []string
	choices      []string
	selected     map[int]string
	cursor       int
	choice       string
	searchQuery  string
	message      string
	inSearchMode bool
	isSSHContext bool
}

// Wrapper types for type safety
type main_model struct {
	baseModel
}

type ssh_model struct {
	baseModel
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

// Common filter function
func (m *baseModel) filterChoices() {
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

// SSH shortcut handlers
func (m *baseModel) handleSSHShortcuts(key string) (tea.Model, tea.Cmd) {
	shortcuts := map[string]string{
		"s": fmt.Sprintf("%s(s)%s ssh", yellow, reset),
		"n": fmt.Sprintf("%s(n)%s Notes", yellow, reset),
		"p": fmt.Sprintf("%s(p)%s Set Password", yellow, reset),
		"t": fmt.Sprintf("%s(t)%s sftp (text UI)", yellow, reset),
		"o": fmt.Sprintf("%s(o)%s sftp (os native)", yellow, reset),
		"i": fmt.Sprintf("%s(i)%s ping", yellow, reset),
		"c": fmt.Sprintf("%s(c)%s tcping", yellow, reset),
		"d": fmt.Sprintf("%s(d)%s Duplicate/Edit Profile", yellow, reset),
		"h": fmt.Sprintf("%s(h)%s Set http proxy", yellow, reset),
		"H": fmt.Sprintf("%s(H)%s Remove http proxy", yellow, reset),
		"R": fmt.Sprintf("%s(R)%s Remove Profile", yellow, reset),
		"x": fmt.Sprintf("%s(x)%s Set SSH Tunnel", yellow, reset),
		"X": fmt.Sprintf("%s(X)%s Remove SSH Tunnel", yellow, reset),
		"f": fmt.Sprintf("%s(f)%s Set Folder", yellow, reset),
		"k": fmt.Sprintf("%s(k)%s ssh-copy-id", yellow, reset),
		"r": fmt.Sprintf("%s(r)%s Reveal Password", yellow, reset),
	}

	if choice, exists := shortcuts[key]; exists {
		m.choice = choice
		m.searchQuery = ""
		m.inSearchMode = false
		m.filterChoices()
		return ssh_model{*m}, tea.Quit
	}

	return ssh_model{*m}, nil
}

// Common update logic
func (m *baseModel) updateBase(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if m.isSSHContext {
				return ssh_model{*m}, nil
			}
			return main_model{*m}, nil

		case "ctrl+c", "q":
			m.choice = ""
			m.searchQuery = ""
			m.inSearchMode = false
			m.filterChoices()
			if m.isSSHContext {
				return ssh_model{*m}, tea.Quit
			}
			return main_model{*m}, tea.Quit

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
				if m.isSSHContext {
					return ssh_model{*m}, tea.Quit
				}
				return main_model{*m}, tea.Quit
			}
			if m.isSSHContext {
				return ssh_model{*m}, nil
			}
			return main_model{*m}, nil

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
				if m.isSSHContext {
					return ssh_model{*m}, tea.Quit
				}
				return main_model{*m}, tea.Quit
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
		} else if m.isSSHContext {
			// Handle SSH keyboard shortcuts only when not in search mode
			return m.handleSSHShortcuts(msg.String())
		}
	}

	if m.isSSHContext {
		return ssh_model{*m}, nil
	}
	return main_model{*m}, nil
}

// Common view logic
func (m *baseModel) viewBase() string {
	var s strings.Builder

	if m.inSearchMode {
		s.WriteString(fmt.Sprintf("Search mode: %s\n\n", m.searchQuery))
	} else if !m.isSSHContext {
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

// Interface methods for ssh_model
func (m ssh_model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.baseModel.updateBase(msg)
}

func (m ssh_model) View() string {
	return m.baseModel.viewBase()
}

func (m ssh_model) Init() tea.Cmd {
	return nil
}

// Interface methods for main_model
func (m main_model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.baseModel.updateBase(msg)
}

func (m main_model) View() string {
	return m.baseModel.viewBase()
}

func (m main_model) Init() tea.Cmd {
	return nil
}

func main_ui(items []string, message string, isSshContextMenu bool) (string, error) {
	var p *tea.Program

	if isSshContextMenu {
		p = tea.NewProgram(ssh_model{
			baseModel: baseModel{
				allChoices:   getSubMenuContent(),
				choices:      getSubMenuContent(),
				selected:     make(map[int]string),
				isSSHContext: true,
			},
		}, tea.WithAltScreen())
	} else {
		p = tea.NewProgram(main_model{
			baseModel: baseModel{
				allChoices:   items,
				choices:      items,
				selected:     make(map[int]string),
				message:      message,
				isSSHContext: false,
			},
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
