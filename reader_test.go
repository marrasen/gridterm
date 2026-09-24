package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/files"
	"github.com/marrasen/gridterm/vfs"
)

// aReadableFile writes a file on this machine and returns its name and
// its path.
func aReadableFile(t *testing.T, lines ...string) (name, path string) {
	t.Helper()
	name = "notes.txt"
	path = filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	return name, path
}

// Opening a file from the browser puts a reader in the window, with a
// row on the sidebar under the machine the file is on.
func TestOpeningAFileFromTheBrowserPutsAReaderInTheWindow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one", "two", "three")

	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}

	r := onlyReader(t, a)
	waitUntil(t, "the file to be read", func() bool {
		a.pump.run()
		return r.Lines() > 0
	})
	if got := r.Lines(); got != 3 {
		t.Errorf("the reader holds %d lines, want the three in the file", got)
	}
	held := a.readers[r]
	if held == nil {
		t.Fatal("the reader has no row on the sidebar")
	}
	if held.row.Kind != conns.Reader {
		t.Errorf("the row is a %v, want a reader", held.row.Kind)
	}
	if held.row.Label != name {
		t.Errorf("the row is called %q, want the file's name", held.row.Label)
	}
}

// Closing a reader takes its row off the sidebar and its pane out of the
// window.
func TestClosingAReaderTakesItsRowAway(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one")
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	before := a.registry.Len()

	if err := a.closePane(r); err != nil {
		t.Fatalf("close it: %v", err)
	}

	if len(a.readers) != 0 {
		t.Errorf("the window still holds %d readers", len(a.readers))
	}
	if got := a.registry.Len(); got != before-1 {
		t.Errorf("the sidebar holds %d rows, want one fewer than the %d it had", got, before)
	}
	if inTree(a, r) {
		t.Error("the reader is still in the tree")
	}
}

// A reader is a pane: the window names it, files it under the machine
// the file is on, and lists it among the panes to switch between.
func TestAReaderIsAPane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one")
	if err := a.openReader(vfs.NewLocal(), "kettle", path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)

	if got := a.paneName(r); !strings.Contains(got, name) {
		t.Errorf("the window calls it %q, want the file's name in it", got)
	}
	if got := a.entryOf(r); got == nil {
		t.Error("the window finds no row for it")
	}
	if !a.isPane(r) {
		t.Error("a reader is not a pane, so nothing can close it")
	}
}

// A file that will not read leaves the reader saying why, on the pane
// and on its row.
func TestAFileThatWillNotReadSaysWhyOnItsRow(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	gone := filepath.Join(t.TempDir(), "gone.txt")

	if err := a.openReader(vfs.NewLocal(), conns.Local, gone, "gone.txt", false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}

	r := onlyReader(t, a)
	waitUntil(t, "the read to fail", func() bool {
		a.pump.run()
		return r.Err() != nil
	})
	if got := a.readerNote(r); got == "" {
		t.Error("the row says nothing about a file that would not read")
	}
}

// onlyReader is the window's one reader.
func onlyReader(t *testing.T, a *testApp) *files.Reader {
	t.Helper()
	if len(a.readers) != 1 {
		t.Fatalf("the window holds %d readers, want the one", len(a.readers))
	}
	for r := range a.readers {
		return r
	}
	return nil
}

// inTree reports whether a widget is still in the window's tree.
func inTree(a *testApp, w ui.Widget) bool {
	return slices.Contains(ui.Leaves(a.root.Widget()), w)
}

// A reader following a file picks up what is written to it, and stays at
// the end as it grows.
//
// The file is longer than the pane on purpose. A file that fits has
// nowhere to scroll, so its reader is at the end whatever following did.
func TestAFollowingReaderPicksUpWhatIsWritten(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	was := logOf(60)
	name, path := aReadableFile(t, was...)
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	waitUntil(t, "the first read", func() bool {
		a.pump.run()
		return r.Lines() == len(was)
	})
	r.Follow(true)
	wasTop := r.Top()
	if wasTop == 0 {
		t.Fatal("the file is not longer than the pane, so this proves nothing")
	}

	// The file grows, the way a log does.
	grown := logOf(90)
	if err := os.WriteFile(path, []byte(strings.Join(grown, "\n")), 0o600); err != nil {
		t.Fatalf("write the file: %v", err)
	}
	at := time.Now()
	waitUntil(t, "the reader to pick up the new lines", func() bool {
		at = at.Add(followEvery)
		a.followReaders(at)
		a.pump.run()
		return r.Lines() == len(grown)
	})

	if got := r.Top(); got <= wasTop {
		t.Errorf("the reader is on line %d, where it was before the file grew", got+1)
	}
	if !r.AtEnd() {
		t.Error("the reader followed the file and did not stay at its end")
	}
}

// logOf is a file of n lines, each saying which it is.
func logOf(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "line " + strconv.Itoa(i+1)
	}
	return out
}

// A file that has not changed is not read again, or a reader following a
// quiet log would read it sixty times a second.
func TestAFileThatHasNotChangedIsNotReadAgain(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one", "two")
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	waitUntil(t, "the first read", func() bool {
		a.pump.run()
		return r.Lines() == 2
	})
	r.Follow(true)

	// Asked about twice, so the second answer is compared with the
	// first rather than with nothing.
	at := time.Now()
	reads := 0
	for range 4 {
		at = at.Add(followEvery)
		a.followReaders(at)
		waitUntil(t, "the question to come back", func() bool {
			a.pump.run()
			return !a.readers[r].checking
		})
		if r.Busy() {
			reads++
			waitUntil(t, "that read", func() bool {
				a.pump.run()
				return !r.Busy()
			})
		}
	}

	if reads > 1 {
		t.Errorf("a file nobody wrote to was read %d times over", reads)
	}
}

// A reader takes the keys when it is focused, and says so on its bar.
func TestAReaderTakesTheKeys(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one", "two")
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)

	a.focus(r)

	if !r.Focused() {
		t.Error("the window focused the reader and it does not know")
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != ui.Widget(r) {
		t.Errorf("the keys are on %T, want the reader", got)
	}
}

// With a reader in front, the window acts on the machine the file is on.
func TestAReaderInFrontIsOnItsOwnMachine(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one")
	if err := a.openReader(vfs.NewLocal(), "kettle", path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	a.focus(onlyReader(t, a))

	if got := a.currentHost(); got != "kettle" {
		t.Errorf("the window is on %q, want the machine the file is on", got)
	}
}

// A reader's row is named the way the sidebar names a kind, not
// "Unknown".
func TestAReadersKindHasAName(t *testing.T) {
	if got := conns.Reader.String(); got == "Unknown" || got == "" {
		t.Errorf("a reader's kind is called %q", got)
	}
}

// Closing the browser pane a reader was opened from leaves the reader
// able to read: the connection it reads through is still its to use.
func TestClosingTheBrowserLeavesAReaderAbleToRead(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "one", "two")
	f := vfs.NewLocal()
	if err := a.openReader(f, conns.Local, path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	waitUntil(t, "the first read", func() bool {
		a.pump.run()
		return r.Lines() == 2
	})

	// The browser lets the filesystem go while the reader still has it.
	if err := a.browserLetGoFS(f); err != nil {
		t.Fatalf("let it go: %v", err)
	}

	if a.fsHeld[f] == 0 {
		t.Error("the reader's hold on the filesystem was not counted")
	}
	if !a.fsGone[f] {
		t.Error("the browser letting go was not remembered")
	}
	// And the reader can still read through it.
	r.Open()
	waitUntil(t, "the reread", func() bool {
		a.pump.run()
		return !r.Busy()
	})
	if err := r.Err(); err != nil {
		t.Errorf("the reread failed after the browser let go: %v", err)
	}

	// Closing the reader is what finally lets it go.
	if err := a.closePane(r); err != nil {
		t.Fatalf("close the reader: %v", err)
	}
	if a.fsHeld[f] != 0 || a.fsGone[f] {
		t.Error("the filesystem was not let go of when the last reader closed")
	}
}

// Dragging over a reader picks text out, and the window's copy puts it
// on the clipboard.
func TestDraggingOverAReaderCopiesWhatWasPickedOut(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	name, path := aReadableFile(t, "hello there", "second line")
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	waitUntil(t, "the file to be read", func() bool {
		a.pump.run()
		return r.Lines() > 0
	})
	a.focus(r)
	a.relayout()

	area, on := a.paneArea(r)
	if !on {
		t.Fatal("the reader has no room in the window")
	}
	// The first line of the file, which is the row under the name.
	row := area.Y + 1
	for _, ev := range []input.MouseEvent{
		{Kind: input.MousePress, Button: input.MouseLeft, Col: area.X, Row: row},
		{Kind: input.MouseMove, Button: input.MouseLeft, Col: area.X + 4, Row: row},
		{Kind: input.MouseRelease, Button: input.MouseLeft, Col: area.X + 4, Row: row},
	} {
		if _, err := a.root.HandleMouse(ev); err != nil {
			t.Fatalf("the drag failed: %v", err)
		}
	}

	if got, want := r.SelectedText(), "hello"; got != want {
		t.Fatalf("the drag picked out %q, want %q", got, want)
	}
	a.commands()
	if err := a.root.Commands.Run(copyCommand); err != nil {
		t.Fatalf("the copy: %v", err)
	}
	waitFor(t, a, "the text to reach the clipboard", func() bool {
		return a.copiedText() == "hello"
	})
}

// A read posts how far it has got no more often than a frame. A read
// off a local disk comes back in sixty-four kilobyte chunks, and one
// post per chunk would be thousands a second for a number nobody can
// read that fast.
func TestHowFarAReadHasGotIsPostedNoFasterThanAFrame(t *testing.T) {
	a := newTestApp(t, 80, 24)
	watch := a.watchRead(files.NewReader("x", "/tmp/x"))

	for range 1000 {
		watch(1)
	}

	if got := a.pump.pending(); got > 1 {
		t.Errorf("a thousand counts posted %d times, want at most one inside a frame", got)
	}
}

// The counts do get through, once a frame has passed.
func TestHowFarAReadHasGotDoesGetThrough(t *testing.T) {
	a := newTestApp(t, 80, 24)
	r := files.NewReader("x", "/tmp/x")
	watch := a.watchRead(r)

	watch(1)
	if got := a.pump.pending(); got != 1 {
		t.Fatalf("the first count posted %d times, want one", got)
	}
	time.Sleep(progressEvery + 20*time.Millisecond)
	watch(2)

	if got := a.pump.pending(); got != 2 {
		t.Errorf("a count after a frame posted %d times in all, want two", got)
	}
}

// The count reaches the pane, which is the whole point: a file coming
// down a slow connection says how much of it has arrived.
func TestHowFarAReadHasGotReachesThePane(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	lines := make([]string, 400)
	for i := range lines {
		lines[i] = "a line of text"
	}
	name, path := aReadableFile(t, lines...)
	e, err := vfs.NewLocal().Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false, e.Size); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	waitUntil(t, "the file to be read", func() bool {
		a.pump.run()
		return r.Lines() > 0
	})

	// Every count posted during the read ran through the pump above.
	// What it must not have done is leave one behind that puts the
	// pane back to reading after it has finished.
	if r.Busy() {
		t.Fatal("the read is still out")
	}
	if got := r.SoFar(); got != 0 {
		t.Errorf("the reader still holds a count of %d after the read finished", got)
	}
	if got := r.Where(); strings.Contains(got, "reading") {
		t.Errorf("the top line says %q after the read finished", got)
	}
}

// The size the browser listed reaches the reader, so a file being read
// says how much is coming rather than looking empty.
//
// The browser has the size already, from the listing it drew. Reading
// the file to find out how big it is would be the wrong way round.
func TestTheSizeTheBrowserListedReachesTheReader(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	lines := make([]string, 400)
	for i := range lines {
		lines[i] = "a line of text"
	}
	name, path := aReadableFile(t, lines...)
	e, err := vfs.NewLocal().Stat(path)
	if err != nil {
		t.Fatalf("stat the file: %v", err)
	}
	if e.Size == 0 {
		t.Fatal("the file is empty, so this proves nothing")
	}
	if err := a.openFilesOn(conns.Local); err != nil {
		t.Fatalf("open the browser: %v", err)
	}
	pane := a.files.view.Here()
	if pane == nil {
		t.Fatal("the browser has no pane")
	}
	pane.Open(filepath.Dir(path))
	waitFor(t, a, "the directory to be listed", func() bool {
		return len(pane.Entries()) > 0
	})

	if err := a.readFileFrom(pane, e, false); err != nil {
		t.Fatalf("read %s: %v", name, err)
	}

	if got := onlyReader(t, a).Expect; got != e.Size {
		t.Errorf("the reader was told the file is %d bytes, want the %d the listing said",
			got, e.Size)
	}
}

// Shift and a page key picks text out of a reader rather than scrolling
// it. Both chords are window accelerators, and an accelerator runs
// before any widget sees the key, so the reader has to claim them.
func TestShiftAndAPageKeyPicksTextOutOfAReader(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "a line of text"
	}
	name, path := aReadableFile(t, lines...)
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	waitUntil(t, "the file to be read", func() bool {
		a.pump.run()
		return r.Lines() > 0
	})
	a.focus(r)
	a.relayout()
	page := r.Top()
	if err := a.root.Commands.Run("view.scrollDown"); err != nil {
		t.Fatalf("scroll forward: %v", err)
	}
	page = r.Top() - page
	if page <= 1 {
		t.Fatalf("a page is %d lines here, too few to tell a page from a line", page)
	}
	r.Home()

	if _, err := a.root.HandleKey(press(input.KeyPageDown, input.ModShift)); err != nil {
		t.Fatalf("shift and PageDown: %v", err)
	}

	if r.SelectedText() == "" {
		t.Fatal("shift and PageDown picked nothing out, so it scrolled instead")
	}
	// The view follows the loose end of the selection, so it moves by
	// the one row that end went past the bottom, not by a whole page.
	if got := r.Top(); got >= page {
		t.Errorf("the reader moved %d lines, want it to have followed the selection", got)
	}
}

// A terminal does not claim those chords, so the window still scrolls
// the one in front. The reader's claim must not cost the terminal its
// scrollback keys.
func TestShiftAndAPageKeyStillScrollsATerminal(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	pane := onlyPaneOn(t, a)
	a.shells[0].out <- []byte(strings.Repeat("a line of text\r\n", 40))
	waitFor(t, a, "the shell to fill the scrollback", func() bool {
		return screenOf(pane).At(0, 0).Rune == 'a'
	})
	a.relayout()
	if pane.ViewOffset() != 0 {
		t.Fatalf("the terminal starts scrolled back %d", pane.ViewOffset())
	}

	if _, err := a.root.HandleKey(press(input.KeyPageUp, input.ModShift)); err != nil {
		t.Fatalf("shift and PageUp: %v", err)
	}

	if pane.ViewOffset() <= 0 {
		t.Error("shift and PageUp did not scroll the terminal back")
	}
}

// The window's scroll commands move a reader as well as a terminal.
//
// They are bound to Shift+PageUp and Shift+PageDown, and an accelerator
// runs before any widget sees the key, so without this those two chords
// do nothing at all in a reader.
func TestTheScrollCommandsMoveAReader(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	a.commands()
	lines := make([]string, 200)
	for i := range lines {
		lines[i] = "line"
	}
	name, path := aReadableFile(t, lines...)
	if err := a.openReader(vfs.NewLocal(), conns.Local, path, name, false, 0); err != nil {
		t.Fatalf("open a reader: %v", err)
	}
	r := onlyReader(t, a)
	waitUntil(t, "the file to be read", func() bool {
		a.pump.run()
		return r.Lines() > 0
	})
	a.focus(r)
	a.relayout()

	if err := a.root.Commands.Run("view.scrollDown"); err != nil {
		t.Fatalf("scroll forward: %v", err)
	}

	was := r.Top()
	if was == 0 {
		t.Fatal("scrolling forward did not move the reader down the file")
	}

	if err := a.root.Commands.Run("view.scrollUp"); err != nil {
		t.Fatalf("scroll back: %v", err)
	}

	if got := r.Top(); got >= was {
		t.Errorf("scrolling back left the reader on line %d, want above %d", got, was)
	}
}

// Ctrl+D on a file puts it away and gives the keys back to the browser
// pane it was picked in. They used to land wherever the tree put them,
// which with another pane open was not the browser.
func TestClosingAFileGoesBackToTheBrowser(t *testing.T) {
	a := newTestApp(t, 120, 30)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	name, path := aReadableFile(t, "one", "two")
	e, err := vfs.NewLocal().Stat(path)
	if err != nil {
		t.Fatalf("stat the file: %v", err)
	}
	if err := a.openFilesOn(conns.Local); err != nil {
		t.Fatalf("open the browser: %v", err)
	}
	pane := a.files.view.Here()
	pane.Open(filepath.Dir(path))
	waitFor(t, a, "the directory to be listed", func() bool {
		return len(pane.Entries()) > 0
	})
	// Another pane, opened after the browser, and then back to the
	// browser to pick the file.
	if err := a.openPane(); err != nil {
		t.Fatalf("open a terminal: %v", err)
	}
	a.focus(pane)

	if err := a.readFileFrom(pane, e, false); err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	r := onlyReader(t, a)
	if ui.FocusedLeaf(a.root.Widget()) != ui.Widget(r) {
		t.Fatal("the file did not take the keys")
	}
	if _, err := r.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyD, Mods: input.ModCtrl}); err != nil {
		t.Fatalf("Ctrl+D: %v", err)
	}
	a.pump.run()

	if inTree(a, r) {
		t.Fatal("Ctrl+D left the file open")
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != ui.Widget(pane) {
		t.Errorf("the keys went to %T, want the browser pane the file was picked in", got)
	}
}

// A file closed from its row while the user works in another pane
// leaves the keys in that pane. Only a file that had them gives them
// back to the browser.
func TestClosingAFileFromItsRowLeavesTheKeysWhereTheyAre(t *testing.T) {
	a := newTestApp(t, 120, 30)
	withPanel(t, a)
	withDialogs(t, a)
	a.commands()
	name, path := aReadableFile(t, "one", "two")
	e, err := vfs.NewLocal().Stat(path)
	if err != nil {
		t.Fatalf("stat the file: %v", err)
	}
	if err := a.openFilesOn(conns.Local); err != nil {
		t.Fatalf("open the browser: %v", err)
	}
	pane := a.files.view.Here()
	pane.Open(filepath.Dir(path))
	waitFor(t, a, "the directory to be listed", func() bool {
		return len(pane.Entries()) > 0
	})
	if err := a.readFileFrom(pane, e, false); err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	r := onlyReader(t, a)
	if err := a.openPane(); err != nil {
		t.Fatalf("open a terminal: %v", err)
	}
	working := ui.FocusedLeaf(a.root.Widget())

	if err := a.closePaneRow(a.readers[r].row); err != nil {
		t.Fatalf("press the cross: %v", err)
	}

	if inTree(a, r) {
		t.Fatal("the cross left the file open")
	}
	if got := ui.FocusedLeaf(a.root.Widget()); got != working {
		t.Errorf("the keys went to %T, want them left in the pane the user was working in", got)
	}
}
