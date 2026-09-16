package main

import (
	"slices"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/ui"
)

// aWindowConnectedToMargit is two windows, with the serving one also
// connected to a machine called margit and a shell open on it.
func aWindowConnectedToMargit(t *testing.T) (host, client *testApp, addr string) {
	t.Helper()
	host, client, addr = twoWindows(t)
	s := sshtest.New(t)
	withDialogs(t, host)
	pinServers(t, host, s)
	host.connectAs("margit", host.prepare(serverConfig(t, s)))
	// Connected, not only connecting: the window over there lists the
	// connection itself once it holds it, and a shell on it with a
	// screen.
	waitFor(t, host, "the window over there to hold margit and a shell on it", func() bool {
		host.refreshPanel(time.Now())
		var held, shell bool
		for _, open := range client.windows.named(addr).win.Opens() {
			if open.Host != "margit" {
				continue
			}
			if open.Kind == conns.Server.String() {
				held = true
			} else if open.HasScreen() {
				shell = true
			}
		}
		return held && shell
	}, client)
	return host, client, addr
}

// remoteHeadings are the machine headings this window shows under a
// window taken over.
func remoteHeadings(a *testApp, addr string) []string {
	var heads []string
	for _, row := range a.remoteRows(a.about(addr)) {
		if row.Header {
			heads = append(heads, row.Text)
		}
	}
	return heads
}

// remoteRowOn is the row this window shows for a screen on a machine
// of a window taken over, by the machine's name.
func remoteRowOn(t *testing.T, a *testApp, addr, host string) remoteKey {
	t.Helper()
	for _, row := range a.remoteRows(a.about(addr)) {
		key, ok := row.Key.(remoteKey)
		if !ok {
			continue
		}
		if open, there := a.openOver(key); there && open.Host == host {
			return key
		}
	}
	t.Fatalf("no row for a screen on %s: %v", host, a.remoteRows(a.about(addr)))
	return remoteKey{}
}

// A machine's heading stays when the screen under it is watched here.
//
// The heading used to be worked out from the rows under it, so choosing
// the one row on a machine took the heading away with it, and the user
// was left wondering where the machine had gone.
func TestAMachineHeadingStaysWhenItsScreenIsWatched(t *testing.T) {
	_, client, addr := aWindowConnectedToMargit(t)

	row := remoteRowOn(t, client, addr, "margit")
	panes := len(client.panes)
	attachFromTheSidebar(t, client, row)
	waitFor(t, client, "a pane watching the screen on margit", func() bool {
		return len(client.panes) == panes+1
	})

	// The screen has a row of its own now, not one under the heading.
	if client.windows.watcher(row) == nil {
		t.Fatal("nothing is watching the screen chosen")
	}
	for _, r := range client.remoteRows(client.about(addr)) {
		if r.Key == row {
			t.Error("the screen is still listed under the machine as well as having a pane here")
		}
	}
	// And the heading is still there.
	if heads := remoteHeadings(client, addr); !slices.Contains(heads, "margit") {
		t.Errorf("headings %v, want margit still among them", heads)
	}
}

// A machine the window over there is connected to has a heading with
// nothing open on it, the same as a machine of this window's own.
func TestAMachineOverThereHasAHeadingWithNothingOpenOnIt(t *testing.T) {
	host, client, addr := aWindowConnectedToMargit(t)

	// The one shell on margit closed over there. The connection stays.
	for pane, e := range host.panes {
		if e.Host == "margit" {
			if err := host.closePane(pane); err != nil {
				t.Fatalf("close the pane on margit: %v", err)
			}
		}
	}
	waitFor(t, host, "the window over there to stop listing a screen on margit", func() bool {
		host.refreshPanel(time.Now())
		for _, open := range client.windows.named(addr).win.Opens() {
			if open.Host == "margit" && open.HasScreen() {
				return false
			}
		}
		return true
	}, client)

	if heads := remoteHeadings(client, addr); !slices.Contains(heads, "margit") {
		t.Errorf("headings %v, want margit while the window over there is connected to it", heads)
	}
}

// A screen over there that has finished is not listed: there is nothing
// left on it to watch, and the window over there shows it greyed for
// whoever is sitting at it.
func TestAFinishedScreenOverThereIsNotListed(t *testing.T) {
	host, client, addr := twoWindows(t)

	// A machine over there, saved under its own address so a command
	// can be run on it by name.
	s := sshtest.New(t)
	withDialogs(t, host)
	pinServers(t, host, s)
	host.connect(serverConfig(t, s))
	waitForPanes(t, host, 2)
	margit := serverConfig(t, s).Target()

	if err := host.openOn(margit, []string{"uname", "-a"}, nil); err != nil {
		t.Fatalf("run a command on %s: %v", margit, err)
	}
	var done *conns.Entry
	waitFor(t, host, "the command to finish over there", func() bool {
		for _, e := range host.panes {
			if e.Kind == conns.Command && e.State(time.Now()) == meter.Closed {
				done = e
				return true
			}
		}
		return false
	}, client)
	// Told to this window, as finished.
	waitFor(t, host, "the window over there to say the command finished", func() bool {
		host.refreshPanel(time.Now())
		for _, open := range client.windows.named(addr).win.Opens() {
			if open.ID == done.ID() && open.State == meter.Closed.String() {
				return true
			}
		}
		return false
	}, client)

	for _, row := range client.remoteRows(client.about(addr)) {
		if key, ok := row.Key.(remoteKey); ok && key.id == done.ID() {
			t.Errorf("the finished command is listed as something to watch: %v", row)
		}
	}
}

// A screen watched here goes without leaving a row when the window over
// there closes it.
//
// A shell of this window's own that ends keeps a greyed row saying so.
// One over there never had a row on that window, and what ended it is
// that window's business, so a greyed row here said nothing the user
// could use.
func TestAScreenWatchedHereGoesWithoutARow(t *testing.T) {
	host, client, addr := twoWindows(t)

	hostPane := onlyPaneOn(t, host)
	held := windowAt(t, client, addr)
	row := remoteKey{window: held, id: host.panes[hostPane].ID()}
	waitFor(t, host, "a row for the shell over there", func() bool {
		host.refreshPanel(panelNow)
		client.refreshPanel(panelNow)
		_, ok := panelRow(client, row)
		return ok
	}, client)
	panes := len(client.panes)
	attachFromTheSidebar(t, client, row)
	waitFor(t, host, "a pane watching it", func() bool {
		return len(client.panes) == panes+1
	}, client)
	rows := len(rowsUnderWindow(client, addr))

	// Closed over there, by whoever sits at that window.
	if err := host.closePane(hostPane); err != nil {
		t.Fatalf("close the pane over there: %v", err)
	}
	waitFor(t, host, "the watching pane to go", func() bool {
		client.reapExited()
		return len(client.panes) == panes
	}, client)

	// No row left behind for it, greyed or otherwise.
	client.refreshPanel(time.Now())
	under := rowsUnderWindow(client, addr)
	if len(under) != rows-1 {
		t.Errorf("%d rows under the window, want %d: %v", len(under), rows-1, panelText(client, time.Now()))
	}
	for _, r := range under {
		if r.State == meter.Closed {
			t.Errorf("a finished row was left behind: %q", r.Label)
		}
	}
}

// rowsUnderWindow are the rows of this window's own filed under a
// window taken over.
func rowsUnderWindow(a *testApp, addr string) []conns.Row {
	for _, g := range a.registry.Groups(time.Now()) {
		if g.Host == addr {
			return g.Rows
		}
	}
	return nil
}

// A window's heading says its name and nothing else: the address when
// nothing is saved at it, and the saved name once something is.
//
// It used to carry the address as a note as well, and in a sidebar of
// ordinary width the note took the room the name needed.
func TestAWindowsHeadingSaysOnlyItsName(t *testing.T) {
	_, client, addr, keyFile := aServingWindow(t)
	takeOverFromTheDialog(t, client, addr, keyFile)

	heading := func(name string) ui.ListRow {
		t.Helper()
		client.refreshPanel(time.Now())
		row, ok := panelRow(client, hostKey(name))
		if !ok {
			t.Fatalf("no heading for %q: %v", name, panelText(client, time.Now()))
		}
		return row
	}
	if row := heading(addr); row.Text != addr || row.Note != "" {
		t.Errorf("the heading says %q with note %q, want the address alone", row.Text, row.Note)
	}

	saveWindowFromTheDialog(t, client, "office", addr, keyFile)
	if row := heading("office"); row.Text != "office" || row.Note != "" {
		t.Errorf("the heading says %q with note %q, want the saved name alone", row.Text, row.Note)
	}
}
