package main

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dvdksn/sbx-herdr/internal/sandbox"
)

func managerKey(m managerModel, key string) (managerModel, tea.Cmd) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	if key == "enter" {
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	}
	model, cmd := m.Update(msg)
	return model.(managerModel), cmd
}
func TestManagerNavigationAndConfirmation(t *testing.T) {
	stopped := demo
	stopped.Status = "stopped"
	m := managerModel{width: 72, height: 16, data: managerInventory{
		sandboxes: []sandbox.Info{demo, stopped},
		machines:  []savedMachine{{ID: "m1", Target: "demo.sbx", Session: "default"}},
	}}
	m, _ = managerKey(m, "j")
	if m.cursor != 1 {
		t.Fatal("j did not select second sandbox")
	}
	m, cmd := managerKey(m, "enter")
	if !m.confirming || cmd != nil {
		t.Fatal("stopped sandbox did not ask before starting")
	}
	m, cmd = managerKey(m, "enter")
	if m.confirming || cmd != nil {
		t.Fatal("confirmation did not default to No")
	}
	m, _ = managerKey(m, "l")
	if !m.remove || m.cursor != 0 {
		t.Fatal("l did not switch tabs and reset selection")
	}
	m, cmd = managerKey(m, "enter")
	if !m.confirming || cmd != nil {
		t.Fatal("removal did not ask for confirmation")
	}
	m, cmd = managerKey(m, "n")
	if m.confirming || cmd != nil {
		t.Fatal("declining removal triggered a command")
	}
	m, _ = managerKey(m, "h")
	if m.remove {
		t.Fatal("h did not switch to add tab")
	}
	m, _ = managerKey(m, "k")
	if m.cursor != 0 {
		t.Fatal("selection moved above first row")
	}
}
func TestManagerRemovalWorksWhenSBXUnavailable(t *testing.T) {
	r := scriptedRegistration(t, []registrationStep{step("herdr", profile, "machine", "list", "--json")})
	r.inventory = func() ([]sandbox.Info, error) { return nil, errors.New("daemon unavailable") }
	m := managerModel{runtime: r, width: 72, height: 16}
	model, _ := m.Update(m.load())
	m = model.(managerModel)
	if !strings.Contains(m.View(), "daemon unavailable") {
		t.Fatal("missing SBX error")
	}
	m, _ = managerKey(m, "l")
	if strings.Contains(m.View(), "daemon unavailable") || !strings.Contains(m.View(), "demo") {
		t.Fatal("SBX failure hid saved connections")
	}
	m, cmd := managerKey(m, "enter")
	if !m.confirming || cmd != nil {
		t.Fatal("cannot select stale connection for removal")
	}
}
func TestManagerScrollKeepsSelectedRowVisible(t *testing.T) {
	items := make([]sandbox.Info, 20)
	for i := range items {
		items[i] = sandbox.Info{Name: strings.Repeat("x", i+1), Status: "running"}
	}
	m := managerModel{data: managerInventory{sandboxes: items}, width: 40, height: 12, cursor: 19}
	view := m.View()
	if !strings.Contains(view, strings.Repeat("x", 20)) {
		t.Fatal("selected row not visible")
	}
	if strings.Contains(view, "\n  x  ·") {
		t.Fatal("list did not scroll")
	}
}
func TestManagerRegistrationFailurePreventsMutation(t *testing.T) {
	m := managerModel{data: managerInventory{sandboxes: []sandbox.Info{demo}, machineErr: errors.New("invalid inventory")}}
	m, cmd := managerKey(m, "enter")
	if cmd != nil || !strings.Contains(m.notice, "invalid inventory") {
		t.Fatal("attempted mutation with invalid inventory")
	}
}

func TestManagerCanCloseWhileRefreshing(t *testing.T) {
	_, cmd := managerKey(managerModel{loading: true}, "q")
	if cmd == nil {
		t.Fatal("quit ignored during refresh")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("quit did not close the manager")
	}
}
