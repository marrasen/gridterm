package main

import (
	"errors"
	"image"
	"os"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui"
)

// A pane on a machine reached by SSH is told so rather than handed a
// path to a file on this machine.
func TestPastingAPictureIntoAnSSHPaneSaysItCannotYet(t *testing.T) {
	a := newTestApp(t, 80, 24)
	pane := firstPane(t, a)
	a.panes[pane] = &conns.Entry{Host: "kettle", Kind: conns.Terminal}
	a.readClipImage = func() (image.Image, bool, error) {
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), true, nil
	}

	err := a.pastePicture(pane)

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

	err := a.pastePicture(pane)

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

	err := a.pastePicture(pane)

	if err == nil {
		t.Fatal("a clipboard that could not be read was passed over in silence")
	}
	if !strings.Contains(err.Error(), "held by something else") {
		t.Errorf("it says %q, want what the clipboard said", err)
	}
}

// A pane on this machine is handed the picture by pressing paste: it is
// already on the clipboard the program reads, so there is nothing to
// move and no file to write.
func TestPastingAPictureIntoALocalPanePressesPaste(t *testing.T) {
	a := newTestApp(t, 80, 24)
	pane := firstPane(t, a)
	a.panes[pane] = &conns.Entry{Host: conns.Local, Kind: conns.Terminal}
	a.readClipImage = func() (image.Image, bool, error) {
		return image.NewRGBA(image.Rect(0, 0, 2, 2)), true, nil
	}

	if err := a.pastePicture(pane); err != nil {
		t.Fatalf("paste it: %v", err)
	}

	// Ctrl+V, which is what a program reads as paste.
	waitFor(t, a, "the paste key to reach the shell", func() bool {
		return strings.Contains(a.shells[0].sentText(), "\x16")
	})
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
}

// Pasting into a pane takes whatever is there: text when there is text,
// and the picture when there is none. The user pressed paste, and there
// is one thing on the clipboard to paste.
func TestPastingIntoAPaneTakesThePictureWhenThereIsNoText(t *testing.T) {
	a := newTestApp(t, 80, 24)
	pane := firstPane(t, a)
	a.panes[pane] = &conns.Entry{Host: conns.Local, Kind: conns.Terminal}
	a.hasClipText = func() bool { return false }
	a.readClipImage = func() (image.Image, bool, error) {
		return image.NewRGBA(image.Rect(0, 0, 2, 2)), true, nil
	}

	if err := a.paste(pane); err != nil {
		t.Fatalf("paste: %v", err)
	}

	// The picture is already on the clipboard this pane's program
	// reads, so pressing paste is the whole of it.
	waitFor(t, a, "the paste key to reach the shell", func() bool {
		return strings.Contains(a.shells[0].sentText(), "\x16")
	})
}

// A clipboard holding both is text. That is what copying from a browser
// leaves, and the words are what was meant far more often.
func TestPastingIntoAPaneTakesTheTextWhenThereIsBoth(t *testing.T) {
	a := newTestApp(t, 80, 24)
	pane := firstPane(t, a)
	a.panes[pane] = &conns.Entry{Host: conns.Local, Kind: conns.Terminal}
	a.hasClipText = func() bool { return true }
	a.readClip = func() (string, error) { return "the words", nil }
	a.readClipImage = func() (image.Image, bool, error) {
		t.Error("it went for the picture with text on the clipboard")
		return nil, false, nil
	}

	if err := a.paste(pane); err != nil {
		t.Fatalf("paste: %v", err)
	}

	waitFor(t, a, "the text to reach the shell", func() bool {
		return strings.Contains(a.shells[0].sentText(), "the words")
	})
}

// An empty clipboard pastes nothing and says nothing.
func TestPastingIntoAPaneWithAnEmptyClipboardSaysNothing(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	pane := firstPane(t, a)
	a.hasClipText = func() bool { return false }
	a.readClipImage = func() (image.Image, bool, error) { return nil, false, nil }

	if err := a.paste(pane); err != nil {
		t.Fatalf("paste: %v", err)
	}

	if len(a.modals) != 0 {
		t.Errorf("an empty clipboard put %d dialogs up", len(a.modals))
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

// Asking for the picture as a file writes one on this machine and types
// the path, which is a name to hand to a program at a prompt.
func TestAskingForThePictureAsAFileTypesAPath(t *testing.T) {
	a := newTestApp(t, 80, 24)
	pane := firstPane(t, a)
	a.panes[pane] = &conns.Entry{Host: conns.Local, Kind: conns.Terminal}
	a.readClipImage = func() (image.Image, bool, error) {
		return image.NewRGBA(image.Rect(0, 0, 2, 2)), true, nil
	}

	if err := a.pasteImage(pane); err != nil {
		t.Fatalf("paste it as a file: %v", err)
	}

	waitFor(t, a, "the path to reach the shell", func() bool {
		return strings.Contains(a.shells[0].sentText(), ".png")
	})
	typed := strings.TrimSpace(a.shells[0].sentText())
	t.Cleanup(func() { os.Remove(typed) })
	if _, err := os.Stat(typed); err != nil {
		t.Errorf("it typed a path to nothing: %v", err)
	}
}
