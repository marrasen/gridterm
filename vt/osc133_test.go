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

	h.write("\x1b]133;C\x07one\r\n")
	h.wantCommand("after C", Command{Integrated: true, Running: true})

	h.write("\x1b]133;D;0\x07")
	h.wantCommand("after D", Command{Integrated: true, hasStatus: true, status: 0, Done: 1})
}

func TestSemanticPromptNonZeroStatus(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;127\x07")
	h.wantCommand("after D;127", Command{Integrated: true, hasStatus: true, status: 127, Done: 1})
}

// A shell may end a command without saying how it went, and that is a
// different answer from status 0.
func TestSemanticPromptNoStatus(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D\x07")
	h.wantCommand("after a bare D", Command{Integrated: true, Done: 1})
}

func TestSemanticPromptStatusIsNotANumber(t *testing.T) {
	for _, status := range []string{"", "ok", "SIGINT", "0x7f"} {
		h := newHarness(t, 20, 4)
		h.write("\x1b]133;C\x07\x1b]133;D;" + status + "\x07")
		h.wantCommand("after D;"+status, Command{Integrated: true, Done: 1})
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
	h.wantCommand("after C with an empty option", Command{Integrated: true, Running: true})

	h.write("\x1b]133;D;2;aid=12345\x07")
	h.wantCommand("after D;2;aid=12345", Command{Integrated: true, hasStatus: true, status: 2, Done: 1})
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
	h.wantCommand("after marks ended with ST", Command{Integrated: true, hasStatus: true, status: 3, Done: 1})

	h = newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;3\x07")
	h.wantCommand("after marks ended with BEL", Command{Integrated: true, hasStatus: true, status: 3, Done: 1})
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
	h.wantCommand("after a C with no A", Command{Integrated: true, Running: true})

	h.write("\x1b]133;D;0\x07")
	h.wantCommand("after its D", Command{Integrated: true, hasStatus: true, Done: 1})
}

// A prompt means the shell is not running a command, even from a shell
// that never sends D. The status the prompt clears belongs to the
// command before it.
func TestSemanticPromptPromptEndsARunningCommand(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;0\x07")
	h.wantCommand("after a command that succeeded", Command{Integrated: true, hasStatus: true, Done: 1})

	h.write("\x1b]133;A\x07\x1b]133;B\x07\x1b]133;C\x07")
	h.wantCommand("after a second command started", Command{Integrated: true, Running: true, hasStatus: true, Done: 1})

	h.write("\x1b]133;A\x07")
	h.wantCommand("after a prompt with no D", Command{Integrated: true, Done: 1})
}

func TestSemanticPromptTwoCommands(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;A\x07\x1b]133;B\x07\x1b]133;C\x07\x1b]133;D;0\x07")
	first := h.term.Command()
	h.wantCommand("after the first command", Command{Integrated: true, hasStatus: true, Done: 1})

	h.write("\x1b]133;A\x07\x1b]133;B\x07\x1b]133;C\x07\x1b]133;D;1\x07")
	second := h.term.Command()
	h.wantCommand("after the second command", Command{Integrated: true, hasStatus: true, status: 1, Done: 2})
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
	want := Command{Integrated: true, hasStatus: true, Done: 1}
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
	h.wantCommand("while the program draws", Command{Integrated: true, Running: true})

	h.write("\x1b[?1049l")
	h.write("\x1b]133;D;0\x07")
	h.wantCommand("after the program exits", Command{Integrated: true, hasStatus: true, Done: 1})
}

func TestSemanticPromptTwoStartsOneFinish(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;C\x07")
	h.wantCommand("after a second C", Command{Integrated: true, Running: true})

	h.write("\x1b]133;D;0\x07")
	h.wantCommand("after the one D", Command{Integrated: true, hasStatus: true, Done: 1})
	if len(h.ends) != 1 {
		t.Fatalf("CommandDone fired %+v for two Cs and one D, want one finish", h.ends)
	}
}

// A second D for a command that already finished is dropped.
func TestSemanticPromptSecondDone(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;0\x07\x1b]133;D;1\x07")
	h.wantCommand("after a second D", Command{Integrated: true, hasStatus: true, Done: 1})
	if len(h.ends) != 1 {
		t.Fatalf("CommandDone fired %+v for a second D, want one finish", h.ends)
	}
}

// PowerShell reports a native crash as a negative $LASTEXITCODE, such
// as -1073741819 for an access violation.
func TestSemanticPromptNegativeStatus(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;-1073741819\x07")
	h.wantCommand("after an access violation", Command{Integrated: true, hasStatus: true, status: -1073741819, Done: 1})
}

// RIS resets the screen and leaves the command state alone: the command
// that sent it is still running.
func TestSemanticPromptReset(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1bc")
	h.wantCommand("after RIS", Command{Integrated: true, Running: true})

	h.write("\x1b]133;D;0\x07")
	h.wantCommand("after its D", Command{Integrated: true, hasStatus: true, Done: 1})
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
	h.wantCommand("after a program that never restored the screen", Command{Integrated: true, Running: true})
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
	want := []Command{{Integrated: true, hasStatus: true, status: 5, Done: 1}}
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
	h.wantCommand("after C", Command{Integrated: true, Running: true})

	h.write("\x1b]633;D;-1073741819\x07")
	h.wantCommand("after D", Command{Integrated: true, hasStatus: true, status: -1073741819, Done: 1})
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
	h.wantCommand("after a bare D", Command{Integrated: true, Done: 1})
}

func TestSemanticPrompt633MixedWith133(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;A\x07\x1b]633;B\x07\x1b]133;C\x07")
	h.wantCommand("after a command started with marks of both kinds", Command{Integrated: true, Running: true})

	h.write("\x1b]633;D;3\x07")
	h.wantCommand("after a 633 D finished a 133 C", Command{Integrated: true, hasStatus: true, status: 3, Done: 1})
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
