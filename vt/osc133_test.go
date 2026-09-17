package vt

import "testing"

// wantCommand checks the whole command state in one go, so a test reads
// as the answer a caller would get rather than four separate questions.
func (h *harness) wantCommand(where string, want Command) {
	h.t.Helper()
	if got := h.term.Command(); got != want {
		h.t.Errorf("%s: Command() = %+v, want %+v", where, got, want)
	}
}

func TestSemanticPromptWholeCommand(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.wantCommand("before any mark", Command{})

	h.write("\x1b]133;A\x07$ ")
	h.wantCommand("after A", Command{Integrated: true})

	h.write("\x1b]133;B\x07ls\r\n")
	h.wantCommand("after B", Command{Integrated: true})

	// The command line was written on row 0, so its output begins on row
	// 1, which is the second line this screen ever had.
	h.write("\x1b]133;C\x07one\r\n")
	h.wantCommand("after C", Command{Integrated: true, Running: true, from: 1, hasFrom: true})

	h.write("\x1b]133;D;0\x07")
	h.wantCommand("after D",
		Command{Integrated: true, hasStatus: true, status: 0, Done: 1, from: 1, hasFrom: true})
}

func TestSemanticPromptNonZeroStatus(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;127\x07")
	h.wantCommand("after D;127",
		Command{Integrated: true, hasStatus: true, status: 127, Done: 1, hasFrom: true})
}

// A shell may end a command without saying how it went, and that is a
// different answer from status 0.
func TestSemanticPromptNoStatus(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D\x07")
	h.wantCommand("after a bare D", Command{Integrated: true, Done: 1, hasFrom: true})
}

func TestSemanticPromptStatusIsNotANumber(t *testing.T) {
	for _, status := range []string{"", "ok", "SIGINT", "0x7f"} {
		h := newHarness(t, 20, 4)
		h.write("\x1b]133;C\x07\x1b]133;D;" + status + "\x07")
		h.wantCommand("after D;"+status, Command{Integrated: true, Done: 1, hasFrom: true})
	}
}

// Real shells hang extra parameters off every mark. An unknown one is
// ignored rather than making the mark itself unreadable. The sequences
// here are the ones Ghostty's bash and zsh integrations send.
func TestSemanticPromptExtraParameters(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;A;redraw=last;cl=line;aid=12345\x07")
	h.wantCommand("after A with three options", Command{Integrated: true})

	h.write("\x1b]133;B;aid=12345\x07")
	h.wantCommand("after B;aid=12345", Command{Integrated: true})

	h.write("\x1b]133;C;\x07") // bash sends the trailing separator
	h.wantCommand("after C with an empty option",
		Command{Integrated: true, Running: true, hasFrom: true})

	h.write("\x1b]133;D;2;aid=12345\x07")
	h.wantCommand("after D;2;aid=12345",
		Command{Integrated: true, hasStatus: true, status: 2, Done: 1, hasFrom: true})
}

// The prompt marks a shell sends between A and B say what kind of
// prompt it is. Nothing here reads them, and they change nothing.
func TestSemanticPromptKindMarks(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;A;cl=line\x07\x1b]133;P;k=i\x07$ \x1b]133;B\x07")
	h.wantCommand("at a prompt", Command{Integrated: true})

	h.write("\x1b]133;P;k=s\x07> \x1b]133;B\x07")
	h.wantCommand("at a continuation prompt", Command{Integrated: true})
}

func TestSemanticPromptBothTerminators(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x1b\\\x1b]133;D;3\x1b\\")
	h.wantCommand("after marks ended with ST",
		Command{Integrated: true, hasStatus: true, status: 3, Done: 1, hasFrom: true})

	h = newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;3\x07")
	h.wantCommand("after marks ended with BEL",
		Command{Integrated: true, hasStatus: true, status: 3, Done: 1, hasFrom: true})
}

// A shell that has never sent a mark must not look like one that just
// reported success.
func TestSemanticPromptSilentShell(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("$ ls\r\none\r\n$ ")
	got := h.term.Command()
	h.wantCommand("after a shell that says nothing", Command{})
	if _, ok := got.Exit(); ok {
		t.Errorf("Command() = %+v: a silent shell claims an exit status", got)
	}
	if got.Done != 0 {
		t.Errorf("Command() = %+v: a silent shell counted a command finishing", got)
	}
}

// A D with no command running finishes nothing: the shell was set up
// mid-session, or a program printed the sequence itself.
func TestSemanticPromptDoneWithNoCommand(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;D;0\x07")
	h.wantCommand("after a stray D", Command{Integrated: true})

	h.write("\x1b]133;A\x07\x1b]133;D;5\x07")
	h.wantCommand("after a D following a prompt", Command{Integrated: true})
}

// Cancelling a half-typed line with ctrl-C sends a D with no C before
// it, which finishes nothing: the command never ran.
func TestSemanticPromptCancelledInput(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;A\x07$ \x1b]133;B\x07ls -l")
	h.write("\x1b]133;D;;err=CANCEL\x07")
	h.wantCommand("after a cancelled line", Command{Integrated: true})
	if len(h.ends) != 0 {
		t.Fatalf("CommandDone fired %+v for a cancelled line", h.ends)
	}
}

// An OSC 133 with no mark after it, and a mark nothing here reads, are
// both left alone.
func TestSemanticPromptUnreadableMarks(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133\x07\x1b]133;\x07\x1b]133;L\x07\x1b]133;P;k=i\x07")
	h.wantCommand("after marks nothing here reads", Command{})
}

// A C with no prompt before it still starts a command.
func TestSemanticPromptCommandWithNoPrompt(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07")
	h.wantCommand("after a C with no A", Command{Integrated: true, Running: true, hasFrom: true})

	h.write("\x1b]133;D;0\x07")
	h.wantCommand("after its D", Command{Integrated: true, hasStatus: true, Done: 1, hasFrom: true})
}

// A prompt means the shell is not running a command, even from a shell
// that never sends D. The status the prompt clears belongs to the
// command before it.
//
// It counts as a command finishing. A command stopped with ctrl+c ends
// this way, and something waiting for a command to finish is waiting for
// exactly that.
func TestSemanticPromptPromptEndsARunningCommand(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;0\x07")
	h.wantCommand("after a command that succeeded", Command{Integrated: true, hasStatus: true, Done: 1, hasFrom: true})

	h.write("\x1b]133;A\x07\x1b]133;B\x07\x1b]133;C\x07")
	h.wantCommand("after a second command started", Command{Integrated: true, Running: true, hasStatus: true, Done: 1, hasFrom: true})

	h.write("\x1b]133;A\x07")
	h.wantCommand("after a prompt with no D", Command{Integrated: true, Done: 2, hasFrom: true})
}

// A prompt where nothing was running finishes nothing, so the count
// stands still. A shell redrawing its prompt is not a command.
func TestSemanticPromptAPromptWithNothingRunningFinishesNothing(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;0\x07")
	h.wantCommand("after a command that succeeded", Command{Integrated: true, hasStatus: true, Done: 1, hasFrom: true})

	h.write("\x1b]133;A\x07\x1b]133;B\x07\x1b]133;A\x07\x1b]133;B\x07")
	h.wantCommand("after two prompts with nothing between them",
		Command{Integrated: true, hasStatus: true, Done: 1, hasFrom: true})
}

func TestSemanticPromptTwoCommands(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;A\x07\x1b]133;B\x07\x1b]133;C\x07\x1b]133;D;0\x07")
	first := h.term.Command()
	h.wantCommand("after the first command", Command{Integrated: true, hasStatus: true, Done: 1, hasFrom: true})

	h.write("\x1b]133;A\x07\x1b]133;B\x07\x1b]133;C\x07\x1b]133;D;1\x07")
	second := h.term.Command()
	h.wantCommand("after the second command", Command{Integrated: true, hasStatus: true, status: 1, Done: 2, hasFrom: true})
	if second.Done == first.Done {
		t.Errorf("Done stayed at %d across two commands", first.Done)
	}
}

func TestSemanticPromptCallback(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;7\x07")
	h.write("\x1b]133;C\x07\x1b]133;D\x07")
	want := []cmdEnd{{status: 7, ok: true}, {status: 0, ok: false}}
	if len(h.ends) != len(want) {
		t.Fatalf("CommandDone fired %d times (%+v), want %d", len(h.ends), h.ends, len(want))
	}
	for i := range want {
		if h.ends[i] != want[i] {
			t.Errorf("CommandDone %d = %+v, want %+v", i, h.ends[i], want[i])
		}
	}
}

// A stray D fires nothing: there was no command to finish.
func TestSemanticPromptCallbackNotFiredByAStrayDone(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;D;0\x07")
	if len(h.ends) != 0 {
		t.Fatalf("CommandDone fired %+v for a D with no command running", h.ends)
	}
}

func TestSemanticPromptNilCallback(t *testing.T) {
	term := New(20, 4, DefaultPalette(), 100, Callbacks{})
	if _, err := term.Write([]byte("\x1b]133;C\x07\x1b]133;D;0\x07")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := Command{Integrated: true, hasStatus: true, Done: 1, hasFrom: true}
	if got := term.Command(); got != want {
		t.Errorf("Command() = %+v, want %+v", got, want)
	}
}

// A full-screen program is not a command with a prompt around it, so
// marks it prints are ignored while it draws.
func TestSemanticPromptIgnoredOnAlternateScreen(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b[?1049h")
	h.write("\x1b]133;A\x07\x1b]133;B\x07\x1b]133;C\x07\x1b]133;D;0\x07")
	h.wantCommand("after marks from a full-screen program", Command{})
	if len(h.ends) != 0 {
		t.Fatalf("CommandDone fired %+v for a mark on the alternate screen", h.ends)
	}

	h.write("\x1b[?1049l")
	h.wantCommand("back on the ordinary screen", Command{})
}

// The command that runs a full-screen program is still a command: it is
// bracketed on the ordinary screen, either side of the alternate one.
func TestSemanticPromptSurvivesAFullScreenProgram(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;A\x07\x1b]133;B\x07\x1b]133;C\x07")
	h.write("\x1b[?1049h")
	h.wantCommand("while the program draws", Command{Integrated: true, Running: true, hasFrom: true})

	h.write("\x1b[?1049l")
	h.write("\x1b]133;D;0\x07")
	h.wantCommand("after the program exits", Command{Integrated: true, hasStatus: true, Done: 1, hasFrom: true})
}

func TestSemanticPromptTwoStartsOneFinish(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;C\x07")
	h.wantCommand("after a second C", Command{Integrated: true, Running: true, hasFrom: true})

	h.write("\x1b]133;D;0\x07")
	h.wantCommand("after the one D", Command{Integrated: true, hasStatus: true, Done: 1, hasFrom: true})
	if len(h.ends) != 1 {
		t.Fatalf("CommandDone fired %+v for two Cs and one D, want one finish", h.ends)
	}
}

// A second D for a command that already finished is dropped.
func TestSemanticPromptSecondDone(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;0\x07\x1b]133;D;1\x07")
	h.wantCommand("after a second D", Command{Integrated: true, hasStatus: true, Done: 1, hasFrom: true})
	if len(h.ends) != 1 {
		t.Fatalf("CommandDone fired %+v for a second D, want one finish", h.ends)
	}
}

// PowerShell reports a native crash as a negative $LASTEXITCODE, such
// as -1073741819 for an access violation.
func TestSemanticPromptNegativeStatus(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;-1073741819\x07")
	h.wantCommand("after an access violation", Command{
		Integrated: true, hasStatus: true, status: -1073741819, Done: 1, hasFrom: true})
}

// RIS resets the screen and leaves the command state alone: the command
// that sent it is still running.
func TestSemanticPromptReset(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1bc")
	h.wantCommand("after RIS", Command{Integrated: true, Running: true, hasFrom: true})

	h.write("\x1b]133;D;0\x07")
	h.wantCommand("after its D",
		Command{Integrated: true, hasStatus: true, Done: 1, hasFrom: true})
}

// A full-screen program killed without restoring the ordinary screen
// leaves Running true for ever: its D was dropped on the alternate
// screen, and nothing later says the command ended.
func TestSemanticPromptFrozenByAFullScreenProgram(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07")
	h.write("\x1b[?1049h")
	h.write("\x1b]133;D;0\x07")
	h.write("\x1b[?1049l")
	h.wantCommand("after a program that never restored the screen",
		Command{Integrated: true, Running: true, hasFrom: true})
	if len(h.ends) != 0 {
		t.Fatalf("CommandDone fired %+v for a D on the alternate screen", h.ends)
	}
}

// A caller reading the command state from inside the callback sees the
// finish that fired it, Done included.
func TestSemanticPromptCallbackSeesTheFinish(t *testing.T) {
	var term *Terminal
	var seen []Command
	term = New(20, 4, DefaultPalette(), 100, Callbacks{
		CommandDone: func(int, bool) { seen = append(seen, term.Command()) },
	})
	if _, err := term.Write([]byte("\x1b]133;C\x07\x1b]133;D;5\x07")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := []Command{{Integrated: true, hasStatus: true, status: 5, Done: 1, hasFrom: true}}
	if len(seen) != len(want) {
		t.Fatalf("CommandDone fired %d times (%+v), want %d", len(seen), seen, len(want))
	}
	if seen[0] != want[0] {
		t.Errorf("Command() inside the callback = %+v, want %+v", seen[0], want[0])
	}
}

// VS Code's shell integration sends OSC 633, whose A, B, C and D marks
// have the grammar OSC 133 gives them.
func TestSemanticPrompt633WholeCommand(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]633;A\x07$ ")
	h.wantCommand("after A", Command{Integrated: true})

	h.write("\x1b]633;P;Cwd=C:\\\\Users\\\\marcus\x07")
	h.wantCommand("after a property", Command{Integrated: true})

	h.write("\x1b]633;B\x07")
	h.wantCommand("after B", Command{Integrated: true})

	h.write("\x1b]633;E;ls -l;1234\x07")
	h.wantCommand("after the command line", Command{Integrated: true})

	h.write("\x1b]633;C\x07one\r\n")
	h.wantCommand("after C", Command{Integrated: true, Running: true, hasFrom: true})

	h.write("\x1b]633;D;-1073741819\x07")
	h.wantCommand("after D", Command{
		Integrated: true, hasStatus: true, status: -1073741819, Done: 1, hasFrom: true})
}

// The marks 633 has and 133 does not are ignored, and none of them says
// the shell is integrated.
func TestSemanticPrompt633UnreadMarks(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]633;E;ls -l;1234\x07")
	h.write("\x1b]633;P;IsWindows=True\x07")
	h.write("\x1b]633;F\x07\x1b]633;G\x07")
	h.write("\x1b]633;EnvJson;{};1234\x07")
	h.write("\x1b]633;EnvSingleStart;0;1234\x07")
	h.write("\x1b]633;EnvSingleEntry;PATH;C:\\\\Windows;1234\x07")
	h.write("\x1b]633;EnvSingleEnd;1234\x07")
	h.wantCommand("after marks nothing here reads", Command{})
}

// A bare D with no status is how VS Code's PowerShell integration ends
// ctrl-C and Enter on an empty line.
func TestSemanticPrompt633BareDone(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]633;C\x07\x1b]633;D\x07")
	h.wantCommand("after a bare D", Command{Integrated: true, Done: 1, hasFrom: true})
}

func TestSemanticPrompt633MixedWith133(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;A\x07\x1b]633;B\x07\x1b]133;C\x07")
	h.wantCommand("after a command started with marks of both kinds",
		Command{Integrated: true, Running: true, hasFrom: true})

	h.write("\x1b]633;D;3\x07")
	h.wantCommand("after a 633 D finished a 133 C",
		Command{Integrated: true, hasStatus: true, status: 3, Done: 1, hasFrom: true})
}

// Exit is the only way to read the status, so no caller can mistake a
// command that reported nothing for one that succeeded.
func TestCommandExit(t *testing.T) {
	h := newHarness(t, 20, 4)
	if status, ok := h.term.Command().Exit(); ok || status != 0 {
		t.Errorf("Exit() = %d, %v before any mark, want 0, false", status, ok)
	}

	h.write("\x1b]133;C\x07\x1b]133;D\x07")
	if status, ok := h.term.Command().Exit(); ok || status != 0 {
		t.Errorf("Exit() = %d, %v after a bare D, want 0, false", status, ok)
	}

	h.write("\x1b]133;C\x07\x1b]133;D;1\x07")
	if status, ok := h.term.Command().Exit(); !ok || status != 1 {
		t.Errorf("Exit() = %d, %v after D;1, want 1, true", status, ok)
	}

	h.write("\x1b]133;C\x07\x1b]133;A\x07")
	if status, ok := h.term.Command().Exit(); ok || status != 0 {
		t.Errorf("Exit() = %d, %v after a prompt ended a command, want 0, false", status, ok)
	}
}

// The line a command's output began on is remembered, and goes on naming
// the same line as the screen scrolls under it.
//
// It is what lets a caller read what one command printed rather than a
// rectangle of the screen with the end of the last command still in it.
func TestSemanticPromptRemembersWhereTheOutputBegan(t *testing.T) {
	h := newHarness(t, 20, 3)
	h.write("\x1b]133;A\x07$ \x1b]133;B\x07ls\r\n\x1b]133;C\x07")
	from, ok := h.term.Command().Output()
	if !ok || from != 1 {
		t.Fatalf("the output begins on line %d, known %v; want line 1", from, ok)
	}

	// Enough output to push that line off the top of the screen. The
	// number stands still: it names the line, not a row.
	h.write("one\r\ntwo\r\nthree\r\nfour\r\n")
	if got, ok := h.term.Command().Output(); !ok || got != from {
		t.Errorf("after scrolling the output begins on line %d, want %d", got, from)
	}

	// The next command moves it on.
	h.write("\x1b]133;D;0\x07\x1b]133;A\x07$ \x1b]133;B\x07df\r\n\x1b]133;C\x07")
	next, ok := h.term.Command().Output()
	if !ok || next <= from {
		t.Errorf("the second command's output begins on line %d, and the first on %d",
			next, from)
	}
}

// The boundary is a line of the output, not a row of the screen.
//
// Recorded on a screen that has already scrolled, it is the line's own
// number: rows start again at the top and lines do not. A boundary kept
// as a row would name the wrong text the moment anything scrolled, which
// is every command that prints more than a screenful.
func TestTheOutputBoundaryIsALineAndNotARow(t *testing.T) {
	h := newHarness(t, 20, 3)
	// Five lines through a three row screen, so three have gone off the
	// top and the cursor is on the bottom row.
	h.write("one\r\ntwo\r\nthree\r\nfour\r\n$ ls\r\n")
	scr := h.term.Screen()
	_, row := scr.CursorPos()
	if scr.LineNumber(0) == 0 {
		t.Fatal("the screen has not scrolled, so this proves nothing")
	}

	h.write("\x1b]133;C\x07")
	from, ok := h.term.Command().Output()
	if !ok {
		t.Fatal("the shell's mark recorded no boundary")
	}
	if want := scr.LineNumber(row); from != want {
		t.Errorf("the output begins on line %d, want %d; the cursor is on row %d",
			from, want, row)
	}
	if from == uint64(row) {
		t.Errorf("the boundary is %d, which is the row rather than the line", from)
	}
}

// A shell that marks nothing has no boundary to give.
func TestASilentShellMarksNoOutput(t *testing.T) {
	h := newHarness(t, 20, 3)
	h.write("$ ls\r\none\r\n$ ")
	if from, ok := h.term.Command().Output(); ok {
		t.Errorf("a silent shell says its output began on line %d", from)
	}
}
