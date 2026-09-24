package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// aCopyWindow is a window whose settings are in a file the test owns.
func aCopyWindow(t *testing.T) (*testApp, string) {
	t.Helper()
	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := withSettings(t, a, path); err != nil {
		t.Fatalf("settings: %v", err)
	}
	return a, path
}

// aFinishedCopy runs a copy of one file between two directories the test
// owns, waits for it to finish and opens its dialog.
func aFinishedCopy(t *testing.T, a *testApp) (*jobPane, string, string) {
	t.Helper()
	at := t.TempDir()
	into := t.TempDir()
	if err := os.WriteFile(filepath.Join(at, "deploy.log"), []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write it: %v", err)
	}
	here := jobEnd{host: conns.Local}
	op := jobs.Op{
		Kind: jobs.Copy,
		From: vfs.NewLocal(), At: at, Names: []string{"deploy.log"},
		To: vfs.NewLocal(), Into: into,
	}
	j := a.runJob(op, here, here, nil)
	if j == nil {
		t.Fatal("the copy did not start")
	}
	waitFor(t, a, "the copy to finish", func() bool { return j.Progress().Done })

	row := theRowFor(t, a, j)
	a.showJobPane(j, row, here, here)
	d := theJobPane(t, a)
	return d, at, into
}

// A copy the user asks to keep is kept, and it is kept with both ends
// and the files it copies.
func TestACopyAskedToBeKeptIsKept(t *testing.T) {
	a, _ := aCopyWindow(t)
	d, at, into := aFinishedCopy(t, a)

	pressChoice(t, d, fldSaveCopy)

	kept := a.copies.all()
	if len(kept) != 1 {
		t.Fatalf("the window kept %v, want the one copy", kept)
	}
	want := settings.SavedCopy{
		From: conns.Local, To: conns.Local,
		At: at, Into: into, Names: []string{"deploy.log"},
	}
	if !kept[0].Same(want) {
		t.Errorf("it kept %+v, want %+v", kept[0], want)
	}
}

// The box says whether the copy is saved, and turning it back off drops
// it again.
//
// A box rather than a button that renamed itself between Remember and
// Forget: ticked says it is saved and unticked says it is not, which one
// button could only say by being read twice.
func TestTheSaveBoxSaysWhetherTheCopyIsKept(t *testing.T) {
	a, _ := aCopyWindow(t)
	d, _, _ := aFinishedCopy(t, a)

	if !offersChoice(d, fldSaveCopy) {
		t.Fatalf("the finished pane offers %v, with no box that saves the copy",
			choiceTitles(d))
	}
	if ticked(d, fldSaveCopy) {
		t.Error("the box starts ticked for a copy that is not saved")
	}

	pressChoice(t, d, fldSaveCopy)
	if !ticked(d, fldSaveCopy) || len(a.copies.all()) != 1 {
		t.Fatalf("the box is %v and the window keeps %v",
			ticked(d, fldSaveCopy), a.copies.all())
	}

	pressChoice(t, d, fldSaveCopy)
	if ticked(d, fldSaveCopy) {
		t.Error("the box is still ticked for a copy that was dropped")
	}
	if got := a.copies.all(); len(got) != 0 {
		t.Errorf("the window still keeps %v", got)
	}
}

// A kept copy outlives the window: it is in the settings file, so the
// next run reads it back.
func TestAKeptCopyIsInTheSettingsFile(t *testing.T) {
	a, path := aCopyWindow(t)
	d, _, _ := aFinishedCopy(t, a)

	pressChoice(t, d, fldSaveCopy)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the settings: %v", err)
	}
	if !strings.Contains(string(raw), "deploy.log") {
		t.Errorf("the settings file holds %q, want the copy in it", raw)
	}
}

// Only a copy can be kept. Doing a delete again would take something
// away without asking, and a move has already taken the original.
func TestOnlyACopyCanBeKept(t *testing.T) {
	for what, kind := range map[string]jobs.Kind{
		"a move":   jobs.Move,
		"a delete": jobs.Delete,
	} {
		op := jobs.Op{Kind: kind, At: "/a", Into: "/b", Names: []string{"x"}, To: vfs.NewLocal()}
		if _, can := asSavedCopy(op, jobEnd{}, jobEnd{}); can {
			t.Errorf("%s can be kept", what)
		}
	}
	op := jobs.Op{Kind: jobs.Copy, At: "/a", Into: "/b", Names: []string{"x"}, To: vfs.NewLocal()}
	if _, can := asSavedCopy(op, jobEnd{}, jobEnd{}); !can {
		t.Error("a copy cannot be kept")
	}
}

// Running a remembered copy copies the files again, without a browser
// pane anywhere.
func TestRunningARememberedCopyCopiesAgain(t *testing.T) {
	a, _ := aCopyWindow(t)
	d, at, into := aFinishedCopy(t, a)
	pressChoice(t, d, fldSaveCopy)
	// Taken away, so the copy has to put it back.
	landed := filepath.Join(into, "deploy.log")
	if err := os.Remove(landed); err != nil {
		t.Fatalf("take it away: %v", err)
	}

	kept := a.copies.all()
	if err := a.runSavedCopy(kept[0]); err != nil {
		t.Fatalf("run it again: %v", err)
	}

	waitFor(t, a, "the file to arrive again", func() bool {
		_, err := os.Stat(landed)
		return err == nil
	})
	if _, err := os.Stat(filepath.Join(at, "deploy.log")); err != nil {
		t.Errorf("the file it copies from went: %v", err)
	}
}

// A copy through a window this one is no longer connected to says so
// rather than starting work that cannot be done.
func TestARememberedCopyThroughAWindowThatHasGoneSaysSo(t *testing.T) {
	a, _ := aCopyWindow(t)

	err := a.runSavedCopy(settings.SavedCopy{
		From: "picard", FromWindow: "nyli", To: conns.Local,
		At: "/logs", Into: "/tmp", Names: []string{"deploy.log"},
	})

	if err == nil {
		t.Fatal("it started a copy through a window that is not connected")
	}
	if !strings.Contains(err.Error(), "nyli") {
		t.Errorf("it says %q, want it to name the window", err)
	}
}

// The list says what each copy copies and which way it goes.
func TestTheListSaysWhatACopyDoes(t *testing.T) {
	for what, tc := range map[string]struct {
		copy       settings.SavedCopy
		text, note string
	}{
		"one file": {
			settings.SavedCopy{From: "picard", At: "/var/log", Names: []string{"deploy.log"}},
			"deploy.log", "picard → Local",
		},
		"several files": {
			settings.SavedCopy{To: "margit", At: "/var/log", Names: []string{"a.log", "b.log"}},
			"2 files from log", "Local → margit",
		},
		"through a window": {
			settings.SavedCopy{From: "picard", FromWindow: "nyli", At: "/logs", Names: []string{"x"}},
			"x", "picard on nyli → Local",
		},
	} {
		if got := copiedWhat(tc.copy); got != tc.text {
			t.Errorf("%s: the row reads %q, want %q", what, got, tc.text)
		}
		if got := copiedWhere(tc.copy); got != tc.note {
			t.Errorf("%s: the note reads %q, want %q", what, got, tc.note)
		}
	}
}

// The dialog lists what is kept, and the cross at the end of a row takes
// that copy off the list without closing the dialog.
func TestTheDialogListsAndForgets(t *testing.T) {
	a, _ := aCopyWindow(t)
	d, _, _ := aFinishedCopy(t, a)
	pressChoice(t, d, fldSaveCopy)

	if err := a.openCopies(); err != nil {
		t.Fatalf("open the list: %v", err)
	}
	c := awaitModal[*ui.Chooser](t, a, "the list of kept copies", nil)
	if got := c.Len(); got != 1 {
		t.Fatalf("the list holds %d copies, want the one kept", got)
	}

	// The cross is on the row, not only in the chooser: a button nothing
	// draws is one nobody can press.
	rows := c.Rows()
	if len(rows) != 1 || rows[0].Button != forgetButton {
		t.Fatalf("the row carries %q, want the cross %q", rows[0].Button, forgetButton)
	}

	if err := c.Press(0); err != nil {
		t.Fatalf("the cross: %v", err)
	}

	if got := a.copies.all(); len(got) != 0 {
		t.Errorf("the window still keeps %v", got)
	}
	if got := c.Len(); got != 0 {
		t.Errorf("the list still shows %d copies", got)
	}
	if a.root.Modal() != ui.Widget(c) {
		t.Error("forgetting a copy closed the list")
	}
}

// With nothing kept the dialog says where a copy is kept from, rather
// than opening an empty list.
func TestTheDialogWithNothingKeptSaysWhereToKeepOne(t *testing.T) {
	a, _ := aCopyWindow(t)

	if err := a.openCopies(); err != nil {
		t.Fatalf("open the list: %v", err)
	}

	n := awaitModal[*ui.Notice](t, a, "a word about keeping one", nil)
	if !strings.Contains(n.Message(), "Save this copy") {
		t.Errorf("it says %q, want it to name the box that keeps one", n.Message())
	}
}

// A copy through a window this one has taken over is kept with the
// machine's name and the window's, not the window's twice.
//
// The panel files such an end under the window, and the machine it
// reached is where the files really are.
func TestACopyThroughAWindowKeepsBothNames(t *testing.T) {
	nyli := &taken{name: "nyli"}
	from := jobEnd{host: "nyli", far: remoteHostKey{window: nyli, host: "picard"}}
	to := jobEnd{host: conns.Local}
	op := jobs.Op{
		Kind: jobs.Copy, At: "/var/log", Into: "/tmp",
		Names: []string{"deploy.log"}, To: vfs.NewLocal(),
	}

	saved, can := asSavedCopy(op, from, to)

	if !can {
		t.Fatal("a copy through a window cannot be kept")
	}
	if saved.From != "picard" {
		t.Errorf("it kept the machine as %q, want picard", saved.From)
	}
	if saved.FromWindow != "nyli" {
		t.Errorf("it kept the window as %q, want nyli", saved.FromWindow)
	}
	if got, want := copiedWhere(saved), "picard on nyli → Local"; got != want {
		t.Errorf("the note reads %q, want %q", got, want)
	}
}

// Running such a copy opens the machine on that window, not the window
// on itself.
func TestARememberedCopyThroughAWindowOpensTheMachine(t *testing.T) {
	a, _ := aCopyWindow(t)
	nyli := &taken{name: "nyli"}
	a.windows.add(nyli)

	end, err := a.endOfSaved("picard", "", "nyli")

	if err != nil {
		t.Fatalf("find the end: %v", err)
	}
	if end.far.window != nyli {
		t.Errorf("it opens through %v, want nyli", end.far.window)
	}
	if end.far.host != "picard" {
		t.Errorf("it opens %q, want picard", end.far.host)
	}
}

// A copy that cannot be kept is offered no button, rather than one whose
// only outcome is an error.
func TestACopyThatCannotBeKeptOffersNoButton(t *testing.T) {
	a, _ := aCopyWindow(t)
	here := jobEnd{host: conns.Local}
	// A copy of nothing: there are no files to name, so there is nothing
	// to run a second time.
	j := a.runJob(jobs.Op{
		Kind: jobs.Copy,
		From: vfs.NewLocal(), At: t.TempDir(),
		To: vfs.NewLocal(), Into: t.TempDir(),
	}, here, here, nil)
	if j == nil {
		t.Fatal("the copy did not start")
	}
	waitFor(t, a, "the copy to finish", func() bool { return j.Progress().Done })
	a.showJobPane(j, theRowFor(t, a, j), here, here)
	d := theJobPane(t, a)

	if offersChoice(d, fldSaveCopy) {
		t.Errorf("the pane offers %v, want no box on work that cannot be kept",
			choiceTitles(d))
	}
}

// The order the files were picked out in does not make one copy two.
func TestTheOrderOfTheNamesDoesNotMakeACopyTwice(t *testing.T) {
	one := settings.SavedCopy{At: "/a", Into: "/b", Names: []string{"x", "y"}}
	other := settings.SavedCopy{At: "/a", Into: "/b", Names: []string{"y", "x"}}

	if !one.Same(other) {
		t.Error("the same two files picked out the other way round is a different copy")
	}
}

// The box says what the saved list holds, not what it was last set to,
// so one pane does not go on saying a copy is unsaved once it is saved.
func TestTheSaveBoxFollowsTheList(t *testing.T) {
	a, _ := aCopyWindow(t)
	d, _, _ := aFinishedCopy(t, a)
	saved, can := asSavedCopy(d.job.Op(), d.from, d.to)
	if !can {
		t.Fatal("the copy cannot be kept")
	}
	if !offersChoice(d, fldSaveCopy) {
		t.Fatalf("the finished pane offers %v, with no box", choiceTitles(d))
	}

	// Kept behind this pane's back, the way another pane on the same
	// copy would.
	if err := a.copies.keep(saved); err != nil {
		t.Fatalf("keep it: %v", err)
	}
	if !ticked(d, fldSaveCopy) {
		t.Error("the box still says the copy is not saved")
	}

	// And dropped again, the same way.
	if err := a.copies.forget(saved); err != nil {
		t.Fatalf("forget it: %v", err)
	}
	if ticked(d, fldSaveCopy) {
		t.Error("the box still says the copy is saved")
	}
}

// A copy done again is watched in the pane it was done again from, with
// the focus on Close while it runs: Cancel is drawn where Repeat was, so
// an Enter pressed twice would otherwise stop the copy it just started.
func TestARepeatIsWatchedInTheSamePane(t *testing.T) {
	a, _ := aCopyWindow(t)
	d, _, _ := aFinishedCopy(t, a)
	first := d.job
	jobPaneText(d)

	pressChoice(t, d, btnRepeat)

	if d.job == first {
		t.Fatal("the pane still shows the copy that finished")
	}
	if n := len(a.jobPanes); n != 1 {
		t.Errorf("the window holds %d panes on the work, want the one", n)
	}
	running := jobs.Progress{Files: 1, Started: time.Now()}
	jobPaneDrawn(d, running, time.Now())
	if got := d.choicesFor(running)[d.at].title; got != btnClose {
		t.Errorf("the focus is on %q while the repeat runs, want Close", got)
	}
}
