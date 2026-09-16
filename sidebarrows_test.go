package main

import (
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/internal/sshtest"
	"github.com/marrasen/gridterm/vfs"
)

// halfwayFS is this machine, with the first write to a file let through
// and every write after it held until the test lets go.
//
// It stands for a copy part way through one big file: some of the bytes
// are counted rather than all or none of them, which is the state a
// filled row exists to show.
type halfwayFS struct {
	vfs.FS

	held chan struct{}

	// wrote is closed once one write has gone through, so a test can wait
	// for the copy to be part way rather than guess.
	once  sync.Once
	wrote chan struct{}

	release sync.Once
}

func newHalfwayFS() *halfwayFS {
	return &halfwayFS{
		FS:    vfs.NewLocal(),
		held:  make(chan struct{}),
		wrote: make(chan struct{}),
	}
}

// Name is what the panel calls it.
func (f *halfwayFS) Name() string { return "halfway" }

// Create makes the file, with its writes held after the first one.
func (f *halfwayFS) Create(path string, mode fs.FileMode) (io.WriteCloser, error) {
	w, err := f.FS.Create(path, mode)
	if err != nil {
		return nil, err
	}
	return &halfwayWriter{WriteCloser: w, on: f}, nil
}

// let lets the writes through. It can be called more than once, so a test
// can free a job on its way out without knowing whether it already did.
func (f *halfwayFS) let() { f.release.Do(func() { close(f.held) }) }

// partWay waits for one write to have gone through.
func (f *halfwayFS) partWay(t *testing.T) {
	t.Helper()
	select {
	case <-f.wrote:
	case <-time.After(waitBudget):
		t.Fatal("no job ever wrote to the filesystem")
	}
}

// halfwayWriter writes once and then waits for the test.
type halfwayWriter struct {
	io.WriteCloser

	on   *halfwayFS
	past bool
}

func (w *halfwayWriter) Write(p []byte) (int, error) {
	if w.past {
		<-w.on.held
	}
	n, err := w.WriteCloser.Write(p)
	w.past = true
	w.on.once.Do(func() { close(w.on.wrote) })
	return n, err
}

// A copy part way through one big file fills that share of its row. A
// single file used to sit at "0 of 1" until it was done.
func TestAJobsRowFillsAsTheBytesGo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	held := newHalfwayFS()
	t.Cleanup(held.let)
	b, from, _ := browserOnto(t, a, held)
	// More than one read, so the copy counts some of the file and then
	// stops with the rest of it to go.
	putFile(t, from, "big.bin", strings.Repeat("x", 3*64*1024))
	left := b.Panes()[0]
	left.Reload()
	waitFor(t, a, "the listing", func() bool { return !left.Busy() })

	copyTheFirstFile(t, a, b)
	held.partWay(t)

	e := theJobRow(t, a)
	var fill float64
	waitFor(t, a, "the row to fill", func() bool {
		a.refreshJobs()
		a.refreshPanel(time.Now())
		row, ok := panelRow(a, e)
		fill = row.Fill
		return ok && fill > 0
	})
	if fill >= 1 {
		t.Fatalf("the row is filled %v while the copy is part way through", fill)
	}
	// The note is the count of files, as it was.
	if e.Note != "0 of 1" {
		t.Errorf("the row says %q, want the count of files", e.Note)
	}
	// And a copy that is still going offers nothing to clear.
	if row, _ := panelRow(a, e); row.Button != 0 {
		t.Errorf("a running copy's row offers %q", row.Button)
	}

	held.let()
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})
	a.refreshPanel(time.Now())
	row, ok := panelRow(a, e)
	if !ok {
		t.Fatalf("the finished copy has no row: %v", panelText(a, time.Now()))
	}
	if row.Fill != 0 {
		t.Errorf("the finished row is filled %v, want nothing", row.Fill)
	}
}

// clickClear presses the × at the end of a row, the way a user does:
// through the tree, at the column the list drew it in.
func clickClear(t *testing.T, a *testApp, key any) {
	t.Helper()
	a.refreshPanel(time.Now())
	area, ok := a.root.AreaOf(a.side)
	if !ok {
		t.Fatal("the sidebar is not in the tree")
	}
	y := a.panel.RowTop(key)
	if y < 0 {
		t.Fatalf("no row for %v: %v", key, panelText(a, time.Now()))
	}
	took, err := a.root.HandleMouse(input.MouseEvent{
		Kind: input.MousePress, Button: input.MouseLeft,
		Col: area.X + area.Cols - 2, Row: area.Y + y,
	})
	if err != nil {
		t.Fatalf("the press on the × failed: %v", err)
	}
	if !took {
		t.Fatal("the press on the × travelled on")
	}
}

// A connection that dropped carries a × at the end of its row, and
// clicking it takes the row off the panel.
func TestTheCrossClearsADroppedConnection(t *testing.T) {
	s := sshtest.New(t)
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	a.connect(serverConfig(t, s))
	waitForPanes(t, a, 2)
	host := serverConfig(t, s).Target()
	row := serverRow(t, a, host)

	// A connection that is still up offers nothing to clear.
	a.refreshPanel(time.Now())
	if drawn, ok := panelRow(a, row); ok && drawn.Button != 0 {
		t.Errorf("a live row offers %q", drawn.Button)
	}

	// The network goes away.
	s.CloseClients()
	waitFor(t, a, "the connection to be given up on", func() bool {
		return a.machines.named(host) == nil
	})

	a.refreshPanel(time.Now())
	drawn, ok := panelRow(a, row)
	if !ok {
		t.Fatalf("the dropped connection has no row: %v", panelText(a, time.Now()))
	}
	if drawn.Button != clearButton {
		t.Fatalf("the greyed row offers %q, want the ×", drawn.Button)
	}

	clickClear(t, a, row)
	a.refreshPanel(time.Now())
	if _, ok := panelRow(a, row); ok {
		t.Errorf("the row is still on the panel: %v", panelText(a, time.Now()))
	}
}

// A copy that has finished carries the same ×, and clicking it takes the
// row off the panel without cancelling anything or saying a word.
func TestTheCrossClearsAFinishedCopy(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)

	b, from, _ := browserOnto(t, a, vfs.NewLocal())
	putFile(t, from, "one.txt", "the body")
	left := b.Panes()[0]
	left.Reload()
	waitFor(t, a, "the listing", func() bool { return !left.Busy() })

	copyTheFirstFile(t, a, b)
	e := theJobRow(t, a)
	waitFor(t, a, "the copy to finish", func() bool {
		a.refreshJobs()
		return len(a.jobs) == 0
	})

	a.refreshPanel(time.Now())
	drawn, ok := panelRow(a, e)
	if !ok {
		t.Fatalf("the finished copy has no row: %v", panelText(a, time.Now()))
	}
	if drawn.Button != clearButton {
		t.Fatalf("the finished copy's row offers %q, want the ×", drawn.Button)
	}

	clickClear(t, a, e)
	a.refreshPanel(time.Now())
	if _, ok := panelRow(a, e); ok {
		t.Errorf("the row is still on the panel: %v", panelText(a, time.Now()))
	}
	if n := len(a.queue.Jobs()); n != 0 {
		t.Errorf("the queue still holds %d jobs", n)
	}
	// Nothing was said: the copy had already finished, so clearing its
	// row is not a cancel and there is nothing to report.
	if m := a.root.Modal(); m != nil {
		t.Errorf("clearing the row put %T on screen", m)
	}
}
