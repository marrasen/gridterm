package main

import (
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// clickPlus presses the plus on a machine's heading, the way a user
// does: through the tree, at the column the list drew it in.
func clickPlus(t *testing.T, a *testApp, host string) *ui.Menu {
	t.Helper()
	a.refreshPanel(time.Now())
	area, ok := a.root.AreaOf(a.side)
	if !ok {
		t.Fatal("the sidebar is not in the tree")
	}
	y := a.panel.RowTop(hostKey(host))
	if y < 0 {
		t.Fatalf("no heading for %q: %v", host, panelText(a, time.Now()))
	}
	took, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: area.X + area.Cols - 2, Row: area.Y + y,
	})
	if err != nil {
		t.Fatalf("the press failed: %v", err)
	}
	if !took {
		t.Fatal("the press on the plus travelled on")
	}
	menu, ok := a.root.Modal().(*ui.Menu)
	if !ok {
		t.Fatalf("the plus opened %T, want a menu", a.root.Modal())
	}
	return menu
}

// commands returns the ids a menu is offering, with "" for a rule.
func menuCommands(m *ui.Menu) []string {
	var out []string
	for _, item := range m.Items() {
		out = append(out, item.Command)
	}
	return out
}

// chooseMenuItem runs the line naming a command, the way Enter does.
func chooseMenuItem(t *testing.T, m *ui.Menu, id string) {
	t.Helper()
	for step := 0; step < len(m.Items()); step++ {
		if cmd, ok := m.Selected(); ok && cmd.ID == id {
			if _, err := m.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEnter}); err != nil {
				t.Fatalf("running %s: %v", id, err)
			}
			return
		}
		m.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyDown})
	}
	t.Fatalf("%s is not on the menu: %v", id, menuCommands(m))
}

// offers reports whether a command is on a menu.
func offers(m *ui.Menu, id string) bool {
	for _, item := range m.Items() {
		if item.Command == id {
			return true
		}
	}
	return false
}

// localPane returns a terminal running on this machine.
func localPane(t *testing.T, a *testApp) *term.Terminal {
	t.Helper()
	for pane, e := range a.panes {
		if e.Host == conns.Local {
			return pane
		}
	}
	t.Fatal("no pane is running here")
	return nil
}

// panesOn counts the terminals the window has open on a machine.
func panesOn(a *testApp, host string) int {
	var n int
	for _, m := range a.paneOn {
		if m != nil && m.at.name == host {
			n++
		}
	}
	return n
}

// The machine gridterm is running on has nothing to connect or tunnel,
// so its plus offers the two things that can be opened here.
func TestThePlusOnLocalOffersFilesAndATerminal(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	menu := clickPlus(t, a, conns.Local)
	if got := menuCommands(menu); len(got) != 2 {
		t.Fatalf("the menu offers %v, want a terminal and files", got)
	}
	if !offers(menu, "tab.open") || !offers(menu, "conn.files") {
		t.Fatalf("the menu offers %v", menuCommands(menu))
	}
}

// A machine reached over a connection has more to offer, and something
// to close.
func TestThePlusOnAServerOffersWhatAConnectionCanCarry(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()

	menu := clickPlus(t, a, host)
	for _, want := range []string{
		"conn.terminal", "conn.files", "conn.command",
		"conn.tunnel", "conn.socks", "conn.close",
	} {
		if !offers(menu, want) {
			t.Errorf("the menu is missing %s: %v", want, menuCommands(menu))
		}
	}
	// And not the one that opens a plain tab here, which would say it
	// runs on the server and not.
	if offers(menu, "tab.open") {
		t.Errorf("the menu offers a local tab: %v", menuCommands(menu))
	}
}

// The lines on the menu are the same commands the menu bar and the keys
// use, and they act on the machine the user is looking at. A menu
// dropped from a row says which machine that is, whatever has the keys.
func TestThePlusSaysWhichMachineTheCommandsAreFor(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()

	// The keys are on a pane running here, so without the menu the
	// commands would mean this machine.
	a.focus(localPane(t, a))
	if got := a.currentHost(); got != conns.Local {
		t.Fatalf("the window is looking at %q before the menu", got)
	}

	menu := clickPlus(t, a, host)
	if got := a.currentHost(); got != host {
		t.Fatalf("with the menu up the window is looking at %q, want %s", got, host)
	}
	// It lasts long enough for the line that was chosen to run: a menu
	// goes away before it runs anything.
	chooseMenuItem(t, menu, "conn.terminal")
	waitForPanes(t, a, 3)
	if got := panesOn(a, host); got != 2 {
		t.Fatalf("%d panes run on %s, want the login and the one the menu opened", got, host)
	}

	// And it is forgotten again, so the next command means whatever has
	// the keys.
	a.pump.run()
	if a.acting {
		t.Fatal("the window is still acting on a machine no menu is open for")
	}
}

// A menu closed without choosing anything leaves nothing behind either.
func TestAMenuDismissedForgetsItsMachine(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)

	menu := clickPlus(t, a, serverConfig(t, s).Target())
	menu.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEscape})
	if a.root.Modal() != nil {
		t.Fatalf("Escape left %T on the stack", a.root.Modal())
	}
	a.pump.run()
	if a.acting {
		t.Fatal("the machine outlived the menu")
	}
}
