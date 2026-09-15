package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vfs"
)

// about answers every kind, so the one decision really is one.
func TestAboutNamesEveryKindOfHost(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)

	if err := a.book.Put(remote.Host{
		Name: "statio", Address: "10.0.0.5", Port: 2222, Window: true,
	}, ""); err != nil {
		t.Fatalf("save the window: %v", err)
	}
	saveHostNamed(t, a, "margit", "10.0.0.6")
	a.refreshServers()

	// The three the window holds itself, put there by hand: this is a
	// test of the rule that reads them, not of the paths that fill them.
	pretendWindow(t, a, "taken", "10.0.0.7:2222")
	pretendMachine(t, a, "live")
	pretendDialling(t, a, "onitsway")

	for _, want := range []struct {
		host string
		kind hostKind
	}{
		{conns.Local, hostHere},
		{"taken", hostWindow},
		{"statio", hostSavedWindow},
		{"live", hostMachine},
		{"onitsway", hostConnecting},
		{"margit", hostSavedMachine},
		{"nobody", hostUnknown},
	} {
		if got := a.about(want.host).kind; got != want.kind {
			t.Errorf("%q is a %v, want a %v", want.host, got, want.kind)
		}
	}
}

// The machine -ssh put the panes on is here too, and says it is not the
// one gridterm is running on.
//
// The two used to disagree: the here-commands counted it as here and the
// file manager did not, so "browse files on it" was registered and could
// never work.
func TestAboutCountsTheSshTargetAsHere(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	a.localHost = "tester@box"

	far := a.about("tester@box")
	if far.kind != hostHere {
		t.Errorf("the machine every pane runs on is a %v, want here", far.kind)
	}
	if far.local {
		t.Error("it says it is the machine gridterm is running on")
	}
	here := a.about(conns.Local)
	if here.kind != hostHere || !here.local {
		t.Errorf("this machine is a %v (local %v)", here.kind, here.local)
	}
}

// A connection made under a name beats that name being "here", because
// it is a machine the window reached rather than the one it runs on.
func TestAConnectionUnderTheLocalNameIsAMachine(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	a.localHost = "tester@box"
	pretendMachine(t, a, "tester@box")

	if got := a.about("tester@box").kind; got != hostMachine {
		t.Errorf("it is a %v, want the connection to win", got)
	}
}

// serves stays true once a window is taken over, because "saved as a
// window" and "taken over" are two different facts.
func TestASavedWindowStillServesOnceItIsTakenOver(t *testing.T) {
	client := aWindowTakenOverAs(t, "statio")

	f := client.about("statio")
	if f.kind != hostWindow {
		t.Errorf("it is a %v, want a window", f.kind)
	}
	if !f.serves || !f.saved {
		t.Errorf("saved %v, serves %v, want both", f.saved, f.serves)
	}
	if f.toTakeOver() {
		t.Error("it says a window already taken over is still to be taken over")
	}
}

// Three names that are two things at once, because the server list and
// what the window holds are answered separately.
//
// Each pair decides whether asking for a terminal means taking a window
// over. A connection under a name the list saves as a window is not that
// window, and a window already held is not one to take over again.
func TestAboutOnANameThatIsTwoThings(t *testing.T) {
	saveWindow := func(t *testing.T, a *testApp, name, address string, port int) {
		t.Helper()
		if err := a.book.Put(remote.Host{
			Name: name, Address: address, Port: port, Window: true,
		}, ""); err != nil {
			t.Fatalf("save the window: %v", err)
		}
		a.refreshServers()
	}

	t.Run("a connection under a saved window's name", func(t *testing.T) {
		a := newTestApp(t, 90, 30)
		withDialogs(t, a)
		saveWindow(t, a, "statio", "10.0.0.5", 2222)
		pretendMachine(t, a, "statio")

		f := a.about("statio")
		if f.kind != hostMachine || !f.serves {
			t.Errorf("it is a %v (serves %v), want a connected machine the list calls a window",
				f.kind, f.serves)
		}
		if !f.toTakeOver() {
			t.Error("the list says it is a window, so a terminal on it means taking it over")
		}
	})

	t.Run("a dial under a saved window's name", func(t *testing.T) {
		a := newTestApp(t, 90, 30)
		withDialogs(t, a)
		saveWindow(t, a, "statio", "10.0.0.5", 2222)
		pretendDialling(t, a, "statio")

		f := a.about("statio")
		if f.kind != hostConnecting || !f.serves {
			t.Errorf("it is a %v (serves %v), want a dial under a name the list calls a window",
				f.kind, f.serves)
		}
		if !f.toTakeOver() {
			t.Error("the list says it is a window, so a terminal on it means taking it over")
		}
	})

	t.Run("a saved window already held under another name", func(t *testing.T) {
		a := newTestApp(t, 90, 30)
		withDialogs(t, a)
		saveWindow(t, a, "office", "10.0.0.5", 2222)
		// Taken over by address, before the list knew the window at all.
		pretendWindow(t, a, "10.0.0.5:2222", "10.0.0.5:2222")

		f := a.about("office")
		if f.kind != hostSavedWindow {
			t.Errorf("it is a %v, want a saved window: the maps are keyed by the address", f.kind)
		}
		if f.heldAt == nil {
			t.Fatal("it does not see the window already held at its address")
		}
		if f.toTakeOver() {
			t.Error("it would take over a window this one is already holding")
		}
	})
}

// The case rule: the book folds case, the maps do not.
//
// The two cannot be made one. A rename from "picard" to "Picard" is the
// same name to the book and a new key to the maps, and a guard that
// folded case for both would refuse that rename as a name already taken.
func TestAboutFoldsCaseForTheBookAndNotForTheMaps(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	if err := a.book.Put(remote.Host{
		Name: "statio", Address: "10.0.0.5", Port: 2222, Window: true,
	}, ""); err != nil {
		t.Fatalf("save the window: %v", err)
	}
	a.refreshServers()

	shouted := a.about("STATIO")
	if !shouted.saved || !shouted.serves {
		t.Errorf("saved %v, serves %v: the book folds case", shouted.saved, shouted.serves)
	}
	if shouted.spelling != "statio" {
		t.Errorf("the list spells it %q, want its own spelling", shouted.spelling)
	}
	if got := shouted.record().Name; got != "statio" {
		t.Errorf("the record is %q, want the list's own spelling", got)
	}
	if shouted.name != "STATIO" {
		t.Errorf("the name to act on is %q, want the name as it was asked about", shouted.name)
	}

	pretendWindow(t, a, "statio", "10.0.0.5:2222")
	if got := a.about("STATIO").window; got != nil {
		t.Error("the maps answered a name they are not keyed by")
	}
	if got := a.about("statio").window; got == nil {
		t.Error("the maps did not answer the name they are keyed by")
	}
}

// A saved window taken over opens a terminal however its name is
// capitalised, because the book folds case.
//
// Typed into the address dialog, which is the one way in that carries
// whatever capitals the user pressed.
func TestATerminalOnAWindowIgnoresCapitals(t *testing.T) {
	client := aWindowTakenOverAs(t, "statio")
	panes := len(client.panes)
	dials := dialCounter(client)

	connectByName(t, client, "STATIO")

	waitFor(t, client, "another pane on the window", func() bool {
		return len(client.panes) > panes
	})
	if *dials != 0 {
		t.Errorf("%d connections were prepared to dial; a window is taken over, not logged in to", *dials)
	}
	if n := len(client.windows); n != 1 {
		t.Errorf("it is holding %v, want the one window", mapKeys(client.windows))
	}
}

// connectByName types something into the address dialog and presses
// Connect.
func connectByName(t *testing.T, a *testApp, target string) {
	t.Helper()
	if _, err := a.root.HandleKey(press(input.KeyN, input.ModCtrl|input.ModShift)); err != nil {
		t.Fatalf("the connect chord: %v", err)
	}
	f := waitForDialog(t, a, "Connect to a server")
	typeIntoField(t, a, f, "Server", target)
	pressButton(t, a, f, "Connect")
}

// A window taken over by address and saved afterwards gives a terminal
// under its saved name, rather than the complaint that it has already
// been taken over.
//
// The connection is held under the address and the name is held under
// nothing, so a guard that asked by name saw a window nobody had.
func TestATerminalOnAWindowHeldUnderAnotherNameOpensOnIt(t *testing.T) {
	_, client, addr, keyFile := aServingWindow(t)
	if err := client.workOnWindow(addr, keyFile, nil); err != nil {
		t.Fatalf("take it over: %v", err)
	}
	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows[addr] != nil
	})
	if err := client.book.Put(remote.Host{
		Name: "office", Address: hostOf(t, addr), Port: portOf(t, addr),
		Window: true, Identities: []string{keyFile},
	}, ""); err != nil {
		t.Fatalf("save the window: %v", err)
	}
	client.refreshServers()
	panes := len(client.panes)

	if err := client.openTerminalOn("office", nil); err != nil {
		t.Fatalf("a terminal on it under its saved name: %v", err)
	}
	waitFor(t, client, "another pane on the window", func() bool {
		return len(client.panes) > panes
	})
	if n := len(client.windows); n != 1 {
		t.Errorf("it is holding %v, want the one window", mapKeys(client.windows))
	}
}

// aWindowHeldUnder is a window taken over by address and saved under a
// name afterwards, so what the window holds goes under the address while
// the server list calls it something else.
func aWindowHeldUnder(t *testing.T, name string) (client *testApp, addr string) {
	t.Helper()
	_, client, addr, keyFile := aServingWindow(t)
	if err := client.workOnWindow(addr, keyFile, nil); err != nil {
		t.Fatalf("take it over: %v", err)
	}
	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows[addr] != nil
	})
	if err := client.book.Put(remote.Host{
		Name: name, Address: hostOf(t, addr), Port: portOf(t, addr),
		Window: true, Identities: []string{keyFile},
	}, ""); err != nil {
		t.Fatalf("save the window: %v", err)
	}
	client.refreshServers()
	return client, addr
}

// Files on a saved window already taken over under another name are read
// over the connection being held.
//
// The pane asked about the saved name, which the maps hold nothing
// under, so the answer was that nothing is connected to it.
func TestFilesOnAWindowHeldUnderAnotherNameReadItsFiles(t *testing.T) {
	client, addr := aWindowHeldUnder(t, "office")

	// Something on the serving machine's disk to find. The pane is asked
	// for a directory by name, so the test does not depend on where
	// either window happens to be running.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "over-there.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The plus on its row, and the line that browses.
	chooseMenuItem(t, clickPlus(t, client, "office"), "conn.files")

	b := client.files
	if b == nil {
		t.Fatal("the window has no file manager")
	}
	panes := b.view.Panes()
	if len(panes) != 1 {
		t.Fatalf("the manager holds %d panes, want the one", len(panes))
	}
	pane := panes[0]
	if got := pane.FS().Name(); got != addr {
		t.Errorf("the pane reads %q, want the window held at %s", got, addr)
	}
	// And its sidebar row goes under the name holding the window, so the
	// sidebar keeps one heading for it.
	row := b.rows[pane]
	if row == nil {
		t.Fatal("the pane has no row on the sidebar")
	}
	if row.Host != addr {
		t.Errorf("the row is filed under %q, want %s", row.Host, addr)
	}

	var entries []vfs.Entry
	within(t, "read the directory over there", func() error {
		var err error
		entries, err = pane.FS().ReadDir(overThere(dir))
		return err
	})
	found := false
	for _, e := range entries {
		if e.Name == "over-there.txt" {
			found = true
		}
	}
	if !found {
		t.Errorf("the file was not there: %v", entries)
	}
}

// The plus on a saved window already taken over under another name
// offers what a window offers, rather than offering to take it over.
func TestThePlusOnAWindowHeldUnderAnotherNameOffersTheWindowsLines(t *testing.T) {
	client, _ := aWindowHeldUnder(t, "office")

	menu := clickPlus(t, client, "office")

	for _, item := range menu.Items() {
		if item.Title == "Take it over" {
			t.Errorf("it offers to take over a window it is already holding: %v", menuCommands(menu))
		}
	}
	for _, want := range []string{"conn.terminal", "conn.files", "conn.disconnect"} {
		if !offers(menu, want) {
			t.Errorf("it does not offer %s: %v", want, menuCommands(menu))
		}
	}
	// And letting go really lets go, rather than reporting that nothing
	// is connected to that name.
	chooseMenuItem(t, menu, "conn.disconnect")
	if n := len(client.windows); n != 0 {
		t.Errorf("it is still holding %v", mapKeys(client.windows))
	}
}

// The take-over dialog, given the address of a window this one already
// holds, opens a terminal on it.
//
// The dialog went straight to the code that takes a window over, which
// refuses one it already holds, so the answer to "work on that window"
// was a complaint.
func TestTheTakeOverDialogOnAHeldWindowOpensATerminal(t *testing.T) {
	_, client, addr, keyFile := aServingWindow(t)
	if err := client.workOnWindow(addr, keyFile, nil); err != nil {
		t.Fatalf("take it over: %v", err)
	}
	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows[addr] != nil
	})
	panes := len(client.panes)

	m := openBarMenu(t, client, "Servers")
	chooseMenuItem(t, m, "serve.takeOver")
	f := waitForDialog(t, client, "Take over a window")
	typeIntoField(t, client, f, "Machine", addr)
	typeIntoField(t, client, f, "Key file", keyFile)
	pressButton(t, client, f, "Take over")

	waitFor(t, client, "another pane on the window", func() bool {
		return len(client.panes) > panes
	})
	if n := len(client.windows); n != 1 {
		t.Errorf("it is holding %v, want the one window", mapKeys(client.windows))
	}
}

// A terminal on a saved machine asked for under other capitals is held
// under the server list's own spelling, so the sidebar keeps one
// heading for it.
//
// The maps are keyed exactly and the book folds case. Opening under the
// name as typed put the connection under one spelling and the pane's row
// under another, and the sidebar grew a second heading.
func TestATerminalOnASavedMachineUsesTheListsSpelling(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pinServers(t, a, s)
	saveHost(t, a, "margit", s, "")
	a.refreshServers()
	panes := len(a.panes)

	// The body of the "Connect to margit" command, asked with capitals
	// the command itself would not carry.
	if err := a.connectSaved("MARGIT"); err != nil {
		t.Fatalf("connectSaved: %v", err)
	}
	waitFor(t, a, "the machine to answer", func() bool {
		return a.machines["margit"] != nil && len(a.panes) > panes
	})
	if n := len(a.machines); n != 1 {
		t.Errorf("it is holding %v, want the one connection", mapKeys(a.machines))
	}

	a.refreshPanel(time.Now())
	var headings []string
	for _, row := range a.panel.Rows() {
		if row.Header && strings.EqualFold(strings.TrimSpace(row.Text), "margit") {
			headings = append(headings, row.Text)
		}
	}
	if len(headings) != 1 {
		t.Errorf("the sidebar has %d headings for the machine: %v", len(headings), headings)
	}
}

// A command needs a shell, so it is refused on a window and offered on
// a machine.
//
// The guard asked whether the list saved the name as a window, so a
// machine connected under a name saved as a window was told it was a
// gridterm window, which it is not.
func TestACommandIsRefusedOnAWindowAndNotOnAMachine(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.book.Put(remote.Host{
		Name: "statio", Address: "10.0.0.5", Port: 2222, Window: true,
	}, ""); err != nil {
		t.Fatalf("save the window: %v", err)
	}
	a.refreshServers()

	// The plus on its row is the machine the user is looking at.
	clickPlus(t, a, "statio")
	err := a.openCommandHere()

	if err == nil {
		t.Fatal("it offered to run a command on a gridterm window")
	}
	if !strings.Contains(err.Error(), "no shell") {
		t.Errorf("it says %q, want it to say the window has no shell", err)
	}

	// The same name, with a connection held under it. That connection is
	// a machine with a shell, whatever the list calls the name.
	if _, err := a.root.HandleKey(press(input.KeyEscape, 0)); err != nil {
		t.Fatalf("closing the menu: %v", err)
	}
	a.pump.run()
	pretendMachine(t, a, "statio")
	clickPlus(t, a, "statio")
	if err := a.openCommandHere(); err != nil {
		t.Fatalf("it refused a command on a connected machine: %v", err)
	}
}

// pretendMachine, pretendWindow and pretendDialling record what the
// window would be holding, without connecting to anything.
//
// Taken away again when the test ends: nothing stands behind them, so a
// window closing one at the end of the test would close a nil connection.
func pretendMachine(t *testing.T, a *testApp, name string) {
	t.Helper()
	a.machines[name] = &machine{at: step{name: name}}
	t.Cleanup(func() { delete(a.machines, name) })
}

func pretendWindow(t *testing.T, a *testApp, name, addr string) {
	t.Helper()
	a.windows[name] = &taken{name: name, addr: addr}
	t.Cleanup(func() { delete(a.windows, name) })
}

func pretendDialling(t *testing.T, a *testApp, name string) {
	t.Helper()
	a.opening[name] = &dialling{names: []string{name}}
	t.Cleanup(func() { delete(a.opening, name) })
}

// dialCounter counts the connections that got as far as being prepared
// to dial, which is every login the window makes along a route.
//
// Taking over a window is an SSH login too, and this does not count it:
// that handshake is made by the take-over path, which prepares no
// config. So zero here means nothing went the way a machine goes.
//
// It wraps whatever prepare a test has already installed, so a machine
// still connects with the test server's host key.
func dialCounter(a *testApp) *int {
	var n int
	was := a.prepare
	a.prepare = func(cfg remote.Config) remote.Config {
		n++
		if was != nil {
			return was(cfg)
		}
		return cfg
	}
	return &n
}

// clickTerminalLine presses the plus on a machine's heading and runs the
// line that opens a terminal on it, whichever command is behind it.
func clickTerminalLine(t *testing.T, a *testApp, host string) {
	t.Helper()
	m := clickPlus(t, a, host)
	for _, item := range m.Items() {
		switch item.Title {
		case "Terminal", "Take it over":
			chooseMenuItem(t, m, item.Command)
			return
		}
	}
	t.Fatalf("the plus on %q offers nothing that opens a terminal: %v", host, menuCommands(m))
}

// runFromPalette opens the palette with its chord, types the command's
// own title, and presses Enter on it.
func runFromPalette(t *testing.T, a *testApp, id string) {
	t.Helper()
	cmd, ok := a.root.Commands.Lookup(id)
	if !ok {
		t.Fatalf("there is no command %q", id)
	}
	if _, err := a.root.HandleKey(press(input.KeyK, input.ModCtrl)); err != nil {
		t.Fatalf("the palette chord: %v", err)
	}
	p, ok := a.root.Modal().(*ui.Palette)
	if !ok {
		t.Fatalf("the palette chord opened %T", a.root.Modal())
	}
	for _, r := range cmd.Title {
		if _, err := a.root.HandleKey(input1(r)); err != nil {
			t.Fatalf("typing into the palette: %v", err)
		}
	}
	// The title is not always the best match for itself, so step down
	// the list the way a user reading it does.
	for i := 0; i < len(p.Matches()); i++ {
		if got, ok := p.Selected(); ok && got.ID == id {
			break
		}
		if _, err := a.root.HandleKey(press(input.KeyDown, 0)); err != nil {
			t.Fatalf("down the palette: %v", err)
		}
	}
	if got, ok := p.Selected(); !ok || got.ID != id {
		t.Fatalf("the palette has %q selected, want %q", got.ID, id)
	}
	if _, err := a.root.HandleKey(press(input.KeyEnter, 0)); err != nil {
		t.Fatalf("running %q from the palette: %v", id, err)
	}
	a.pump.run()
}

// openBarMenu opens one of the menu bar's menus the way a user does:
// F10, then along the titles with the arrow keys.
func openBarMenu(t *testing.T, a *testApp, title string) *ui.Menu {
	t.Helper()
	want := -1
	var titles []string
	for i, def := range a.bar.Menus {
		titles = append(titles, def.Title)
		if def.Title == title {
			want = i
		}
	}
	if want < 0 {
		t.Fatalf("there is no %q menu: %v", title, titles)
	}
	if _, err := a.root.HandleKey(press(input.KeyF10, 0)); err != nil {
		t.Fatalf("F10: %v", err)
	}
	for range a.bar.Menus {
		if a.bar.OpenIndex() == want {
			break
		}
		if _, err := a.root.HandleKey(press(input.KeyRight, 0)); err != nil {
			t.Fatalf("along the bar: %v", err)
		}
	}
	if a.bar.OpenIndex() != want {
		t.Fatalf("the bar stopped on %d, want %q at %d", a.bar.OpenIndex(), title, want)
	}
	m, ok := a.root.Modal().(*ui.Menu)
	if !ok {
		t.Fatalf("the bar opened %T, want a menu", a.root.Modal())
	}
	return m
}

// splitFromTheChord opens the split chooser the way the key does.
func splitFromTheChord(t *testing.T, a *testApp) *ui.Chooser {
	t.Helper()
	if _, err := a.root.HandleKey(press(input.KeyD, input.ModCtrl|input.ModShift)); err != nil {
		t.Fatalf("the split chord: %v", err)
	}
	c, ok := a.root.Modal().(*ui.Chooser)
	if !ok {
		t.Fatalf("the split chord opened %T, want a chooser", a.root.Modal())
	}
	return c
}

// wayIn is one way a user can ask for a terminal on a machine. Each one
// starts at the click, the menu line, the button or the chord, because
// every round of this bug was in a path the test did not take.
type wayIn struct {
	name string
	open func(t *testing.T, a *testApp, host, addr, keyFile string)
}

// waysIn are the ways in that work for a saved machine and for a saved
// window alike.
func waysIn() []wayIn {
	return []wayIn{
		{"the plus on its row", func(t *testing.T, a *testApp, host, addr, keyFile string) {
			clickTerminalLine(t, a, host)
		}},
		{"the palette", func(t *testing.T, a *testApp, host, addr, keyFile string) {
			runFromPalette(t, a, termPrefix+remote.CommandName(host))
		}},
		{"the split chooser", func(t *testing.T, a *testApp, host, addr, keyFile string) {
			takeChoice(t, splitFromTheChord(t, a), "Terminal on "+host)
		}},
		{"the Servers menu", func(t *testing.T, a *testApp, host, addr, keyFile string) {
			m := openBarMenu(t, a, "Servers")
			chooseMenuItem(t, m, openPrefix+remote.CommandName(host))
		}},
		{"connect to a server, by address", func(t *testing.T, a *testApp, host, addr, keyFile string) {
			if _, err := a.root.HandleKey(press(input.KeyN, input.ModCtrl|input.ModShift)); err != nil {
				t.Fatalf("the connect chord: %v", err)
			}
			f := waitForDialog(t, a, "Connect to a server")
			typeIntoField(t, a, f, "Server", addr)
			pressButton(t, a, f, "Connect")
		}},
	}
}

// A saved gridterm window is taken over whichever way the user asks for
// a terminal on it, and nothing anywhere tries to log in to it.
//
// This is the bug that came back four times: each fix went into the one
// path the fixer had in mind, and the next round came in through
// another. So each row here starts where the user starts.
//
// The table is the user-facing net rather than a test of one guard. Two
// guards stand behind it -- openTerminalOn, and openRoute for the ways
// in that build a route -- and a row passes when either holds.
func TestEveryWayInTakesOverASavedWindow(t *testing.T) {
	ways := append(waysIn(), wayIn{
		"take over a window, by address",
		func(t *testing.T, a *testApp, host, addr, keyFile string) {
			m := openBarMenu(t, a, "Servers")
			chooseMenuItem(t, m, "serve.takeOver")
			f := waitForDialog(t, a, "Take over a window")
			typeIntoField(t, a, f, "Machine", addr)
			typeIntoField(t, a, f, "Key file", keyFile)
			pressButton(t, a, f, "Take over")
		},
	})

	for _, way := range ways {
		t.Run(way.name, func(t *testing.T) {
			host, client, addr, keyFile := aServingWindow(t)
			if err := client.book.Put(remote.Host{
				Name: "statio", Address: hostOf(t, addr), Port: portOf(t, addr),
				Window: true, Identities: []string{keyFile},
			}, ""); err != nil {
				t.Fatalf("save the window: %v", err)
			}
			client.refreshServers()
			dials := dialCounter(client)

			way.open(t, client, "statio", addr, keyFile)

			waitFor(t, client, "the window to be taken over", func() bool {
				return client.windows["statio"] != nil
			})
			if *dials != 0 {
				t.Errorf("%d connections were prepared to dial; a window is taken over, not logged in to", *dials)
			}
			if n := len(client.machines); n != 0 {
				t.Errorf("it is holding %v as machines", mapKeys(client.machines))
			}
			waitFor(t, client, "a pane drawn from the window", func() bool {
				return len(client.paneOnWindow) > 0
			})
			if n := len(client.windows); n != 1 {
				t.Errorf("it is holding %v, want the one window", mapKeys(client.windows))
			}
			if n := len(host.serving.clients()); n != 1 {
				t.Errorf("the serving window saw %d clients, want the one", n)
			}
		})
	}
}

// The same ways in, against a saved machine: each one logs in, once.
//
// The other half of the rule. A guard that refused everything would pass
// the window table and fail here.
func TestEveryWayInDialsASavedMachineOnce(t *testing.T) {
	for _, way := range waysIn() {
		t.Run(way.name, func(t *testing.T) {
			s := sshtest.New(t)
			a := newTestApp(t, 90, 30)
			withDialogs(t, a)
			withPanel(t, a)
			withMenubar(t, a)
			pinServers(t, a, s)
			saveHost(t, a, "margit", s, "")
			a.refreshServers()
			dials := dialCounter(a)
			panes := len(a.panes)

			addr := serverConfig(t, s).Target()
			way.open(t, a, "margit", addr, "")

			waitFor(t, a, "the machine to answer", func() bool {
				return len(a.machines) > 0 && len(a.panes) > panes
			})
			if *dials != 1 {
				t.Errorf("%d connections were prepared to dial, want the one", *dials)
			}
			if n := s.Conns(); n != 1 {
				t.Errorf("the server saw %d logins, want the one", n)
			}
			if n := len(a.windows); n != 0 {
				t.Errorf("it took over %v, which is a machine", mapKeys(a.windows))
			}
		})
	}
}

// And against this machine, where a terminal is one more pane and
// nothing connects anywhere.
func TestEveryWayInOpensAPaneHere(t *testing.T) {
	for _, way := range []wayIn{
		{"the plus on its row", func(t *testing.T, a *testApp, host, addr, keyFile string) {
			clickTerminalLine(t, a, host)
		}},
		{"the palette", func(t *testing.T, a *testApp, host, addr, keyFile string) {
			runFromPalette(t, a, termPrefix+remote.CommandName(host))
		}},
		{"the split chooser", func(t *testing.T, a *testApp, host, addr, keyFile string) {
			c := splitFromTheChord(t, a)
			// A terminal here is the first line, so the machine itself
			// is not offered a second time further down.
			for _, text := range choiceTexts(c) {
				// Nothing else is saved or open, so any machine the
				// chooser names is this one, offered a second time.
				if strings.HasPrefix(text, "Terminal on") {
					t.Fatalf("this machine is offered twice: %v", choiceTexts(c))
				}
			}
			takeChoice(t, c, "New terminal")
		}},
	} {
		t.Run(way.name, func(t *testing.T) {
			a := newTestApp(t, 90, 30)
			withDialogs(t, a)
			withPanel(t, a)
			withMenubar(t, a)
			a.refreshServers()
			dials := dialCounter(a)
			panes := len(a.panes)

			way.open(t, a, conns.Local, "", "")

			waitFor(t, a, "another pane here", func() bool { return len(a.panes) > panes })
			if *dials != 0 {
				t.Errorf("%d connections were prepared to dial for a pane on this machine", *dials)
			}
			if n := len(a.machines) + len(a.windows); n != 0 {
				t.Errorf("it opened %d connections for a pane on this machine", n)
			}
		})
	}
}

// aServingWindow is a window serving, and another ready to take it over
// without being asked to trust its key.
//
// The key is written into the client's own known_windows, so every row
// of the table is the same shape: nothing to answer, and a row that
// fails says what did not happen rather than timing out on a dialog.
func aServingWindow(t *testing.T) (host, client *testApp, addr, keyFile string) {
	t.Helper()
	host = newTestApp(t, 90, 30)
	withDialogs(t, host)
	keyFile, line := aKeyFile(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	addr = host.serving.addr()

	client = newTestApp(t, 90, 30)
	withDialogs(t, client)
	withPanel(t, client)
	withMenubar(t, client)
	if err := os.WriteFile(client.knownWindowsAt, []byte(knownLine(t, host)), 0o600); err != nil {
		t.Fatalf("write the known window: %v", err)
	}
	return host, client, addr, keyFile
}

// aWindowTakenOverAs is a window serving under a name in the server
// list, already taken over by the window this hands back.
func aWindowTakenOverAs(t *testing.T, name string) *testApp {
	t.Helper()
	_, client, addr, keyFile := aServingWindow(t)
	if err := client.book.Put(remote.Host{
		Name: name, Address: hostOf(t, addr), Port: portOf(t, addr),
		Window: true, Identities: []string{keyFile},
	}, ""); err != nil {
		t.Fatalf("save the window: %v", err)
	}
	client.refreshServers()
	if err := client.openTerminalOn(name, nil); err != nil {
		t.Fatalf("take it over: %v", err)
	}
	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows[name] != nil
	})
	return client
}

// A terminal on a window asked for from the split chooser lands in the
// split, the way one on a machine does.
//
// The chooser went straight to the code that logs in, so a window was
// refused outright; and the take-over path had nowhere to put a spot, so
// even once that was fixed the pane would have become a tab.
func TestSplittingWithAWindowPutsThePaneInTheSplit(t *testing.T) {
	_, client, addr, keyFile := aServingWindow(t)
	if err := client.book.Put(remote.Host{
		Name: "statio", Address: hostOf(t, addr), Port: portOf(t, addr),
		Window: true, Identities: []string{keyFile},
	}, ""); err != nil {
		t.Fatalf("save the window: %v", err)
	}
	client.refreshServers()

	first := client.focusedTerminal()
	takeChoice(t, splitFromTheChord(t, client), "Terminal on statio")

	waitFor(t, client, "the window to be taken over", func() bool {
		return client.windows["statio"] != nil && len(client.paneOnWindow) > 0
	})
	if got := len(client.stage.Children()); got != 1 {
		t.Fatalf("the stage holds %d things, want the one split", got)
	}
	split, ok := client.stage.Children()[0].(*ui.Split)
	if !ok {
		t.Fatalf("the pane landed in %T rather than a split", client.stage.Children()[0])
	}
	if split.Dir() != ui.Columns {
		t.Errorf("the split runs %v, want the columns the split-right chord makes", split.Dir())
	}
	kids := split.Children()
	if len(kids) != 2 || kids[0] != ui.Widget(first) {
		t.Fatalf("the split holds %v, want the pane that was split first", kids)
	}
	next, ok := kids[1].(*term.Terminal)
	if !ok {
		t.Fatalf("the other half is %T, want a terminal", kids[1])
	}
	if client.paneOnWindow[next] == nil {
		t.Error("the other half is not the pane drawn from the window")
	}
	checkTree(t, client)
}

// The row for a saved window says what picking it costs, which is taking
// it over rather than a login.
func TestTheSplitChooserSaysAWindowIsTakenOver(t *testing.T) {
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.book.Put(remote.Host{
		Name: "statio", Address: "10.0.0.5", Port: 2222, Window: true,
	}, ""); err != nil {
		t.Fatalf("save the window: %v", err)
	}
	a.refreshServers()

	c := splitFromTheChord(t, a)
	for _, row := range c.Rows() {
		if strings.TrimSpace(row.Text) == "Terminal on statio" {
			if row.Note != "takes it over" {
				t.Errorf("the row says %q, want it to say a window is taken over", row.Note)
			}
			return
		}
	}
	t.Fatalf("the chooser does not offer the window: %v", choiceTexts(c))
}
