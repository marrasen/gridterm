package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
)

// aBrowserPane returns a window with one file pane open on this
// machine, and that pane.
func aBrowserPane(t *testing.T, a *testApp) *files.Pane {
	t.Helper()
	if err := a.openFilesOn(conns.Local); err != nil {
		t.Fatalf("open the browser: %v", err)
	}
	p := a.files.view.Here()
	if p == nil {
		t.Fatal("the browser has no pane")
	}
	return p
}

// A listing that fails while its pane is still open is a dialog: the
// user is looking at that pane and waiting for the answer.
func TestAFailureForAPaneStillOpenIsADialog(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	p := aBrowserPane(t, a)

	a.reportForPane(p, "Could not open margit", errors.New("connection lost"))

	n, up := a.root.Modal().(*ui.Notice)
	if !up {
		t.Fatalf("the top dialog is %T, want a notice", a.root.Modal())
	}
	if !strings.Contains(n.Message(), "connection lost") {
		t.Errorf("the notice says %q, want the reason", n.Message())
	}
}

// A listing that fails after its pane has been closed goes to the log
// instead. The user shut that pane and stopped waiting; a dialog would
// land over whatever they are doing next and take their click.
func TestAFailureForAPaneThatHasGoneGoesToTheLog(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	withLog(t)
	p := aBrowserPane(t, a)
	// The path the browser's own close key takes.
	if err := a.closePane(p); err != nil {
		t.Fatalf("close the pane: %v", err)
	}
	if a.holdsFilePane(p) {
		t.Fatal("the pane is still open, so this proves nothing")
	}
	was := windowLog.Held()

	a.reportForPane(p, "Could not open margit", errors.New("connection lost"))

	if got := a.root.Modal(); got != nil {
		t.Errorf("a dialog (%T) landed for a pane the user had closed", got)
	}
	if windowLog.Held() == was {
		t.Error("the failure was dropped rather than written to the log")
	}
}

// The error is not dropped: it is in the log where the user can read
// it, with the reason and a word about why there was no dialog.
func TestTheFailureForAClosedPaneIsInTheLog(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	withLog(t)
	p := aBrowserPane(t, a)
	// The path the browser's own close key takes.
	if err := a.closePane(p); err != nil {
		t.Fatalf("close the pane: %v", err)
	}

	a.reportForPane(p, "Could not open margit", errors.New("connection lost"))

	r := windowLog.Open()
	defer func() { _ = r.Close() }()
	var read strings.Builder
	b := make([]byte, 4096)
	for i := 0; i < windowLog.Held(); i++ {
		n, err := r.Read(b)
		if err != nil {
			t.Fatalf("read the log back: %v", err)
		}
		read.Write(b[:n])
	}
	if !strings.Contains(read.String(), "connection lost") {
		t.Errorf("the log holds %q, want the reason in it", read.String())
	}
	if !strings.Contains(read.String(), "already been closed") {
		t.Errorf("the log holds %q, want it to say why there was no dialog", read.String())
	}
}
