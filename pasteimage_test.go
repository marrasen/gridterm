package main

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui"
)

// A picture is written to a file of its own and reads back as the one
// that went in.
func TestAPastedPictureIsWrittenAndReadsBack(t *testing.T) {
	want := image.NewRGBA(image.Rect(0, 0, 2, 1))
	want.SetRGBA(0, 0, color.RGBA{0x10, 0x20, 0x30, 0xff})
	want.SetRGBA(1, 0, color.RGBA{0x40, 0x50, 0x60, 0xff})
	at := time.Date(2026, 9, 18, 22, 15, 30, 0, time.UTC)

	path, err := writePastedImage(want, func() time.Time { return at })

	if err != nil {
		t.Fatalf("write it: %v", err)
	}
	t.Cleanup(func() { os.Remove(path) })
	if dir := filepath.Dir(path); filepath.Base(dir) != pastedDir {
		t.Errorf("it wrote to %s, want a directory of its own", dir)
	}
	if !strings.HasSuffix(path, ".png") {
		t.Errorf("it wrote %s, want a name a program can tell is a picture", path)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open what it wrote: %v", err)
	}
	defer f.Close()
	got, err := png.Decode(f)
	if err != nil {
		t.Fatalf("read what it wrote: %v", err)
	}
	if got.Bounds() != want.Bounds() {
		t.Fatalf("it wrote a %v picture, want %v", got.Bounds(), want.Bounds())
	}
	for x := range 2 {
		if got, want := got.At(x, 0), want.At(x, 0); got != want {
			t.Errorf("pixel %d is %v, want %v", x, got, want)
		}
	}
}

// Two pictures pasted in the same millisecond get a file each. The name
// holds the moment, so without numbering the second would be refused and
// the first would quietly stand in for it.
func TestTwoPicturesPastedTogetherGetTheirOwnFiles(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	at := time.Date(2026, 9, 18, 22, 15, 30, 0, time.UTC)
	clock := func() time.Time { return at }

	first, err := writePastedImage(img, clock)
	if err != nil {
		t.Fatalf("the first: %v", err)
	}
	t.Cleanup(func() { os.Remove(first) })
	second, err := writePastedImage(img, clock)
	if err != nil {
		t.Fatalf("the second: %v", err)
	}
	t.Cleanup(func() { os.Remove(second) })

	if first == second {
		t.Errorf("both pictures went to %s", first)
	}
	for _, path := range []string{first, second} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
}

// A pane on a machine reached by SSH is told so rather than handed a
// path to a file on this machine.
func TestPastingAPictureIntoAnSSHPaneSaysItCannotYet(t *testing.T) {
	a := newTestApp(t, 80, 24)
	pane := firstPane(t, a)
	a.panes[pane] = &conns.Entry{Host: "kettle", Kind: conns.Terminal}
	a.readClipImage = func() (image.Image, bool, error) {
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), true, nil
	}

	err := a.pasteImage(pane)

	if err == nil {
		t.Fatal("a picture was pasted into a pane on another machine")
	}
	if !strings.Contains(err.Error(), "kettle") {
		t.Errorf("it says %q, and it has to name the machine", err)
	}
}

// A clipboard with no picture on it is an ordinary thing to meet, and
// what comes back says so rather than reading like a fault.
func TestPastingAPictureWithNoneOnTheClipboardSaysSo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	pane := firstPane(t, a)
	a.panes[pane] = &conns.Entry{Host: conns.Local, Kind: conns.Terminal}
	a.readClipImage = func() (image.Image, bool, error) { return nil, false, nil }

	err := a.pasteImage(pane)

	if err == nil {
		t.Fatal("a picture was pasted with none on the clipboard")
	}
	if !strings.Contains(err.Error(), "no picture") {
		t.Errorf("it says %q, want it to say the clipboard holds no picture", err)
	}
}

// A clipboard that will not be read is a failure rather than an empty
// one, and what it said reaches the user.
func TestAClipboardThatWillNotBeReadIsSaid(t *testing.T) {
	a := newTestApp(t, 80, 24)
	pane := firstPane(t, a)
	a.panes[pane] = &conns.Entry{Host: conns.Local, Kind: conns.Terminal}
	a.readClipImage = func() (image.Image, bool, error) {
		return nil, true, errors.New("the clipboard is held by something else")
	}

	err := a.pasteImage(pane)

	if err == nil {
		t.Fatal("a clipboard that could not be read was passed over in silence")
	}
	if !strings.Contains(err.Error(), "held by something else") {
		t.Errorf("it says %q, want what the clipboard said", err)
	}
}

// The picture on the clipboard is written out and its path typed into
// the pane, which is how a program that reads a terminal is handed one.
func TestPastingAPictureTypesThePathItWroteTo(t *testing.T) {
	a := newTestApp(t, 80, 24)
	pane := firstPane(t, a)
	a.panes[pane] = &conns.Entry{Host: conns.Local, Kind: conns.Terminal}
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	a.readClipImage = func() (image.Image, bool, error) { return img, true, nil }

	if err := a.pasteImage(pane); err != nil {
		t.Fatalf("paste it: %v", err)
	}

	// The pane queues what it sends and a goroutine of its own writes
	// it, so the shell sees it a moment later.
	waitFor(t, a, "the path to reach the shell", func() bool {
		return strings.Contains(a.shells[0].sentText(), ".png")
	})
	typed := strings.TrimSpace(a.shells[0].sentText())
	if !strings.HasSuffix(typed, ".png") {
		t.Fatalf("it typed %q, want the path of a picture", typed)
	}
	t.Cleanup(func() { os.Remove(typed) })
	if _, err := os.Stat(typed); err != nil {
		t.Errorf("it typed a path to nothing: %v", err)
	}
}

// A clipboard holding a picture and no text says so, rather than passing
// on what the library that reads text makes of it.
//
// That library reports the last error when there is no text, and no
// error is what happened, so it says "the operation completed
// successfully" and the user is told a success went wrong.
func TestPastingTextWithAPictureOnTheClipboardSaysWhatIsThere(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	a.hasClipText = func() bool { return false }
	a.readClipImage = func() (image.Image, bool, error) {
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), true, nil
	}
	a.readClip = func() (string, error) {
		t.Error("it went to the clipboard for text that is not there")
		return "", nil
	}

	if got := a.pasteText(); got != "" {
		t.Errorf("it pasted %q", got)
	}

	n, up := a.root.Modal().(*ui.Notice)
	if !up {
		t.Fatalf("top modal = %T, want a notice saying what the clipboard holds", a.root.Modal())
	}
	said := n.Message()
	if !strings.Contains(said, "picture") {
		t.Errorf("it said %q, want it to say the clipboard holds a picture", said)
	}
	if strings.Contains(said, "completed successfully") {
		t.Errorf("it said %q, which is what a success looks like", said)
	}
	// And it names the way to paste it.
	if !strings.Contains(said, a.chordFor("edit.pasteImage")) {
		t.Errorf("it said %q, want the chord that pastes a picture", said)
	}
}

// An empty clipboard says nothing. Pasting nothing from an empty
// clipboard is what the user asked for.
func TestPastingTextWithAnEmptyClipboardSaysNothing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	a.hasClipText = func() bool { return false }
	a.readClipImage = func() (image.Image, bool, error) { return nil, false, nil }

	if got := a.pasteText(); got != "" {
		t.Errorf("it pasted %q", got)
	}

	if len(a.modals) != 0 {
		t.Errorf("an empty clipboard put %d dialogs up", len(a.modals))
	}
}
