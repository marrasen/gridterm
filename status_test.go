package main

import (
	"context"
	"image/color"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vt"
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

// Both status colours have to be read against the bar's own ground,
// which shades from one end of the bar to the other. WCAG asks 4.5:1 for
// text, so the status is measured against both ends of that shading.
func TestTheStatusColoursAreReadableOnTheBar(t *testing.T) {
	const wantText = 4.5
	p := vt.DefaultPalette()
	grounds := []struct {
		where string
		bg    color.RGBA
	}{
		{"the near end of the bar's ground", sidebarTop(p)},
		{"the far end of it", sidebarFoot(p)},
	}
	colours := []struct {
		what string
		fg   color.RGBA
	}{
		{"the controlled status", statusTakenFG(p)},
		{"the idle status", statusIdleFG(p)},
	}

	for _, c := range colours {
		for _, g := range grounds {
			if got := grid.Contrast(c.fg, g.bg); got < wantText {
				t.Errorf("%s is %.2f:1 against %s, want at least %.1f", c.what, got, g.where, wantText)
			}
		}
	}
	// And a window somebody is working in says so more strongly than one
	// that is only listening.
	for _, g := range grounds {
		taken, idle := grid.Contrast(statusTakenFG(p), g.bg), grid.Contrast(statusIdleFG(p), g.bg)
		if taken < idle {
			t.Errorf("against %s the controlled status is %.2f:1 and the idle one %.2f:1, "+
				"want the controlled one at least as strong", g.where, taken, idle)
		}
	}
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
	// The head of it, because a bar this wide cuts the end off and the
	// head is the part that names who has the window.
	if head := want[:20]; !strings.Contains(row, head) {
		t.Errorf("the bar row is %q, want it to start with %q", row, head)
	}

	// The client lets go, and the window is back to serving nobody.
	if err := client.dropWindow(addr); err != nil {
		t.Fatalf("let go: %v", err)
	}
	waitFor(t, host, "the serving window to see it go", func() bool {
		return len(host.serving.clients()) == 0
	}, client)
	// The head of the status again: "Serving on" is what the idle one
	// starts with, and the only one of the two that does.
	if _, row = barRow(t, host); !strings.Contains(row, "Serving on") {
		t.Errorf("the bar row is %q, want it back to saying the window is only served", row)
	}
	if !strings.HasSuffix(host.bar.Status, "nobody connected") {
		t.Errorf("the bar says %q, want it to say nobody is connected", host.bar.Status)
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
	// And this window to see it too, which is what the bar is read off:
	// the client sees the connection go first, and the host drops it
	// when its own end finishes.
	waitFor(t, host, "the serving window to see the connection go", func() bool {
		return len(host.serving.clients()) == 0
	}, client)
	if !host.serving.on() {
		t.Error("kicking the window out stopped the port as well")
	}
	if _, bar := barRow(t, host); !strings.Contains(bar, "Serving on") {
		t.Errorf("the bar row is %q, want it back to saying the window is only served", bar)
	}
	if !strings.HasSuffix(host.bar.Status, "nobody connected") {
		t.Errorf("the bar says %q, want it to say nobody is connected", host.bar.Status)
	}
}

// A window that let go between the dialog opening and the button being
// pressed is not a failed kick. It has gone, which is what the button
// was for, so nothing is said about it.
func TestKickingAWindowThatHasAlreadyGoneSaysNothing(t *testing.T) {
	host, client, addr := twoWindows(t)
	withMenubar(t, host)
	col, row := statusColumn(t, host)

	if _, err := host.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row,
	}); err != nil {
		t.Fatalf("pressing the status: %v", err)
	}
	f := awaitModal(t, host, "the serving dialog", byTitle[*ui.Form]("Serving this window"))
	kick := buttonNamed(t, f, "Kick "+host.serving.clients()[0].Name+" out")

	// The window connected lets go while the dialog is up, so the button
	// names a window that is no longer there.
	if err := client.dropWindow(addr); err != nil {
		t.Fatalf("let go: %v", err)
	}
	waitFor(t, host, "the serving window to see it go", func() bool {
		return len(host.serving.clients()) == 0
	}, client)

	// The button is run rather than pressed: a window going can put a
	// notice over the dialog, and a press that landed on the notice would
	// prove nothing either way. What the button carries is the whole of
	// what the press would run.
	err := kick.Do()

	if err != nil {
		t.Errorf("kicking a window that had already gone reported %v", err)
	}
	if !host.serving.on() {
		t.Error("it stopped the port as well")
	}
}

// buttonNamed is one of a dialog's buttons, by what it says.
func buttonNamed(t *testing.T, f *ui.Form, title string) ui.Button {
	t.Helper()
	for _, b := range f.Buttons() {
		if b.Title == title {
			return b
		}
	}
	t.Fatalf("the dialog has no %q button", title)
	return ui.Button{}
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
