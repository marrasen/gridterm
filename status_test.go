package main

import (
	"context"
	"errors"
	"image/color"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/grid"
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

// chipColumn is a column of the last chip on the bar, for a press that
// lands on it.
func chipColumn(t *testing.T, a *testApp) (int, int) {
	t.Helper()
	area, row := barRow(t, a)
	// By column, not by byte: a chip can say something that takes more
	// than one byte to a column.
	at := -1
	columns := []rune(row)
	for x := len(columns) - 1; x >= 0; x-- {
		if columns[x] != ' ' {
			at = x
			break
		}
	}
	if at < titlesEnd(a) {
		t.Fatalf("the bar row is %q, want a chip past the titles", row)
	}
	return area.X + at, area.Y
}

// chipsSay is what the chips on the bar say, in the order they are drawn
// in.
func chipsSay(a *testApp) []string {
	out := make([]string, 0, len(a.bar.Status))
	for _, chip := range a.bar.Status {
		out = append(out, chip.Text)
	}
	return out
}

// Both status colours have to be read against the bar's own ground,
// which shades from one end of the bar to the other. WCAG asks 4.5:1 for
// text, so the status is measured against both ends of that shading.
//
// And against each other: two colours that say different things have to
// be told apart, which grid.Contrast puts at 1.5:1.
func TestTheStatusColoursAreReadableOnTheBar(t *testing.T) {
	const (
		wantText   = 4.5
		wantChange = 1.5
	)
	a := aBarWindow(t)
	p := a.colours
	// The bar's own ground, asked of the bar: a test measuring against
	// sidebarTop and sidebarFoot would go on passing if the bar were
	// given a ground of its own.
	grounds := []struct {
		where string
		bg    color.RGBA
	}{
		{"the near end of the bar's ground", a.bar.Style.BG},
		{"the far end of it", a.bar.Style.BGEnd},
	}
	colours := []struct {
		what string
		fg   color.RGBA
	}{
		{"the controlled status", statusTakenFG(p)},
		{"the idle status", statusIdleFG(p)},
		{"the agent status", statusAgentFG(p)},
	}

	// Every chip is read against its own ground as well, which is the
	// one thing on the bar that is not the bar's.
	grounds = append(grounds, struct {
		where string
		bg    color.RGBA
	}{"a chip's own ground", chipBG(p)})

	for _, c := range colours {
		for _, g := range grounds {
			if got := grid.Contrast(c.fg, g.bg); got < wantText {
				t.Errorf("%s is %.2f:1 against %s, want at least %.1f", c.what, got, g.where, wantText)
			}
		}
	}
	// The two are far enough apart to read as a change of colour.
	if got := grid.Contrast(statusTakenFG(p), statusIdleFG(p)); got < wantChange {
		t.Errorf("the two statuses are %.2f:1 apart, want at least %.1f: "+
			"below that they read as the same colour twice", got, wantChange)
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

	if got := chipsSay(a); len(got) != 0 {
		t.Errorf("the bar says %q, want nothing", got)
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

	const want = "Serving"
	if got := chipsSay(a); len(got) != 1 || got[0] != want {
		t.Errorf("the bar says %q, want just %q: the address is the dialog's", got, want)
	}
	if got, red := a.bar.Status[0].FG, statusIdleFG(a.colours); got != red {
		t.Errorf("the chip is %+v, want the dimmer red %+v", got, red)
	}
	if got, ground := a.bar.Status[0].BG, chipBG(a.colours); got != ground {
		t.Errorf("the chip sits on %+v, want a ground of its own %+v", got, ground)
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

	const want = "Remote controlled"
	if got := chipsSay(host); len(got) != 1 || got[0] != want {
		t.Errorf("the bar says %q, want just %q", got, want)
	}
	if got, red := host.bar.Status[0].FG, statusTakenFG(host.colours); got != red {
		t.Errorf("the chip is %+v, want red %+v", got, red)
	}
	if !strings.Contains(row, want) {
		t.Errorf("the bar row is %q, want %q on it", row, want)
	}
	// Who has the window, and from where, is the dialog's to say.
	if strings.Contains(row, clients[0].Name) || strings.Contains(row, clients[0].Addr) {
		t.Errorf("the bar row is %q, want the name and the address kept off it", row)
	}

	// The client lets go, and the window is back to serving nobody.
	if err := client.dropWindow(addr); err != nil {
		t.Fatalf("let go: %v", err)
	}
	waitFor(t, host, "the serving window to see it go", func() bool {
		return len(host.serving.clients()) == 0
	}, client)
	if _, row = barRow(t, host); !strings.Contains(row, "Serving") {
		t.Errorf("the bar row is %q, want it back to saying the window is only served", row)
	}
	if got := chipsSay(host); len(got) != 1 || got[0] != "Serving" {
		t.Errorf("the bar says %q, want it back to just serving", got)
	}

	// And nothing at all once the port closes.
	if err := host.stopServing(); err != nil {
		t.Fatalf("stop serving: %v", err)
	}
	_, row = barRow(t, host)
	if got := chipsSay(host); len(got) != 0 {
		t.Errorf("the bar says %q with nothing served, want nothing", got)
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
	col, row := chipColumn(t, host)

	took, err := host.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row,
	})

	if err != nil {
		t.Fatalf("pressing the chip: %v", err)
	}
	if !took {
		t.Fatal("the press on the chip travelled on")
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
	if _, bar := barRow(t, host); !strings.Contains(bar, "Serving") {
		t.Errorf("the bar row is %q, want it back to saying the window is only served", bar)
	}
	if got := chipsSay(host); len(got) != 1 || got[0] != "Serving" {
		t.Errorf("the bar says %q, want it back to just serving", got)
	}
}

// A window that let go between the dialog opening and the button being
// pressed is not a failed kick. It has gone, which is what the button
// was for, so nothing is said about it.
func TestKickingAWindowThatHasAlreadyGoneSaysNothing(t *testing.T) {
	host, client, addr := twoWindows(t)
	withMenubar(t, host)
	col, row := chipColumn(t, host)

	if _, err := host.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row,
	}); err != nil {
		t.Fatalf("pressing the chip: %v", err)
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

	// Pressed, not run: a deliberate let-go arrives as an end of file and
	// is filtered, so nothing else is on the screen for the press to land
	// on, and pressing is what a user does.
	pressButton(t, host, f, kick.Title)

	if n, up := host.root.Modal().(*ui.Notice); up {
		t.Errorf("kicking a window that had already gone reported %q: %s", n.Title, n.Message())
	}
	if host.root.Modal() != nil {
		t.Error("the dialog is still up after the kick")
	}
	if !host.serving.on() {
		t.Error("it stopped the port as well")
	}
}

// A window that let go and connected again under the same name is not
// kicked by a press that named the old one, and the dialog says so.
//
// The press closes a socket that has already gone, which says nothing.
// Read as a kick that worked, it left the user thinking they had thrown
// somebody out of their window while that person was still working in it.
func TestKickingAWindowThatConnectedAgainSaysSo(t *testing.T) {
	host := aBarWindow(t)
	mine, line := aKeyPair(t)
	withServing(t, host, line)
	if err := host.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	first := reachHost(t, host, mine)
	waitFor(t, host, "the first window to connect", func() bool {
		return len(host.serving.clients()) == 1
	})
	// What the dialog would have named when it opened.
	named := host.serving.clients()

	// It lets go, and the same person connects again under the same name.
	if err := first.Close(); err != nil {
		t.Fatalf("let go: %v", err)
	}
	waitFor(t, host, "the first window to go", func() bool {
		return len(host.serving.clients()) == 0
	})
	reachHost(t, host, mine)
	waitFor(t, host, "the second window to connect", func() bool {
		return len(host.serving.clients()) == 1
	})

	err := host.kickOut(named)

	if err == nil || !strings.Contains(err.Error(), "connected again since") {
		t.Fatalf("it said %v, want it to say the window connected again", err)
	}
	if got := len(host.serving.clients()); got != 1 {
		t.Errorf("%d windows are connected, want the one that connected again left alone", got)
	}
}

// Kicking a window out says nothing: the user asked for it.
//
// The kick closes that client's socket from here, so this window's own
// read of it fails with "use of closed network connection". Reported,
// every kick put up a notice saying the window had been lost.
func TestKickingAWindowOutIsNotReportedAsALoss(t *testing.T) {
	host, client, _ := twoWindows(t)
	withMenubar(t, host)
	col, row := chipColumn(t, host)

	if _, err := host.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row,
	}); err != nil {
		t.Fatalf("pressing the chip: %v", err)
	}
	f := awaitModal(t, host, "the serving dialog", byTitle[*ui.Form]("Serving this window"))
	pressButton(t, host, f, "Kick "+host.serving.clients()[0].Name+" out")

	waitFor(t, host, "the window serving to let the client go", func() bool {
		return servingRows(host) == 0
	}, client)
	if n, up := host.root.Modal().(*ui.Notice); up {
		t.Errorf("the kick reported %q: %s", n.Title, n.Message())
	}
}

// And a client lost to a fault still is reported.
func TestAClientLostToAFaultIsStillReported(t *testing.T) {
	host, _, _ := twoWindows(t)
	c := host.serving.clients()[0]

	host.clientWent(c, errors.New("the transport broke"))

	n := awaitModal[*ui.Notice](t, host, "the loss", nil)
	if !strings.Contains(n.Message(), "the transport broke") {
		t.Errorf("it said %q, want why the window was lost", n.Message())
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
	col, row := chipColumn(t, a)

	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row,
	}); err != nil {
		t.Fatalf("pressing the chip: %v", err)
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
	if _, bar := barRow(t, a); strings.Contains(bar, "Serving") {
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

	if got := chipsSay(host); len(got) != 1 || got[0] != "Remote controlled" {
		t.Errorf("the bar says %q, want the one chip however many are connected", got)
	}
	if strings.Contains(row, "more") {
		t.Errorf("the bar row is %q, want the count kept off it", row)
	}
	col, at := chipColumn(t, host)
	if _, err := host.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: at,
	}); err != nil {
		t.Fatalf("pressing the chip: %v", err)
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
