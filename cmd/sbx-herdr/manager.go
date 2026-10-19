package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/dvdksn/sbx-herdr/internal/sandbox"
)

type managerInventory struct {
	sandboxes              []sandbox.Info
	machines               []savedMachine
	sandboxErr, machineErr error
}
type managerResult struct {
	err     error
	message string
}
type managerModel struct {
	runtime               registration
	data                  managerInventory
	remove                bool
	cursor, width, height int
	loading, confirming   bool
	notice                string
}

func runManager() error {
	_, err := tea.NewProgram(managerModel{runtime: newRegistration(), width: 72, height: 16, loading: true}, tea.WithAltScreen()).Run()
	return err
}
func (m managerModel) load() tea.Msg {
	machines, machineErr := m.runtime.machines()
	items, sandboxErr := m.runtime.inventory()
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	profiles := make([]savedMachine, 0, len(machines))
	for _, profile := range machines {
		if sandboxHost.MatchString(profile.Target) {
			profiles = append(profiles, profile)
		}
	}
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Target+profiles[i].Session+profiles[i].ID < profiles[j].Target+profiles[j].Session+profiles[j].ID
	})
	return managerInventory{items, profiles, sandboxErr, machineErr}
}
func (m managerModel) Init() tea.Cmd { return m.load }
func (m managerModel) count() int {
	if m.remove {
		return len(m.data.machines)
	}
	return len(m.data.sandboxes)
}
func (m managerModel) operation() tea.Cmd {
	var run func() error
	message := "Connection added."
	if m.remove {
		selected := m.data.machines[m.cursor]
		run = func() error { return m.runtime.remove(selected) }
		message = "Connection removed."
	} else {
		selected := m.data.sandboxes[m.cursor]
		run = func() error { return m.runtime.add(selected) }
	}
	return tea.Exec(managerOperation{run}, func(err error) tea.Msg { return managerResult{err, message} })
}

// Bubble Tea restores cooked terminal input while Herdr asks for installation
// approval, then resumes the manager after the command finishes.
type managerOperation struct{ run func() error }

func (op managerOperation) Run() error       { return op.run() }
func (managerOperation) SetStdin(io.Reader)  {}
func (managerOperation) SetStdout(io.Writer) {}
func (managerOperation) SetStderr(io.Writer) {}

func (m managerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case managerInventory:
		m.data = msg
		m.loading = false
		if m.cursor >= m.count() {
			m.cursor = max(0, m.count()-1)
		}
	case managerResult:
		m.notice = msg.message
		if msg.err != nil {
			m.notice = "Error: " + msg.err.Error()
		}
		m.loading = true
		return m, m.load
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.loading {
			if msg.String() == "q" || msg.String() == "esc" {
				return m, tea.Quit
			}
			return m, nil
		}
		if m.confirming {
			switch msg.String() {
			case "y":
				m.confirming = false
				m.loading = true
				return m, m.operation()
			case "n", "esc", "q", "enter":
				m.confirming = false
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "esc":
			return m, tea.Quit
		case "h", "left":
			m.remove = false
			m.cursor = 0
			m.notice = ""
		case "l", "right":
			m.remove = true
			m.cursor = 0
			m.notice = ""
		case "tab":
			m.remove = !m.remove
			m.cursor = 0
			m.notice = ""
		case "j", "down":
			m.cursor = min(max(0, m.count()-1), m.cursor+1)
		case "k", "up":
			m.cursor = max(0, m.cursor-1)
		case "g", "home":
			m.cursor = 0
		case "G", "end":
			m.cursor = max(0, m.count()-1)
		case "r":
			m.loading = true
			m.notice = ""
			return m, m.load
		case "enter":
			if m.data.machineErr != nil {
				m.notice = "Error: " + m.data.machineErr.Error()
				break
			}
			if m.count() == 0 {
				break
			}
			if m.remove || m.data.sandboxes[m.cursor].Status != "running" {
				m.confirming = true
				break
			}
			m.loading = true
			return m, m.operation()
		}
	}
	return m, nil
}

var managerAccent = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
var managerMuted = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

func managerText(s string, width int) string {
	return ansi.Truncate(ansi.Strip(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " ")), max(1, width), "…")
}
func (m managerModel) View() string {
	width := max(12, m.width-4)
	add, remove := " Add connection ", " Remove connection "
	if m.remove {
		remove = managerAccent.Reverse(true).Render(remove)
	} else {
		add = managerAccent.Reverse(true).Render(add)
	}
	lines := []string{"", "  " + add + "  " + remove, ""}
	rows := max(1, m.height-10)
	if m.loading {
		lines = append(lines, "  Loading…")
	} else if m.count() == 0 {
		label := "No local sandboxes."
		if m.remove {
			label = "No sandbox connections."
		}
		lines = append(lines, "  "+managerMuted.Render(label))
	} else {
		widths := []int{max(4, width-32), 13, 11}
		headers := []string{"Sandbox", "Sandbox state", "Herdr"}
		if m.remove {
			widths = []int{max(4, width-33), 11, 14}
			headers = []string{"Sandbox", "Herdr", "Herdr session"}
		}
		if width < 48 {
			widths = []int{max(4, width-16), 11}
			headers = []string{"Sandbox", "Herdr"}
		}
		lines = append(lines, "    "+managerMuted.Bold(true).Render(managerTableRow(headers, widths)), "    "+managerMuted.Render(strings.Repeat("─", max(1, width-2))))
		start := max(0, m.cursor-rows+1)
		for i := start; i < min(m.count(), start+rows); i++ {
			name, state, connection, session := "", "", "Not added", ""
			if m.remove {
				profile := m.data.machines[i]
				name = strings.TrimSuffix(profile.Target, ".sbx")
				session = profile.Session
				connection = "Added"
				if !profile.Enabled {
					connection = "Added (off)"
				}
			} else {
				item := m.data.sandboxes[i]
				name = item.Name
				state = item.Status
				if state != "" {
					state = strings.ToUpper(state[:1]) + state[1:]
				}
				profile, err := matchingRegistration(m.data.machines, item.Name)
				if err != nil {
					connection = "Multiple"
				} else if profile != nil {
					connection = "Added"
					if !profile.Enabled {
						connection = "Added (off)"
					}
				}
			}
			cells := []string{name, state, connection}
			if m.remove {
				cells = []string{name, connection, session}
			}
			if len(widths) == 2 {
				cells = []string{name, connection}
			}
			for col := range cells {
				cells[col] = managerCell(cells[col], widths[col])
				switch {
				case col == 0 && i == m.cursor:
					cells[col] = managerAccent.Render(cells[col])
				case (col == 2 && !m.remove) || (col == 1 && m.remove) || (col == 1 && len(widths) == 2):
					color := "2"
					if connection != "Added" {
						color = "3"
					}
					cells[col] = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(cells[col])
				case col > 0:
					cells[col] = managerMuted.Render(cells[col])
				}
			}
			marker := "  "
			if i == m.cursor {
				marker = managerAccent.Render("› ")
			}
			lines = append(lines, "  "+marker+strings.Join(cells, managerMuted.Render(" │ ")))
		}
	}
	for len(lines) < 5+rows {
		lines = append(lines, "")
	}
	notice := m.notice
	if notice == "" && m.count() > 0 {
		if m.remove {
			session := m.data.machines[m.cursor].Session
			notice = "Herdr session: " + session
			if session == "default" {
				notice += " (standard session)"
			}
		} else {
			notice = "Added = saved in Herdr · Not added = no saved connection"
			if profile, _ := matchingRegistration(m.data.machines, m.data.sandboxes[m.cursor].Name); profile != nil && !profile.Enabled {
				notice = "Added (off) = saved connection, currently disabled"
			}
		}
	}
	if m.data.machineErr != nil {
		notice = "Error: " + m.data.machineErr.Error()
	} else if !m.remove && m.data.sandboxErr != nil {
		notice = "Error: " + m.data.sandboxErr.Error()
	}
	if m.confirming {
		if m.remove {
			notice = fmt.Sprintf("Remove %s (session: %s)? [y/N]", m.data.machines[m.cursor].Target, m.data.machines[m.cursor].Session)
		} else {
			notice = fmt.Sprintf("Start %s and add its connection? [y/N]", m.data.sandboxes[m.cursor].Name)
		}
	}
	noticeStyle := managerMuted
	if strings.HasPrefix(notice, "Error:") {
		noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	}
	if m.confirming {
		noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	}
	lines = append(lines, "", "  "+noticeStyle.Render(managerText(notice, width)), "  "+m.legend(width))
	return strings.Join(lines, "\n")
}

func managerCell(text string, width int) string {
	text = managerText(text, width)
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}
func managerTableRow(cells []string, widths []int) string {
	out := make([]string, len(cells))
	for i, text := range cells {
		out[i] = managerCell(text, widths[i])
	}
	return strings.Join(out, " │ ")
}
func (m managerModel) legend(width int) string {
	hint := func(key, action string) string { return managerAccent.Render(key) + " " + managerMuted.Render(action) }
	if m.confirming {
		return hint("y", "confirm") + "   " + hint("Enter / Esc", "cancel")
	}
	action := "add"
	if m.remove {
		action = "remove"
	}
	hints := []string{hint("h/l", "tabs"), hint("j/k", "select"), hint("↵", action), hint("r", "refresh"), hint("q", "close")}
	return ansi.Truncate(strings.Join(hints, "   "), max(1, width), "…")
}
