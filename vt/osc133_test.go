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
	h.wantCommand("after D", Command{Integrated: true, HasStatus: true, Status: 0, Done: 1})
}

func TestSemanticPromptNonZeroStatus(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;127\x07")
	h.wantCommand("after D;127", Command{Integrated: true, HasStatus: true, Status: 127, Done: 1})
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
	h.wantCommand("after D;2;aid=12345", Command{Integrated: true, HasStatus: true, Status: 2, Done: 1})
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
	h.wantCommand("after marks ended with ST", Command{Integrated: true, HasStatus: true, Status: 3, Done: 1})

	h = newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;D;3\x07")
	h.wantCommand("after marks ended with BEL", Command{Integrated: true, HasStatus: true, Status: 3, Done: 1})
}

// A shell that has never sent a mark must not look like one that just
// reported success.
func TestSemanticPromptSilentShell(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("$ ls\r\none\r\n$ ")
	got := h.term.Command()
	h.wantCommand("after a shell that says nothing", Command{})
	if got.HasStatus {
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
	h.wantCommand("after its D", Command{Integrated: true, HasStatus: true, Done: 1})
}

// A prompt means the shell is not running a command, even from a shell
// that never sends D.
func TestSemanticPromptPromptEndsARunningCommand(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;C\x07\x1b]133;A\x07")
	h.wantCommand("after a prompt with no D", Command{Integrated: true})
}

func TestSemanticPromptTwoCommands(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("\x1b]133;A\x07\x1b]133;B\x07\x1b]133;C\x07\x1b]133;D;0\x07")
	first := h.term.Command()
	h.wantCommand("after the first command", Command{Integrated: true, HasStatus: true, Done: 1})

	h.write("\x1b]133;A\x07\x1b]133;B\x07\x1b]133;C\x07\x1b]133;D;1\x07")
	second := h.term.Command()
	h.wantCommand("after the second command", Command{Integrated: true, HasStatus: true, Status: 1, Done: 2})
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
	want := Command{Integrated: true, HasStatus: true, Done: 1}
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
	h.wantCommand("after the program exits", Command{Integrated: true, HasStatus: true, Done: 1})
}
