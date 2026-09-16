package main

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/ui"
)

// aBarWindow is a window with a menu bar over its tree, serving nothing.
func aBarWindow(t *testing.T) *testApp {
	t.Helper()
	// Wide enough for the whole of a status beside every title, so a
	// test reads what the bar says rather than what it had room for.
	a := newTestApp(t, 120, 30)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	return a
}

// aServedWindow is a window with a menu bar, serving on a port of its
// own with nobody connected.
func aServedWindow(t *testing.T) *testApp {
	t.Helper()
	a := aBarWindow(t)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	return a
}

// barRow sets the status the way a frame does, draws the window, and
// reads the menu bar's row back with where the bar sits.
func barRow(t *testing.T, a *testApp) (ui.Rect, string) {
	t.Helper()
	a.updateStatus()
	a.root.Draw(a.g.View())
	area, ok := a.root.AreaOf(a.bar)
	if !ok {
		t.Fatal("the menu bar is not in the tree")
	}
	var b strings.Builder
	for x := area.X; x < area.X+area.Cols; x++ {
		r := a.g.At(x, area.Y).Rune
		if r == 0 {
			r = ' '
		}
		b.WriteRune(r)
	}
	return area, b.String()
}

// titlesEnd is the column the menu titles run out at, which is where the
// room for a status starts.
func titlesEnd(a *testApp) int {
	end := 0
	for _, def := range a.bar.Menus {
		end += len([]rune(def.Title)) + 2
	}
	return end
}

// statusColumn is the last column the status is drawn on, for a press
// that lands on it.
func statusColumn(t *testing.T, a *testApp) (int, int) {
	t.Helper()
	area, row := barRow(t, a)
	// By column, not by byte: a trimmed status starts with the mark that
	// says it was cut, which is three bytes of one column.
	at := -1
	columns := []rune(row)
	for x := len(columns) - 1; x >= 0; x-- {
		if columns[x] != ' ' {
			at = x
			break
		}
	}
	if at < titlesEnd(a) {
		t.Fatalf("the bar row is %q, want a status past the titles", row)
	}
	return area.X + at, area.Y
}

// A window nobody is being served says nothing on the bar.
func TestAWindowThatIsNotServedSaysNothingOnTheMenuBar(t *testing.T) {
	a := aBarWindow(t)

	_, row := barRow(t, a)

	if a.bar.Status != "" {
		t.Errorf("the bar says %q, want nothing", a.bar.Status)
	}
	if got := strings.TrimSpace(row[titlesEnd(a):]); got != "" {
		t.Errorf("the bar row reads %q past the titles, want it blank", got)
	}
}

// A window with the port open and nobody on it says so, in red, and the
// titles stay where they were.
func TestServingWithNobodyConnectedSaysSoOnTheMenuBar(t *testing.T) {
	a := aBarWindow(t)
	_, plain := barRow(t, a)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}

	area, row := barRow(t, a)

	want := "Serving on " + a.serving.addr() + ", nobody connected"
	if a.bar.Status != want {
		t.Errorf("the bar says %q, want %q", a.bar.Status, want)
	}
	if got, red := a.bar.StatusFG, statusIdleFG(a.colours); got != red {
		t.Errorf("the status is %+v, want the dimmer red %+v", got, red)
	}
	at := strings.Index(row, want)
	if at < 0 {
		t.Fatalf("the bar row is %q, want %q on it", row, want)
	}
	col := area.X + len([]rune(row[:at]))
	if got, red := a.g.At(col, area.Y).FG, statusIdleFG(a.colours); got != red {
		t.Errorf("the status is drawn in %+v, want the dimmer red %+v", got, red)
	}
	if got, was := row[:titlesEnd(a)], plain[:titlesEnd(a)]; got != was {
		t.Errorf("the titles read %q with a status, want %q", got, was)
	}
}

// A window somebody is working in names them, in red, and goes back to
// saying it is only serving when they let go.
func TestAWindowTakenOverSaysWhoHasItOnTheMenuBar(t *testing.T) {
	host, client, addr := twoWindows(t)
	withMenubar(t, host)

	clients := host.serving.clients()
	if len(clients) != 1 {
		t.Fatalf("%d windows are connected, want one", len(clients))
	}
	_, row := barRow(t, host)

	want := "Controlled by " + clients[0].Name + " from " + clients[0].Addr
	if host.bar.Status != want {
		t.Errorf("the bar says %q, want %q", host.bar.Status, want)
	}
	if got, red := host.bar.StatusFG, statusTakenFG(host.colours); got != red {
		t.Errorf("the status is %+v, want red %+v", got, red)
	}
	// The end of it, because a bar this wide cuts the front off.
	if tail := want[len(want)-20:]; !strings.Contains(row, tail) {
		t.Errorf("the bar row is %q, want it to end with %q", row, tail)
	}

	// The client lets go, and the window is back to serving nobody.
	if err := client.dropWindow(addr); err != nil {
		t.Fatalf("let go: %v", err)
	}
	waitFor(t, host, "the serving window to see it go", func() bool {
		return len(host.serving.clients()) == 0
	}, client)
	if _, row = barRow(t, host); !strings.Contains(row, "nobody connected") {
		t.Errorf("the bar row is %q, want it to say nobody is connected", row)
	}

	// And nothing at all once the port closes.
	if err := host.stopServing(); err != nil {
		t.Fatalf("stop serving: %v", err)
	}
	_, row = barRow(t, host)
	if host.bar.Status != "" {
		t.Errorf("the bar says %q with nothing served, want nothing", host.bar.Status)
	}
	if got := strings.TrimSpace(row[titlesEnd(host):]); got != "" {
		t.Errorf("the bar row reads %q past the titles, want it blank", got)
	}
}

// Pressing the status opens the serving dialog, whose Kick button hangs
// up on the window connected and leaves the port open.
func TestPressingTheStatusKicksTheOtherWindowOut(t *testing.T) {
	host, client, addr := twoWindows(t)
	withMenubar(t, host)
	col, row := statusColumn(t, host)

	took, err := host.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row,
	})

	if err != nil {
		t.Fatalf("pressing the status: %v", err)
	}
	if !took {
		t.Fatal("the press on the status travelled on")
	}
	f := awaitModal(t, host, "the serving dialog", byTitle[*ui.Form]("Serving this window"))
	kick := "Kick " + host.serving.clients()[0].Name + " out"
	pressButton(t, host, f, kick)

	waitFor(t, client, "the window over there to see the connection go", func() bool {
		return client.windows.at(addr) == nil
	}, host)
	if !host.serving.on() {
		t.Error("kicking the window out stopped the port as well")
	}
	if _, bar := barRow(t, host); !strings.Contains(bar, "nobody connected") {
		t.Errorf("the bar row is %q, want it to say nobody is connected", bar)
	}
}

// With nobody connected there is nothing to kick, and the same dialog
// still stops serving.
func TestTheServingDialogOffersNoKickWithNobodyConnected(t *testing.T) {
	a := aServedWindow(t)
	col, row := statusColumn(t, a)

	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row,
	}); err != nil {
		t.Fatalf("pressing the status: %v", err)
	}

	f := awaitModal(t, a, "the serving dialog", byTitle[*ui.Form]("Serving this window"))
	for _, b := range f.Buttons() {
		if strings.HasPrefix(b.Title, "Kick") {
			t.Errorf("the dialog offers %q with nobody connected", b.Title)
		}
	}
	pressButton(t, a, f, "Stop serving")
	if a.serving.on() {
		t.Error("the port is still open")
	}
	if _, bar := barRow(t, a); strings.Contains(bar, "Serving on") {
		t.Errorf("the bar row is %q, want nothing about serving", bar)
	}
}

// Two windows connected are counted on the bar and kicked out together,
// and the button says so rather than naming one of them.
func TestKickingEveryoneOutClosesEveryConnection(t *testing.T) {
	host := aBarWindow(t)
	mine, line := aKeyPair(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	reachHost(t, host, mine)
	reachHost(t, host, mine)
	waitFor(t, host, "both windows to be connected", func() bool {
		return len(host.serving.clients()) == 2
	})

	_, row := barRow(t, host)

	if !strings.Contains(row, "and 1 more") {
		t.Errorf("the bar row is %q, want it to count the second window", row)
	}
	col, at := statusColumn(t, host)
	if _, err := host.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: at,
	}); err != nil {
		t.Fatalf("pressing the status: %v", err)
	}
	f := awaitModal(t, host, "the serving dialog", byTitle[*ui.Form]("Serving this window"))
	pressButton(t, host, f, "Kick everyone out")

	waitFor(t, host, "both windows to go", func() bool {
		return len(host.serving.clients()) == 0
	})
	if !host.serving.on() {
		t.Error("kicking them out stopped the port as well")
	}
}

// reachHost connects to a serving window with the key it allows, for a
// test that needs another window on the port and nothing drawn from it.
func reachHost(t *testing.T, host *testApp, key ssh.Signer) *serve.Window {
	t.Helper()
	w, err := serve.Dial(context.Background(), serve.DialConfig{
		Addr: host.serving.addr(), Keys: []ssh.Signer{key},
		HostKey: ssh.FixedHostKey(host.serving.server.HostKey()),
	})
	if err != nil {
		t.Fatalf("reach the serving window: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}
