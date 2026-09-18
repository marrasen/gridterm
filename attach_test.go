package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// watchedPane makes a pane running a fake shell, and a session watching
// it, which is what a client on another machine holds.
func watchedPane(t *testing.T, cols, rows int) (*term.Terminal, *pipeSession, *watched) {
	t.Helper()
	shell := newPipeSession()
	pane, err := term.New(term.Config{
		Session: shell,
		Size:    ui.Size{Cols: cols, Rows: rows},
	})
	if err != nil {
		t.Fatalf("pane: %v", err)
	}
	// Laid out, because a pane that has never been drawn has no size,
	// and a screen has to have one.
	pane.Layout(ui.Size{Cols: cols, Rows: rows})
	t.Cleanup(func() { _ = pane.Close() })

	w, err := newWatched(pane)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return pane, shell, w
}

// readUntil reads a watched pane until it has said something, or gives
// up. It runs the read on a goroutine so a session that never answers
// fails the test rather than hanging it.
func readUntil(t *testing.T, w *watched, want string) string {
	t.Helper()
	var got strings.Builder
	for range 200 {
		b, err := readOnce(t, w)
		got.Write(b)
		if err != nil {
			t.Fatalf("read: %v, having got %q", err, got.String())
		}
		if strings.Contains(got.String(), want) {
			return got.String()
		}
	}
	t.Fatalf("waited for %q, got %q", want, got.String())
	return ""
}

// readOnce reads once, failing the test rather than blocking for ever.
func readOnce(t *testing.T, w *watched) ([]byte, error) {
	t.Helper()
	type got struct {
		b   []byte
		err error
	}
	back := make(chan got, 1)
	go func() {
		p := make([]byte, 4096)
		n, err := w.Read(p)
		back <- got{b: p[:n], err: err}
	}()
	select {
	case g := <-back:
		return g.b, g.err
	case <-time.After(waitBudget):
		t.Fatal("the read never came back")
		return nil, nil
	}
}

// A watcher is given the screen as it stands and then what the program
// says next.
func TestAWatcherIsGivenTheScreenAndThenTheRest(t *testing.T) {
	_, shell, w := watchedPane(t, 40, 8)

	// The screen comes without the program saying anything more.
	readUntil(t, w, "\x1b[H\x1b[2J")

	shell.out <- []byte("after-the-attach")
	readUntil(t, w, "after-the-attach")
}

// The live screen is sent, not the view. Somebody at the machine being
// watched may have scrolled back into history, and what the watcher
// wants is the screen the next output will land on.
func TestAWatcherIsSentTheLiveScreenNotTheScrolledView(t *testing.T) {
	shell := newPipeSession()
	pane, err := term.New(term.Config{Session: shell, Size: ui.Size{Cols: 20, Rows: 3}})
	if err != nil {
		t.Fatalf("pane: %v", err)
	}
	pane.Layout(ui.Size{Cols: 20, Rows: 3})
	defer func() { _ = pane.Close() }()

	// More than a screenful, so there is history to scroll into.
	shell.out <- []byte("one\r\ntwo\r\nthree\r\nfour\r\nfive\r\n")
	waitUntil(t, "the pane to show the last line", func() bool { return strings.Contains(paneText(pane), "five") })
	pane.ScrollView(4)
	waitUntil(t, "the pane to show what it was scrolled back to", func() bool { return strings.Contains(paneText(pane), "one") })

	w, err := newWatched(pane)
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	defer func() { _ = w.Close() }()

	got := readUntil(t, w, "five")
	if strings.Contains(got, "one") {
		t.Errorf("it sent what the user had scrolled back to: %q", got)
	}
}

// A theme change on the machine being watched is sent on, so the
// watcher does not sit on the old theme until the program next redraws.
func TestAWatcherIsSentTheScreenWhenTheThemeChanges(t *testing.T) {
	pane, shell, w := watchedPane(t, 40, 8)
	readUntil(t, w, "\x1b[H\x1b[2J")
	shell.out <- []byte("printed before")
	readUntil(t, w, "printed before")

	paper, have := themes.Named(themes.Built(), "Paper")
	if !have {
		t.Fatal("there is no Paper theme to change to")
	}
	pal, err := paper.Palette()
	if err != nil {
		t.Fatalf("Paper's colours: %v", err)
	}
	pane.SetPalette(pal)

	// The screen again, with the program having said nothing more.
	readUntil(t, w, "printed before")
}

// A watcher that fell too far behind is given the whole screen, not the
// part of the stream that survived.
//
// What was dropped cannot be pieced back together: a chunk is cut
// wherever a read ended, so it can be the middle of an escape sequence,
// and a parser left inside one eats whatever comes next.
func TestAWatcherThatFellBehindIsGivenTheScreen(t *testing.T) {
	pane, shell, w := watchedPane(t, 40, 8)

	// Nothing is read while far more than the queue holds goes past.
	for range watchQueue * 2 {
		shell.out <- []byte("filling\r\n")
	}
	shell.out <- []byte("the-last-thing-said")
	waitUntil(t, "the pane to show the last thing said", func() bool {
		return strings.Contains(paneText(pane), "the-last-thing-said")
	})

	if !w.behind.Load() {
		t.Fatal("it never noticed it had fallen behind")
	}
	got := readUntil(t, w, "the-last-thing-said")
	if !strings.Contains(got, "\x1b[H\x1b[2J") {
		t.Errorf("it carried on mid-sentence instead of sending the screen: %q", got)
	}
}

// A program that has gone ends the session watching it, rather than
// leaving a pane on a dead screen with nothing to say why.
func TestAWatcherIsToldWhenTheProgramHasGone(t *testing.T) {
	_, shell, w := watchedPane(t, 40, 8)
	readUntil(t, w, "\x1b[H\x1b[2J")

	if err := shell.Close(); err != nil {
		t.Fatalf("end the shell: %v", err)
	}

	// The read ends.
	for i := 0; ; i++ {
		_, err := readOnce(t, w)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if i > 20 {
			t.Fatal("the read never ended")
		}
	}

	// The wait comes back without anybody closing it by hand.
	done := make(chan error, 1)
	go func() { done <- w.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("waiting gave %v", err)
		}
	case <-time.After(waitBudget):
		t.Error("waiting needed somebody to close it by hand")
	}

	// And typing into it is refused rather than accepted and dropped.
	if _, err := w.Write([]byte("x")); err == nil {
		t.Error("typing into a program that has gone was accepted")
	}
}

// Watching something that has already finished fails, rather than
// handing back a session that opens empty and ends at once.
func TestWatchingSomethingAlreadyFinishedFails(t *testing.T) {
	shell := newPipeSession()
	pane, err := term.New(term.Config{Session: shell, Size: ui.Size{Cols: 20, Rows: 3}})
	if err != nil {
		t.Fatalf("pane: %v", err)
	}
	pane.Layout(ui.Size{Cols: 20, Rows: 3})
	defer func() { _ = pane.Close() }()

	if err := shell.Close(); err != nil {
		t.Fatalf("end the shell: %v", err)
	}
	waitUntil(t, "the pane to notice the shell went", func() bool { return pane.Exited() })

	w, err := newWatched(pane)
	if err == nil {
		_ = w.Close()
		t.Fatal("it handed back a watch on a program that had gone")
	}
	if !errors.Is(err, term.ErrEnded) {
		t.Errorf("it said %v", err)
	}
}

// Watching does not resize the pane, which is drawn on that machine
// too. The size of somebody else's shell is not a watcher's to set.
func TestAWatcherMayNotResizeWhatItIsWatching(t *testing.T) {
	pane, _, w := watchedPane(t, 40, 8)

	if err := w.Resize(10, 4); err == nil {
		t.Error("it agreed to resize a pane drawn on this machine")
	}
	if got := pane.Size(); got.Cols != 40 || got.Rows != 8 {
		t.Errorf("the pane is now %v", got)
	}
}

// Asking to watch from a window that is closing comes back, rather than
// parking the goroutine serving that client for the rest of the process.
func TestAttachingFromAClosingWindowComesBack(t *testing.T) {
	a := newTestApp(t, 90, 30)
	ctx, cancel := context.WithCancel(context.Background())
	a.ctx = ctx
	cancel()

	done := make(chan error, 1)
	go func() {
		_, err := a.attachTo(serve.Attached{ID: "1"}, 80, 24)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("it handed back a session from a window that is closing")
		}
	case <-time.After(waitBudget):
		t.Fatal("it parked waiting for a window that will never draw again")
	}
}

// A reader waiting for output is woken when what it was waiting for is
// thrown away.
//
// The drop is the last thing that happens: the program said nothing
// after it. Without a wake-up the reader would sleep until the program
// next spoke, which a shell sitting at a prompt never does.
func TestADropWakesAReaderWaitingForOutput(t *testing.T) {
	_, _, w := watchedPane(t, 40, 8)

	// The screen it was given on arrival, so the queue is empty.
	readUntil(t, w, "\x1b[H\x1b[2J")

	// Filled and then overflowed, with nothing said afterwards.
	f := feed{w: w}
	for i := 0; i <= watchQueue; i++ {
		if _, err := f.Write([]byte("filling\r\n")); err != nil {
			t.Fatalf("fill: %v", err)
		}
	}
	if !w.behind.Load() {
		t.Fatal("it never noticed it had fallen behind")
	}

	// The read comes back, without the program saying anything more.
	got, err := readOnce(t, w)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) == 0 {
		t.Error("it came back with nothing")
	}
}

// A client is handed what it asked for, or nothing.
//
// The ID names a place in a list the window builds afresh, so the client
// sends back the machine, the kind and the label it was shown. A name
// whose row now says something else is refused rather than handed over.
func TestAttachingChecksTheRowStillSaysWhatTheClientWasTold(t *testing.T) {
	a := newTestApp(t, 90, 30)

	var want serve.Attached
	var e *conns.Entry
	for _, g := range a.registry.Groups(panelNow) {
		for _, row := range g.Rows {
			if row.Kind != conns.Terminal {
				continue
			}
			want = serve.Attached{ID: row.ID(), Host: g.Host, Kind: row.Kind.String()}
			e = row.Entry
		}
	}
	if want.ID == "" {
		t.Fatal("the window has no shell for a client to watch")
	}

	// The same name, standing for a different kind of thing, and for a
	// thing on a different machine.
	for _, what := range []string{"kind", "host"} {
		stale := want
		switch what {
		case "kind":
			stale.Kind = conns.Files.String()
		case "host":
			stale.Host = "margit"
		}
		if _, err := a.watchPane(stale, 80, 24); err == nil {
			t.Fatalf("a client told the %s was something else was handed the pane", what)
		} else if !strings.Contains(err.Error(), "no longer") {
			t.Errorf("it refused with %q, want it to say the name stands for something else", err)
		}
	}

	// The label is not part of the check: a shell sets its own title, so
	// it changes at every prompt, and a client that was told one title
	// must not be refused for asking a moment later.
	e.Label = "vim README.md"
	sess, err := a.watchPane(want, 80, 24)
	if err != nil {
		t.Fatalf("a pane whose title had changed was refused: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// And what the client really was told about is still handed over.
	sess, err = a.watchPane(want, 80, 24)
	if err != nil {
		t.Fatalf("watchPane: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
