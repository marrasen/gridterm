package main

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// clickPlus presses the plus on a machine's heading, the way a user
// does: through the tree, at the column the list drew it in.
func clickPlus(t *testing.T, a *testApp, host string) *ui.Menu {
	t.Helper()
	return clickPlusOn(t, a, hostKey(host), host)
}

// clickPlusFar presses the plus on the heading of a machine a window
// taken over is connected to.
func clickPlusFar(t *testing.T, a *testApp, addr, host string) *ui.Menu {
	t.Helper()
	key := remoteHostKey{window: windowAt(t, a, addr), host: host}
	return clickPlusOn(t, a, key, host+" under "+addr)
}

// clickPlusOn presses the plus on the heading a key names. what says
// which heading it was, for a test that cannot find it.
func clickPlusOn(t *testing.T, a *testApp, key any, what string) *ui.Menu {
	t.Helper()
	a.refreshPanel(time.Now())
	area, ok := a.root.AreaOf(a.side)
	if !ok {
		t.Fatal("the sidebar is not in the tree")
	}
	y := a.panel.RowTop(key)
	if y < 0 {
		t.Fatalf("no heading for %q: %v", what, panelText(a, time.Now()))
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
	selectMenuItem(t, m, id)
	if _, err := m.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEnter}); err != nil {
		t.Fatalf("running %s: %v", id, err)
	}
}

// selectMenuItem moves the bar onto the line naming a command, the way
// the arrow keys do.
func selectMenuItem(t *testing.T, m *ui.Menu, id string) {
	t.Helper()
	for step := 0; step < len(m.Items()); step++ {
		if cmd, ok := m.Selected(); ok && cmd.ID == id {
			return
		}
		if _, err := m.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyDown}); err != nil {
			t.Fatalf("moving down the menu: %v", err)
		}
	}
	t.Fatalf("%s is not on the menu: %v", id, menuCommands(m))
}

// chooseMenuItemOver runs a menu line that waits on another window's
// answer.
//
// The line runs on a goroutine of its own and the other window is pumped
// meanwhile: it answers from the goroutine that draws, which is the test
// one, so a test that ran the line straight would wait for an answer
// nothing was going to give. Nothing here touches the window the menu is
// on, so that window's tree stays the goroutine's for as long as the
// line is running.
//
// A line that failed says so in a notice, the way every command does, so
// there is nothing to give back here.
func chooseMenuItemOver(t *testing.T, other *testApp, m *ui.Menu, id string) {
	t.Helper()
	selectMenuItem(t, m, id)
	done := make(chan error, 1)
	go func() {
		_, err := m.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEnter})
		done <- err
	}()
	var err error
	waitFor(t, other, "the window over there to answer "+id, func() bool {
		select {
		case err = <-done:
			return true
		default:
			return false
		}
	})
	if err != nil {
		t.Fatalf("running %s: %v", id, err)
	}
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
	m := a.machines.named(host)
	if m == nil {
		return 0
	}
	return len(a.machines.panesOn(m))
}

// The machine gridterm is running on has nothing to connect or tunnel,
// so its plus offers the two things that can be opened here.
//
// Not handing a pane to an agent: a machine's row is about that
// machine, and the pane that would be handed over is whichever one the
// user is looking at, which may be on another machine entirely.
func TestThePlusOnLocalOffersFilesAndATerminal(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	menu := clickPlus(t, a, conns.Local)
	// conn.terminal, the line every machine's row carries: on this row
	// it opens a shell here, whatever a new tab would open on.
	for _, want := range []string{"conn.terminal", "conn.files"} {
		if !offers(menu, want) {
			t.Errorf("the menu does not offer %s: %v", want, menuCommands(menu))
		}
	}
	for _, not := range []string{"tab.open", "conn.tunnel", "conn.disconnect", "agent.hand"} {
		if offers(menu, not) {
			t.Errorf("the menu offers %s, which is not about this machine", not)
		}
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
		"conn.tunnel", "conn.socks", "conn.disconnect",
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
	// Nor the one that closes whatever the list has selected. Clicking
	// the plus does not move the selection, so that line would close
	// some other machine's connection without saying so.
	if offers(menu, "conn.close") {
		t.Errorf("the menu offers the line that closes the selected row: %v", menuCommands(menu))
	}
}

// Closing a machine from its own plus closes that machine, whatever row
// the list has the bar on.
func TestClosingAMachineFromItsPlusClosesThatMachine(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()

	// The bar follows the stage, so it is put on a pane running here:
	// the point of the test is that the plus acts on the machine whose
	// row was clicked rather than on the row the bar is sitting on.
	a.focus(localPane(t, a))
	a.refreshPanel(time.Now())
	e, ok := a.panel.Selected()
	if !ok || e.Key.(*conns.Entry).Host != conns.Local {
		t.Fatalf("the bar is on %v, want a pane on this machine", e.Text)
	}

	menu := clickPlus(t, a, host)
	chooseMenuItem(t, menu, "conn.disconnect")
	waitFor(t, a, "the connection to be closed", func() bool {
		return a.machines.named(host) == nil
	})
	// And the pane the bar was on is still there.
	if len(a.panes) == 0 {
		t.Fatal("the local terminal went with the connection")
	}
	if localPane(t, a) == nil {
		t.Fatal("no pane is running here any more")
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
	if host, up := a.hostMenus.machine(); up {
		t.Fatalf("the window is still acting on %q, and no menu is open for it", host)
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
	dismiss(t, menu)
	if a.root.Modal() != nil {
		t.Fatalf("Escape left %T on the stack", a.root.Modal())
	}
	a.pump.run()
	if host, up := a.hostMenus.machine(); up {
		t.Fatalf("%q outlived the menu", host)
	}
}

// A menu opened while an earlier one is still being forgotten keeps its
// own machine.
//
// The machine is forgotten a turn of the pump after the menu closes, and
// by then another menu may have opened: a key and a click are both
// dealt with inside one frame, so this is a menu dismissed with Escape
// and another opened with the mouse before either is drawn.
func TestANewMenuKeepsItsMachineWhileTheOldOneIsForgotten(t *testing.T) {
	one := sshtest.New(t)
	two := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	a.connectAs("one", serverConfig(t, one))
	waitForPanes(t, a, 2)
	a.connectAs("two", serverConfig(t, two))
	waitForPanes(t, a, 3)

	first := clickPlus(t, a, "one")
	dismiss(t, first)
	second := clickPlus(t, a, "two")

	// The clear queued by the first menu runs now, and must leave the
	// second alone.
	a.pump.run()
	if host, _ := a.hostMenus.machine(); host != "two" {
		t.Fatalf("the window is acting on %q, want the machine the open menu is about", host)
	}
	if got := a.currentHost(); got != "two" {
		t.Fatalf("the commands would act on %q", got)
	}

	// And once the second goes, nothing is left behind.
	dismiss(t, second)
	a.pump.run()
	if host, up := a.hostMenus.machine(); up {
		t.Fatalf("%q outlived the last menu", host)
	}
}

// The plus on a window taken over offers what that connection carries.
//
// Panes and files cross it. A command, a tunnel and a proxy are things
// this window asks a machine for, and the machine over there is not
// this one's to ask, so offering them would be offering something that
// cannot work.
func TestThePlusOnAWindowOffersPanesAndFiles(t *testing.T) {
	items := hostItems(hostFacts{kind: hostWindow})

	offered := map[string]bool{}
	for _, it := range items {
		offered[it.Command] = true
	}
	for _, want := range []string{"conn.terminal", "conn.files", "conn.disconnect"} {
		if !offered[want] {
			t.Errorf("it does not offer %s", want)
		}
	}
	for _, not := range []string{"conn.command", "conn.tunnel", "conn.socks"} {
		if offered[not] {
			t.Errorf("it offers %s, which cannot work on a window", not)
		}
	}
	// And the wording says what letting go of a window means, rather
	// than talking about a connection to a machine.
	for _, it := range items {
		if it.Command == "conn.disconnect" && it.Title != "Let go of this window" {
			t.Errorf("it says %q", it.Title)
		}
	}
}

// The plus on a saved window's row takes it over.
//
// The user's own path: click the plus on the heading, choose the line
// it offers. Driven through the menu rather than by calling the command,
// because every fix so far has been in a place the real path did not go.
func TestThePlusOnASavedWindowTakesItOver(t *testing.T) {
	host := newTestApp(t, 90, 30)
	withDialogs(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	addr := host.serving.addr()

	client := newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	if err := client.book.Put(remote.Host{
		Name: "statio", Address: hostOf(t, addr), Port: portOf(t, addr),
		Window: true, Identities: []string{keyFile},
	}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	client.refreshServers()

	menu := clickPlus(t, client, "statio")
	if !offers(menu, "conn.terminal") {
		t.Fatalf("the plus offers %v", menuCommands(menu))
	}
	chooseMenuItem(t, menu, "conn.terminal")

	// Nothing should have gone wrong, and nothing should be asking.
	if f, ok := client.root.Modal().(*ui.Form); ok {
		t.Fatalf("it reported %q: %v", f.Title, f.Lines)
	}
	answer(t, client, "Connect")
	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows.named("statio") != nil
	})
}

// The commands that act on "here" ask one place what the machine in
// front of the user is, rather than each working it out again.
//
// There were two near-identical routines, one for a machine named and
// one for the machine in front of the user, and a window was handled in
// only one of them. Clicking the plus took the other. Checked in the
// source, because the mistake is one of shape: a second copy of the
// switch is the bug, whatever it says today.
func TestTheHereCommandsDelegate(t *testing.T) {
	// Each one names what the machine is, or hands the name to a command
	// that does. "a.openOn(" is not on this list: it is what a caller
	// runs once the answer is known, so it proves nothing about who
	// worked the answer out.
	delegates := []string{"a.about(", "openTerminalOn(", "openFilesOn(", "dropMachine("}
	// Working it out again, in any of the ways the window used to.
	own := []*regexp.Regexp{
		regexp.MustCompile(`a\.windows\.named\(`),
		regexp.MustCompile(`a\.machines\.named\(`),
		regexp.MustCompile(`a\.machines\.connecting\(`),
		regexp.MustCompile(`a\.book\.`),
		regexp.MustCompile(`\bisWindow\b`),
		regexp.MustCompile(`\bisHere\b`),
		regexp.MustCompile(`\bsavedWindow\b`),
	}

	for _, name := range []string{
		"openTerminalHere", "openFilesHere", "disconnectHere", "openCommandHere",
	} {
		body, where := functionBody(t, name)
		for _, re := range own {
			if re.MatchString(body) {
				t.Errorf("%s (%s) decides what the machine is itself, with %s:\n%s",
					name, where, re, body)
			}
		}
		asks := false
		for _, to := range delegates {
			if strings.Contains(body, to) {
				asks = true
			}
		}
		if !asks {
			t.Errorf("%s (%s) neither asks about() nor hands over to a command that does:\n%s",
				name, where, body)
		}
	}
}

// functionBody is the source of one of the window's own functions, found
// wherever it lives.
//
// By name rather than by file, so moving a function reports what it
// found instead of saying the function is gone. Printed from the tree
// rather than cut out of the file, which leaves the comments behind: a
// rule about what the code does is not met by a comment saying so.
func functionBody(t *testing.T, name string) (body, where string) {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	for _, at := range names {
		if strings.HasSuffix(at, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(at)
		if err != nil {
			t.Fatalf("read %s: %v", at, err)
		}
		file, err := parser.ParseFile(fset, at, raw, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", at, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != name {
				continue
			}
			var out strings.Builder
			if err := printer.Fprint(&out, fset, fn.Body); err != nil {
				t.Fatalf("print %s: %v", name, err)
			}
			return out.String(), at
		}
	}
	t.Fatalf("there is no function called %s any more; this test needs rewriting", name)
	return "", ""
}
