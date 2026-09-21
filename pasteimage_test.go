package main

import (
	"errors"
	"image"
	"os"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
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
	// The Command Prompt, where ctrl+V really is paste. The shell is
	// named rather than left to the machine, because which shell a pane
	// runs is what decides this and a test that took the machine's own
	// would be asking a different question on Linux than on Windows.
	a.started[pane].argv = []string{`C:\Windows\system32\cmd.exe`}
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

// A picture pasted at a POSIX shell's own prompt goes as a file,
// because ctrl+V means something else there and something damaging.
//
// readline reads ctrl+V as quoted-insert: it takes the next character
// literally. So pressing it leaves the shell quoting the beginning of
// whatever is pasted next, which then shows its bracketed-paste markers
// as text rather than obeying them, and goes on doing that.
//
// This is about the shell and not about the machine. A WSL pane on
// Windows is a POSIX shell and reads ctrl+V the same way.
func TestAPictureAtAPosixPromptGoesAsAFile(t *testing.T) {
	for what, argv := range map[string][]string{
		"a login shell":      {"/bin/bash"},
		"zsh":                {"/usr/bin/zsh"},
		"a WSL distribution": {`C:\WINDOWS\system32\wsl.exe`, "-d", "Ubuntu"},
	} {
		t.Run(what, func(t *testing.T) {
			a, pane := aPaneToPasteInto(t, argv)
			// At a prompt, with the marks a shell with setup sends.
			atPrompt(t, a, pane)

			if err := a.pastePicture(pane); err != nil {
				t.Fatalf("paste it: %v", err)
			}

			waitFor(t, a, "the path to reach the shell", func() bool {
				return strings.Contains(a.shells[0].sentText(), ".png")
			})
			typed := lastWord(a.shells[0].sentText())
			t.Cleanup(func() { os.Remove(typed) })
			if _, err := os.Stat(typed); err != nil {
				t.Errorf("it typed a path to nothing: %v", err)
			}
			if strings.Contains(a.shells[0].sentText(), "\x16") {
				t.Error("it pressed ctrl+V at a shell that reads it as quoted-insert")
			}
		})
	}
}

// A picture pasted into a program the shell started still presses
// ctrl+V, which is the thing that works: the program is on this machine
// and reads the clipboard itself, and that is how Claude Code and the
// rest take a picture as a picture rather than as a path to one.
//
// The shell says which it is. A command is running from its C mark
// until the next prompt, and a full-screen program is known by the
// screen it asked for.
func TestAPictureInARunningProgramStillPressesPaste(t *testing.T) {
	for what, says := range map[string]string{
		"a command running":     "\x1b]133;A\a$ \x1b]133;B\aclaude\r\n\x1b]133;C\a",
		"a full-screen program": "\x1b[?1049h",
	} {
		t.Run(what, func(t *testing.T) {
			a, pane := aPaneToPasteInto(t, []string{"/bin/bash"})
			a.shells[0].out <- []byte(says)
			waitFor(t, a, "the pane to be running a program", pane.RunningAProgram)

			if err := a.pastePicture(pane); err != nil {
				t.Fatalf("paste it: %v", err)
			}

			// Ctrl+V, which is what the program reads as paste.
			waitFor(t, a, "the paste key to reach the program", func() bool {
				return strings.Contains(a.shells[0].sentText(), "\x16")
			})
		})
	}
}

// A shell that was never taught to send marks is handed the file.
//
// Nothing says whether the shell or a program is reading, and a ctrl+V
// that lands in a shell breaks the next paste with nothing on screen to
// say why. A path is the answer that is wrong in a way the user can see,
// and every program that takes a picture here reads one already.
func TestAPictureAtAShellWithNoMarksGoesAsAFile(t *testing.T) {
	a, pane := aPaneToPasteInto(t, []string{"/bin/bash"})
	// No marks: the pane has said nothing about prompts at all.
	if pane.RunningAProgram() {
		t.Fatal("a pane that has said nothing claims to be running a program")
	}

	if err := a.pastePicture(pane); err != nil {
		t.Fatalf("paste it: %v", err)
	}

	waitFor(t, a, "the path to reach the shell", func() bool {
		return strings.Contains(a.shells[0].sentText(), ".png")
	})
	t.Cleanup(func() { os.Remove(lastWord(a.shells[0].sentText())) })
	if strings.Contains(a.shells[0].sentText(), "\x16") {
		t.Error("it pressed ctrl+V without knowing what was reading")
	}
}

// aPaneToPasteInto is a local pane running argv, with a picture on the
// clipboard ready to paste.
func aPaneToPasteInto(t *testing.T, argv []string) (*testApp, *term.Terminal) {
	t.Helper()
	a := newTestApp(t, 80, 24)
	pane := firstPane(t, a)
	a.panes[pane] = &conns.Entry{Host: conns.Local, Kind: conns.Terminal}
	a.started[pane].argv = argv
	a.readClipImage = func() (image.Image, bool, error) {
		return image.NewRGBA(image.Rect(0, 0, 2, 2)), true, nil
	}
	return a, pane
}

// atPrompt makes a pane say it is sitting at a shell prompt, in the
// marks a shell with setup sends.
func atPrompt(t *testing.T, a *testApp, pane *term.Terminal) {
	t.Helper()
	a.shells[0].out <- []byte("\x1b]133;A\a$ \x1b]133;B\a")
	waitFor(t, a, "the pane to be at a prompt", func() bool {
		return !pane.RunningAProgram()
	})
}

// lastWord is the path a pane was told, which is the last thing typed
// into it once the bracketed-paste markers are off.
func lastWord(sent string) string {
	sent = strings.ReplaceAll(sent, "\x1b[200~", "")
	sent = strings.ReplaceAll(sent, "\x1b[201~", "")
	return strings.TrimSpace(sent)
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
	// The Command Prompt, so the picture goes by the shortest route and
	// this is left asking what it means to ask: that paste with no text
	// on the clipboard takes the picture. Which route a shell gets is
	// TestPastingAPictureIntoAPosixShellTypesAPathInstead's question.
	a.started[pane].argv = []string{`C:\Windows\system32\cmd.exe`}
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

// The bar says a picture is on its way while it is, and names where it
// is going.
//
// Marcus pasted a screenshot into a pane on another machine and nothing
// on screen said anything was happening until the path appeared. A
// picture is megabytes and the machine may be a long way off.
func TestTheBarSaysAPictureIsOnItsWay(t *testing.T) {
	a := newTestApp(t, 120, 30)
	withPanel(t, a)
	withMenubar(t, a)

	if got := a.sendingText(); got != "" {
		t.Fatalf("the bar says %q with nothing on its way", got)
	}

	sent := a.sendingPicture("margit")

	if got := a.sendingText(); !strings.Contains(got, "margit") {
		t.Errorf("the bar says %q, want it to name where the picture is going", got)
	}
	if !hasChip(a, "margit") {
		t.Errorf("no chip on the bar names it: %v", chipTexts(a))
	}

	sent()

	if got := a.sendingText(); got != "" {
		t.Errorf("the bar still says %q once the picture has landed", got)
	}
	if hasChip(a, "margit") {
		t.Errorf("the chip is still on the bar: %v", chipTexts(a))
	}
}

// Two on their way at once are counted rather than named, because there
// is only room on the bar for one line.
func TestTwoPicturesOnTheirWayAreCounted(t *testing.T) {
	a := newTestApp(t, 120, 30)
	withPanel(t, a)
	withMenubar(t, a)

	first := a.sendingPicture("margit")
	second := a.sendingPicture("statio")

	if got := a.sendingText(); !strings.Contains(got, "2 pictures") {
		t.Errorf("the bar says %q, want it to count them", got)
	}
	first()
	second()
	if got := a.sendingText(); got != "" {
		t.Errorf("the bar still says %q once both have landed", got)
	}
}

// chipTexts is what the chips on the bar say.
func chipTexts(a *testApp) []string {
	var out []string
	for _, c := range a.statusChips() {
		out = append(out, c.Text)
	}
	return out
}

// hasChip reports whether a chip on the bar says something.
func hasChip(a *testApp, want string) bool {
	for _, got := range chipTexts(a) {
		if strings.Contains(got, want) {
			return true
		}
	}
	return false
}
