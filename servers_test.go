package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
)

// serverConfig points at the in-process SSH server, with host key
// checking pinned and nothing that would reach the developer's own keys.
func serverConfig(t *testing.T, s *sshtest.Server) remote.Config {
	t.Helper()
	host, port := s.Host()
	return remote.Config{
		Host: host, Port: port, User: "tester",
		HostKeyCallback: ssh.FixedHostKey(s.HostKey()),
		NoAgent:         true,
		Identities:      []string{sshtest.WriteKey(t)},
	}
}

// waitForPanes runs the pump until the app has n panes and nothing is
// still connecting.
//
// A connection opens its pane as soon as it starts, so the pane is there
// before the machine is, and a test that counted panes alone would go on
// before there was anything to connect to.
func waitForPanes(t *testing.T, a *testApp, n int) {
	t.Helper()
	waitFor(t, a, fmt.Sprintf("%d panes of its own and nothing still connecting", n), func() bool {
		return len(ownPanes(a)) == n && a.machines.beingMade() == 0
	})
}

// A target that is not a target has to be said so where it was typed,
// with what was typed still there to correct.
func TestOpenServerRejectsABadTarget(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.openServer(); err != nil {
		t.Fatalf("openServer: %v", err)
	}
	f := awaitModal(t, a, "the Connect to a server dialog", byTitle[*ui.Form]("Connect to a server"))
	typeIntoField(t, a, f, "Server", "host:nope")
	pressButton(t, a, f, "Connect")

	if a.root.Modal() != f {
		t.Fatal("the dialog closed on a target it could not parse")
	}
	if f.Error() == nil {
		t.Fatal("nothing said why the target was refused")
	}
	if got := f.Field("Server").Text(); got != "host:nope" {
		t.Errorf("the dialog lost what was typed: %q", got)
	}
}

// A connection that fails says why, rather than leaving the user looking
// at a dialog that went away for no stated reason.
func TestConnectShowsWhyItFailed(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	cfg := serverConfig(t, s)
	// A key the server would accept, pointed at a port nothing answers.
	cfg.Port = 1
	a.connect(cfg)

	said := waitForFailure(t, a, cfg.Target())
	if strings.TrimSpace(said) == "" {
		t.Fatal("the failure was reported with no reason in it")
	}
	// The pane that was watching stays, holding the reason: a failure
	// the user can still read is the whole point of watching in a pane.
	// Closing it is what takes it away.
	if len(a.panes) != 2 {
		t.Errorf("%d panes after a failed connection, want the one that was there"+
			" and the one that says why", len(a.panes))
	}
	if a.machines.connecting(cfg.Target()) != nil {
		t.Error("the machine is still marked as being connected to")
	}
	for pane, e := range a.panes {
		if e.Host != cfg.Target() {
			continue
		}
		if err := a.closePane(pane); err != nil {
			t.Fatalf("close the pane: %v", err)
		}
	}
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Host == cfg.Target() {
				t.Fatalf("closing it left %q on the panel", row.Label)
			}
		}
	}
}

// Cancelling gives up on a connection that is genuinely still being
// made, and says nothing afterwards: the user knows, they cancelled.
//
// The server here accepts and then never speaks, so the dial really is
// still running when Cancel is pressed. A port nothing listens on is
// refused in well under a millisecond, which would let this pass without
// the cancellation doing anything at all.
func TestConnectCancelStopsADialThatIsStillRunning(t *testing.T) {
	host, port := sshtest.Deaf(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	cfg := serverConfig(t, sshtest.New(t))
	cfg.Host, cfg.Port = host, port
	a.connect(cfg)

	// A row on the panel says it is on its way, and it can be cancelled
	// from there.
	waiting := waitForConnecting(t, a)
	if a.machines.beingMade() != 1 {
		t.Fatalf("%d connections are being made, want 1", a.machines.beingMade())
	}
	if waiting.Close == nil {
		t.Fatal("the row cannot be cancelled")
	}
	if err := waiting.Close(); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// The dial has to end, and end quietly.
	waitFor(t, a, "the cancelled dial to stop being made", func() bool {
		return a.machines.beingMade() == 0
	})
	if m := a.root.Modal(); m != nil {
		t.Fatalf("a dialog was left open after cancelling: %T", m)
	}
	if len(a.panes) != 1 {
		t.Fatalf("%d panes after cancelling, want the one that was there", len(a.panes))
	}
	// And the row it was waiting in has gone.
	for _, e := range a.registry.Groups(time.Now()) {
		for _, row := range e.Rows {
			if row.Entry == waiting {
				t.Fatal("the row was left on the panel")
			}
		}
	}
}

// Cancelling is the user's own decision, so it is not reported back to
// them as a failure.
func TestReportErrorSaysNothingAboutACancellation(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	a.reportError("Could not connect", context.Canceled)
	if m := a.root.Modal(); m != nil {
		t.Fatalf("cancelling opened a dialog: %T", m)
	}
	a.reportError("Could not connect", errors.New("no route to host"))
	if a.root.Modal() == nil {
		t.Fatal("a real failure opened no dialog")
	}
}

// Several connections can be on their way at once, each with its own row
// on the panel. A dialog each would stack, and closing one takes
// everything above it.
func TestConnectRunsSeveralAtOnce(t *testing.T) {
	host, port := sshtest.Deaf(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	for _, name := range []string{"one", "two", "three"} {
		cfg := serverConfig(t, sshtest.New(t))
		cfg.Host, cfg.Port = host, port
		a.connectAs(name, cfg)
	}
	if a.machines.beingMade() != 3 {
		t.Fatalf("%d connections are being made, want 3", a.machines.beingMade())
	}

	// One row each, under a heading each, and no dialog anywhere.
	rows := panelText(a, time.Now())
	for _, name := range []string{"one", "two", "three"} {
		var found bool
		for _, row := range rows {
			if row == name {
				found = true
			}
		}
		if !found {
			t.Errorf("there is no row for %q: %v", name, rows)
		}
	}
	if m := a.root.Modal(); m != nil {
		t.Fatalf("a dialog opened for a connection: %T", m)
	}

	// Each has a pane of its own, saying what it is doing.
	var opening []*conns.Entry
	for _, group := range a.registry.Groups(time.Now()) {
		for _, row := range group.Rows {
			if row.Label == "connecting" {
				opening = append(opening, row.Entry)
			}
		}
	}
	if len(opening) != 3 {
		t.Fatalf("%d panes say a connection is opening, want 3", len(opening))
	}

	// Cancelling one leaves the others alone. Settled first, so a
	// cancel-all is not missed while the count is passing through 2.
	if err := opening[0].Close(); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	waitFor(t, a, "the cancelled connection to stop being made", func() bool {
		return a.machines.beingMade() <= 2
	})
	// Long enough that a cancel-all would have landed too.
	for i := 0; i < 20; i++ {
		a.pump.run()
		time.Sleep(5 * time.Millisecond)
	}
	if a.machines.beingMade() != 2 {
		t.Fatalf("%d connections are being made after cancelling one, want 2", a.machines.beingMade())
	}
	for _, e := range opening[1:] {
		var found bool
		for _, group := range a.registry.Groups(time.Now()) {
			for _, row := range group.Rows {
				if row.Entry == e {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("cancelling one connection took another off the panel")
		}
	}
}

// waitForConnecting runs the pump until a connection is on its way and
// returns the row standing for it.
func waitForConnecting(t *testing.T, a *testApp) *conns.Entry {
	t.Helper()
	var found *conns.Entry
	waitFor(t, a, "a row that says something is being connected to", func() bool {
		for _, group := range a.registry.Groups(time.Now()) {
			for _, row := range group.Rows {
				if row.Label == "connecting" {
					found = row.Entry
					return true
				}
			}
		}
		return false
	})
	return found
}

// Locking forgets every key, so the next connection asks again.
func TestLockKeysEmptiesTheRing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	path := sshtest.WriteEncryptedKey(t, "let me in")
	if _, err := a.keys.Unlock(a.ctx, path, &fixedAsk{passphrase: "let me in"}); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if !a.keys.Has(path) {
		t.Fatal("the key was not kept")
	}
	if err := a.lockKeys(); err != nil {
		t.Fatalf("lockKeys: %v", err)
	}
	if a.keys.Has(path) {
		t.Fatal("a key survived locking the ring")
	}
}

func TestWrapLines(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  []string
	}{
		{"short", 20, []string{"short"}},
		{"one two three four", 8, []string{"one two", "three", "four"}},
		{"", 10, nil},
		// A word longer than the box is cut rather than pushing the
		// dialog wider than the window.
		{"aaaaaaaaaa", 4, []string{"aaaa", "aaaa", "aa"}},
		// Counted in cells, not bytes. A CJK character takes two cells
		// and three bytes, so a byte count wrapped this four times too
		// early and cut a character in half doing it.
		{"日本語のテ", 6, []string{"日本語", "のテ"}},
		// A character wider than the whole box goes on a line of its
		// own. Cutting it to fit is cutting it to nothing, which left
		// the wrap going round for ever.
		{"日本", 1, []string{"日", "本"}},
		// A combining mark shares its letter's cell and stays with it.
		{"éééé", 2, []string{"éé", "éé"}},
	}
	for _, tc := range cases {
		got := wrapLines(tc.in, tc.width)
		if len(got) != len(tc.want) {
			t.Fatalf("wrapLines(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("wrapLines(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
			}
		}
		for _, line := range got {
			// A line may be wider than the box only when one character
			// is: nothing can make that one narrower.
			if w := grid.StringWidth(line); w > tc.width && len(grid.Clusters(line)) > 1 {
				t.Fatalf("wrapLines(%q, %d) produced a line %d cells wide", tc.in, tc.width, w)
			}
		}
	}
}

// input1 is one typed character.
func input1(r rune) input.Event {
	return input.Event{Kind: input.Text, Rune: r, NormalText: true}
}

// fixedAsk answers with what it was built with, for a test that only
// needs the ring to open a key.
type fixedAsk struct{ passphrase string }

func (f *fixedAsk) Passphrase(context.Context, string) (string, error) {
	return f.passphrase, nil
}
func (f *fixedAsk) Password(context.Context, string, string) (string, error) { return "", nil }
func (f *fixedAsk) Question(context.Context, remote.Question) ([]string, error) {
	return nil, nil
}
func (f *fixedAsk) TrustHostKey(context.Context, remote.HostKey) (bool, error) {
	return false, nil
}
func (f *fixedAsk) Notice(context.Context, remote.Notice) {}

// The path a user actually takes: the command, the form, the keys.
//
// This is what the direct call to connect misses. The Connect button
// used to open the waiting dialog from inside the form's own Do, and the
// form then closed -- taking the waiting dialog with it and cancelling
// the connection it had just started. Nothing was reported.
func TestOpenServerConnectsThroughTheForm(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	host, port := s.Host()

	// The real connection has to go through serverConfig's pinned host
	// key, so the form only supplies the target and the rest is set on
	// the way past.
	a.prepare = func(cfg remote.Config) remote.Config {
		base := serverConfig(t, s)
		base.User = cfg.User
		return base
	}

	if err := a.openServer(); err != nil {
		t.Fatalf("openServer: %v", err)
	}
	f := awaitModal(t, a, "the Connect to a server dialog", byTitle[*ui.Form]("Connect to a server"))
	typeIntoField(t, a, f, "Server", fmt.Sprintf("tester@%s:%d", host, port))
	pressButton(t, a, f, "Connect")

	// A row holds the place while the connection is made.
	waitForConnecting(t, a)
	waitForPanes(t, a, 2)
	if a.root.Modal() != nil {
		t.Errorf("a dialog was left open: %T", a.root.Modal())
	}
	checkTree(t, a)
	if n := s.Conns(); n != 1 {
		t.Fatalf("the server saw %d connections, want the one", n)
	}
}

// waitForFailure waits for the pane watching a connection to say it
// could not be made, and gives back what it says.
//
// A failure is written into the pane that was watching for it rather
// than shown in a dialog: the pane stays, so the reason can still be
// read afterwards.
func waitForFailure(t *testing.T, a *testApp, host string) string {
	t.Helper()
	var said string
	waitFor(t, a, "a pane saying the connection to "+host+" failed", func() bool {
		for pane, e := range a.panes {
			if e.Host != host {
				continue
			}
			if got := paneText(pane); strings.Contains(got, "The connection was not made") {
				said = got
				return true
			}
		}
		return false
	})
	return said
}

// Connecting to a server opens a pane and says what it is doing in it,
// and the same pane then carries the shell.
//
// This is what makes a connection that goes wrong reportable: without
// it there is a row that says "opening" and nothing else, whatever
// happens.
func TestConnectingOpensAPaneAndSaysWhatItIsDoing(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	before := len(a.panes)

	cfg := serverConfig(t, s)
	a.connect(cfg)

	pane := newestPane(t, a)
	watching := watchPane(t, pane)
	if len(a.panes) != before+1 {
		t.Fatalf("%d panes, want one more than the %d there were", len(a.panes), before)
	}
	// It says what it is doing before there is anything to connect to.
	// Read from what the pane was written rather than from the screen:
	// the fold empties the screen as soon as the shell is there.
	waitFor(t, a, "the pane to say what it is doing", func() bool {
		return strings.Contains(watching.text(), "connecting to "+cfg.Target())
	})

	// And the same pane carries the shell, with one line in place of the
	// account of how it was reached.
	waitFor(t, a, "the connection to be made", func() bool {
		return a.machines.named(cfg.Target()) != nil
	})
	waitFor(t, a, "the pane to say it connected", func() bool {
		return strings.Contains(paneText(pane), "Connected to "+cfg.Target())
	})
	if a.machines.runningOn(pane) == nil {
		t.Error("the pane is not on the machine it connected to")
	}
	if len(a.panes) != before+1 {
		t.Errorf("%d panes, so a second one opened for the shell", len(a.panes))
	}
}

// What a server says on the way in is kept whole, in the account of how
// the machine was reached.
//
// A server that signs people in through a browser sends the link this
// way. In a dialog it is cut off at the edge, cannot be selected, and is
// gone the moment the dialog is dismissed. It goes into the pane while
// the connection is being made, and the pane folds that away once the
// shell is there, so the account is where it stays: whole, scrollable
// and there to copy.
func TestWhatAServerSaysIsKeptWhole(t *testing.T) {
	const link = "https://login.tailscale.com/a/0123456789abcdef0123456789abcdef"
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	s.SayOnTheWayIn("To authenticate, visit:\r\n\r\n" + link + "\r\n")

	cfg := serverConfig(t, s)
	a.connect(cfg)
	watching := watchPane(t, newestPane(t, a))

	waitFor(t, a, "the pane to be folded for the shell", func() bool {
		return strings.Contains(watching.text(), clearPane)
	})

	// It was in the pane while the connection was being made, before the
	// fold cleared the pane for the shell.
	saw := watching.text()
	shown, folded := strings.Index(saw, link), strings.Index(saw, clearPane)
	switch {
	case shown < 0:
		t.Errorf("the link never reached the pane: %q", saw)
	case folded < 0:
		t.Error("the pane was never folded, so the order proves nothing")
	case shown > folded:
		t.Errorf("the link was written after the fold cleared the pane: %q", saw)
	}

	// And it is still there to read afterwards.
	account := strings.Join(a.about(cfg.Target()).log().Lines(), "\n")
	// On one line, with nothing cut off it: a link broken across two
	// lines is a link nobody can copy.
	if !strings.Contains(account, link) {
		t.Errorf("the account does not hold the link whole: %q", account)
	}
	if !strings.Contains(account, "says:") {
		t.Errorf("the account does not say who said it: %q", account)
	}
}

// A pane that says why a connection failed is not reaped away.
//
// The pane is the only account of what happened, and it has to still be
// there when the user goes looking.
func TestThePaneThatSaysWhyIsNotReapedAway(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	before := len(a.panes)

	cfg := serverConfig(t, s)
	cfg.Port = 1
	a.connect(cfg)

	said := waitForFailure(t, a, cfg.Target())
	if !strings.Contains(said, "The connection was not made") {
		t.Fatalf("the pane says %q", said)
	}

	// The window tidies up after shells that have gone, every frame.
	for i := 0; i < 10; i++ {
		a.pump.run()
		a.reapExited()
	}

	if len(a.panes) != before+1 {
		t.Fatalf("%d panes after tidying up, want the pane that says why to still be there",
			len(a.panes))
	}
	for pane, e := range a.panes {
		if e.Host != cfg.Target() {
			continue
		}
		if got := paneText(pane); !strings.Contains(got, "The connection was not made") {
			t.Errorf("the pane no longer says why: %q", got)
		}
		return
	}
	t.Error("there is no pane for the machine that could not be reached")
}

// A failure the user cannot retype has to be readable whole and copyable
// with the window's own copy chord.
func TestReportErrorShowsTheWholeMessageAndCopiesIt(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	long := "could not open " + strings.Repeat("a/long/path/", 30) + "file: permission denied"

	a.reportError("Could not open it", errors.New(long))

	n := awaitModal[*ui.Notice](t, a, "a notice", nil)
	if n.Message() != long {
		t.Errorf("the dialog holds %d characters, want the whole %d", len(n.Message()), len(long))
	}
	if !n.Failure {
		t.Error("the dialog is not marked as a failure, so its title is not red")
	}

	if _, err := a.root.HandleKey(press(input.KeyC, input.ModCtrl|input.ModShift)); err != nil {
		t.Fatalf("the copy chord: %v", err)
	}

	waitFor(t, a, "the message to reach the clipboard", func() bool { return a.copiedText() == long })
}

// noticeRow reads one row of a notice back off a grid of its own, the
// way the window draws a dialog onto its own layer.
func noticeRow(a *testApp, n *ui.Notice, y int) string {
	g := grid.New(a.lastSize[0], a.lastSize[1], color.RGBA{}, color.RGBA{})
	ui.DrawApart(n, g.View())
	var b strings.Builder
	for x := 0; x < a.lastSize[0]; x++ {
		if c := g.At(x, y); c.Width != 0 {
			b.WriteRune(c.Rune)
		}
	}
	return b.String()
}

// clickNoticeButton presses a button where it is drawn, through the
// window's own mouse routing. The column comes off the grid rather than
// from the layout, which is the code the hit test uses.
func clickNoticeButton(t *testing.T, a *testApp, n *ui.Notice, label string) {
	t.Helper()
	box := n.Box()
	row := box.Y + box.Rows - 2
	at := strings.Index(noticeRow(a, n, row), label)
	if at < 0 {
		t.Fatalf("the button %q is not drawn on row %d: %q", label, row, noticeRow(a, n, row))
	}
	if _, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft, Col: at, Row: row,
	}); err != nil {
		t.Fatalf("clicking %q: %v", label, err)
	}
}

// A notice has to be dismissable. Left up, it sits over the window for
// ever and nothing behind it can be reached.
func TestANoticeIsDismissedByEscapeAndByItsOKButton(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dismiss func(*testing.T, *testApp, *ui.Notice)
	}{
		{name: "Escape", dismiss: func(t *testing.T, a *testApp, _ *ui.Notice) {
			if _, err := a.root.HandleKey(press(input.KeyEscape, 0)); err != nil {
				t.Fatalf("Escape: %v", err)
			}
		}},
		{name: "the OK button", dismiss: func(t *testing.T, a *testApp, n *ui.Notice) {
			clickNoticeButton(t, a, n, "OK")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t, 80, 24)
			withDialogs(t, a)
			a.reportError("Could not open it", errors.New("the machine went away"))
			n := awaitModal[*ui.Notice](t, a, "a notice", nil)

			tc.dismiss(t, a, n)

			if got := a.root.Modal(); got != nil {
				t.Errorf("%T is still the top dialog, want the window back", got)
			}
			if len(a.modals) != 0 {
				t.Errorf("%d dialogs are still on the stack, want none", len(a.modals))
			}
		})
	}
}

// A notice covers the window, so the window's own shortcuts stop at it.
// Otherwise a key aimed at the dialog closes a pane behind it.
func TestANoticeTakesTheShortcutsWhileItIsUp(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withMenubar(t, a)
	panes := len(a.panes)
	a.reportError("Could not open it", errors.New("the machine went away"))
	n := awaitModal[*ui.Notice](t, a, "a notice", nil)

	for _, ev := range []input.Event{
		press(input.KeyV, input.ModCtrl|input.ModShift), // paste into the shell
		press(input.KeyW, input.ModCtrl|input.ModShift), // close the pane
		press(input.KeyF10, 0),                          // open the menu bar
		press(input.KeyK, input.ModCtrl|input.ModShift), // open the palette
	} {
		if _, err := a.root.HandleKey(ev); err != nil {
			t.Fatalf("%v: %v", ui.ChordOf(ev), err)
		}
	}

	if got := a.shells[0].sentText(); got != "" {
		t.Errorf("the shell behind the dialog received %q", got)
	}
	if len(a.panes) != panes {
		t.Errorf("%d panes are left of %d: one was closed behind the dialog", len(a.panes), panes)
	}
	if a.bar.OpenIndex() >= 0 {
		t.Error("the menu bar dropped a menu over the dialog")
	}
	if got := a.root.Modal(); got != ui.Widget(n) {
		t.Errorf("the top dialog is %T, want the notice still", got)
	}
	if len(a.modals) != 1 {
		t.Errorf("%d dialogs are stacked up, want the notice alone", len(a.modals))
	}
}

// The copy chord is the one shortcut a notice answers: an error nobody
// can retype is there to be copied.
func TestTheCopyChordsCopyFromTheNoticeOnTop(t *testing.T) {
	for _, chord := range []input.Event{
		press(input.KeyC, input.ModCtrl|input.ModShift),
		press(input.KeyInsert, input.ModCtrl),
	} {
		t.Run(ui.ChordOf(chord).String(), func(t *testing.T) {
			a := newTestApp(t, 80, 24)
			withDialogs(t, a)
			a.reportError("Could not open it", errors.New("the machine went away"))
			awaitModal[*ui.Notice](t, a, "a notice", nil)

			if _, err := a.root.HandleKey(chord); err != nil {
				t.Fatalf("the copy chord: %v", err)
			}

			waitFor(t, a, "the message to reach the clipboard", func() bool { return a.copiedText() == "the machine went away" })
		})
	}
}

// With no dialog open the same chord copies what is selected in the
// pane that has the keys.
func TestTheCopyChordCopiesTheTerminalSelectionWithNoDialogOpen(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	pane := a.focusedTerminal()
	if pane == nil {
		t.Fatal("no pane has the keys")
	}
	a.shells[0].out <- []byte("hello there")
	// Drawn each time round: the bytes are on their way from the shell,
	// and the pane only puts what it has read on its grid when it paints.
	waitFor(t, a, "the pane to draw what the shell said", func() bool {
		g := grid.New(a.lastSize[0], a.lastSize[1], color.RGBA{}, color.RGBA{})
		pane.Layout(ui.Size{Cols: a.lastSize[0], Rows: a.lastSize[1]})
		pane.Draw(g.View())
		return g.At(0, 0).Rune == 'h'
	})
	for _, ev := range []input.MouseEvent{
		{Kind: input.MousePress, Button: input.MouseLeft, Col: 0, Row: 0},
		{Kind: input.MouseMove, Button: input.MouseLeft, Col: 4, Row: 0},
		{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 4, Row: 0},
	} {
		if _, err := pane.HandleMouse(ev); err != nil {
			t.Fatalf("the drag: %v", err)
		}
	}
	if got := pane.SelectionText(); got != "hello" {
		t.Fatalf("the pane has %q selected, want the word the chord should copy", got)
	}

	if _, err := a.root.HandleKey(press(input.KeyC, input.ModCtrl|input.ModShift)); err != nil {
		t.Fatalf("the copy chord: %v", err)
	}

	waitFor(t, a, "the selection to reach the clipboard", func() bool { return a.copiedText() == "hello" })
}
