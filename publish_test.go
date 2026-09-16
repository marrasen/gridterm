package main

import (
	"slices"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// aWindowConnectedToMargit is two windows, with the serving one also
// connected to a machine called margit and a shell open on it.
func aWindowConnectedToMargit(t *testing.T) (host, client *testApp, addr string) {
	t.Helper()
	host, client, addr = twoWindows(t)
	s := sshtest.New(t)
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

// remoteRowsOf is what this window shows under a window taken over, the
// way the sidebar builds it: this window's own rows filed under the
// window are handed in, so the ones on a machine over there land under
// that machine.
func remoteRowsOf(a *testApp, addr string) []ui.ListRow {
	on := a.about(addr)
	now := time.Now()
	var mine []conns.Row
	for _, group := range a.registry.Groups(now) {
		if group.Host == on.name {
			mine = group.Rows
		}
	}
	return a.remoteRows(on, mine, now)
}

// drawnHeadings are the machine headings the sidebar draws under a
// window taken over, as the user sees them.
func drawnHeadings(a *testApp, addr string) []string {
	a.refreshPanel(time.Now())
	var heads []string
	for _, row := range a.panel.Rows() {
		if key, ok := row.Key.(remoteHostKey); ok && row.Header && key.window == a.windows.at(addr) {
			heads = append(heads, row.Text)
		}
	}
	return heads
}

// drawnUnder are the rows the sidebar draws under a heading, up to the
// next heading.
func drawnUnder(t *testing.T, a *testApp, key any) []ui.ListRow {
	t.Helper()
	a.refreshPanel(time.Now())
	rows := a.panel.Rows()
	for i, row := range rows {
		if row.Key != key {
			continue
		}
		var out []ui.ListRow
		for _, r := range rows[i+1:] {
			if r.Header {
				break
			}
			out = append(out, r)
		}
		return out
	}
	t.Fatalf("the sidebar has no heading %v: %v", key, panelText(a, time.Now()))
	return nil
}

// remoteRowOn is the row this window shows for a screen on a machine
// of a window taken over, by the machine's name.
func remoteRowOn(t *testing.T, a *testApp, addr, host string) remoteKey {
	t.Helper()
	for _, row := range remoteRowsOf(a, addr) {
		key, ok := row.Key.(remoteKey)
		if !ok {
			continue
		}
		if open, there := a.openOver(key); there && open.Host == host {
			return key
		}
	}
	t.Fatalf("no row for a screen on %s: %v", host, remoteRowsOf(a, addr))
	return remoteKey{}
}

// paneOn is the pane a window has on a machine, which has to be the one.
func paneOn(t *testing.T, a *testApp, host string) *term.Terminal {
	t.Helper()
	var found *term.Terminal
	for pane, e := range a.panes {
		if e.Host != host {
			continue
		}
		if found != nil {
			t.Fatalf("more than one pane on %q", host)
		}
		found = pane
	}
	if found == nil {
		t.Fatalf("no pane on %q", host)
	}
	return found
}

// watchFromTheSidebar chooses a screen over there and waits until the
// window over there says somebody is watching it.
//
// The pane here opens the moment the row is chosen, whether or not the
// far end has attached yet, so the far end is what is waited for.
func watchFromTheSidebar(t *testing.T, host, client *testApp, row remoteKey, far *term.Terminal) *term.Terminal {
	t.Helper()
	attachFromTheSidebar(t, client, row)
	waitFor(t, host, "the window over there to be watched", func() bool {
		return far.Watched() > 0
	}, client)
	pane := client.windows.watcher(row)
	if pane == nil {
		t.Fatal("nothing here is watching the screen chosen")
	}
	return pane
}

// A machine's heading stays when the screen under it is watched here,
// and the pane watching it is drawn under that heading.
//
// The heading used to be worked out from the rows under it, so choosing
// the one row on a machine took the heading away with it, and the user
// was left wondering where the machine had gone.
func TestAMachineHeadingStaysWhenItsScreenIsWatched(t *testing.T) {
	host, client, addr := aWindowConnectedToMargit(t)

	row := remoteRowOn(t, client, addr, "margit")
	pane := watchFromTheSidebar(t, host, client, row, paneOn(t, host, "margit"))

	// Drawn: the heading, with the watching pane's own row under it and
	// no second row for the screen it is watching.
	heading := remoteHostKey{window: windowAt(t, client, addr), host: "margit"}
	under := drawnUnder(t, client, heading)
	var mine, twice bool
	for _, r := range under {
		if r.Key == client.panes[pane] {
			mine = true
		}
		if r.Key == row {
			twice = true
		}
	}
	if !mine {
		t.Errorf("the watching pane is not drawn under margit: %v", panelText(client, time.Now()))
	}
	if twice {
		t.Error("the screen is still listed under margit as well as having a pane here")
	}
}

// A machine the window over there is connected to has a heading with
// nothing open on it, the same as a machine of this window's own.
func TestAMachineOverThereHasAHeadingWithNothingOpenOnIt(t *testing.T) {
	host, client, addr := aWindowConnectedToMargit(t)

	// The one shell on margit closed over there. The connection stays.
	if err := host.closePane(paneOn(t, host, "margit")); err != nil {
		t.Fatalf("close the pane on margit: %v", err)
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

	if heads := drawnHeadings(client, addr); !slices.Contains(heads, "margit") {
		t.Errorf("headings %v, want margit while the window over there is connected to it", heads)
	}
	heading := remoteHostKey{window: windowAt(t, client, addr), host: "margit"}
	if under := drawnUnder(t, client, heading); len(under) != 0 {
		t.Errorf("rows under margit with nothing open on it: %v", under)
	}
}

// A screen over there that has finished is not listed: there is nothing
// left on it to watch, and the window over there shows it greyed for
// whoever is sitting at it.
func TestAFinishedScreenOverThereIsNotListed(t *testing.T) {
	host, client, addr := twoWindows(t)
	own := host.panes[paneOn(t, host, conns.Local)].ID()

	// A machine over there, saved under its own address so a command
	// can be run on it by name.
	s := sshtest.New(t)
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

	client.refreshPanel(time.Now())
	held := windowAt(t, client, addr)
	// The shell still running over there is offered, so an empty list
	// proves nothing.
	if _, ok := panelRow(client, remoteKey{window: held, id: own}); !ok {
		t.Fatalf("the shell over there is not offered, so this proves nothing: %v", panelText(client, time.Now()))
	}
	if row, ok := panelRow(client, remoteKey{window: held, id: done.ID()}); ok {
		t.Errorf("the finished command is listed as something to watch: %v", row)
	}
}

// A screen watched here goes without leaving a row when the shell in it
// ends over there.
//
// A shell of this window's own that ends keeps a greyed row saying so.
// One over there never had a row on that window, and what ended it is
// that window's business, so a greyed row here said nothing the user
// could use. The window over there keeps its own greyed row and says
// so, and that is not listed here either.
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
	pane := watchFromTheSidebar(t, host, client, row, hostPane)
	mine := client.panes[pane]
	panes := len(client.panes)

	// The shell over there ends, the way a shell does.
	if err := host.shells[0].Close(); err != nil {
		t.Fatalf("end the shell over there: %v", err)
	}
	reapWhenTold(t, host)
	// And this window has both heard that it ended and let the pane go.
	waitFor(t, host, "the watching pane to go and the window over there to say the shell finished", func() bool {
		host.refreshPanel(time.Now())
		client.reapExited()
		open, still := client.openOver(row)
		return len(client.panes) == panes-1 && (!still || open.State == meter.Closed.String())
	}, client)

	// Nothing drawn for it under the window: not the pane's row, not
	// the screen over there, and nothing greyed.
	for _, r := range drawnUnder(t, client, hostKey(addr)) {
		switch {
		case r.Key == mine:
			t.Errorf("the watching pane's row was left behind: %q", r.Text)
		case r.Key == row:
			t.Errorf("the finished shell over there is still listed: %q", r.Text)
		case r.FG == client.colours.ANSI[8]:
			t.Errorf("a greyed row was left behind: %q", r.Text)
		}
	}
}

// A shell opened on the window from here goes without leaving a row
// when it ends.
func TestAShellOnTheWindowGoesWithoutARow(t *testing.T) {
	host, client, addr := twoWindows(t)
	pane := paneOnTheWindow(t, client)
	mine := client.panes[pane]
	panes := len(client.panes)

	// The shell the window over there started for this one is the one
	// after its own.
	host.shellsMu.Lock()
	served := len(host.shells)
	host.shellsMu.Unlock()
	if served != 2 {
		t.Fatalf("the window over there started %d shells, want its own and this one's", served)
	}
	if err := host.shells[1].Close(); err != nil {
		t.Fatalf("end the shell over there: %v", err)
	}
	waitFor(t, host, "the pane to go", func() bool {
		client.reapExited()
		return len(client.panes) == panes-1
	}, client)

	for _, r := range drawnUnder(t, client, hostKey(addr)) {
		if r.Key == mine || r.FG == client.colours.ANSI[8] {
			t.Errorf("a row was left behind for the shell that ended: %q", r.Text)
		}
	}
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
