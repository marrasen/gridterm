package main

import (
	"os/exec"
	"slices"
	"testing"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	shellfind "github.com/marrasen/kakel/shells"
)

// A new terminal, and a split, run the shell the pane in front runs,
// rather than the default one.
func TestANewTerminalRunsTheShellOfThePaneInFront(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh here")
	}
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.ctx = t.Context()
	t.Cleanup(func() {
		for len(a.st.Panes) > 0 {
			a.remove(a.st.Panes[0].ID)
		}
	})
	a.found = []shellfind.Shell{{ID: "sh", Title: "Plain Shell", Path: sh}}
	a.nextShell = []string{sh}
	if err := a.openTerminal(); err != nil {
		t.Fatal(err)
	}
	first := a.st.Focus
	if err := a.openTerminal(); err != nil {
		t.Fatal(err)
	}
	if got := a.argvs[a.st.Focus]; !slices.Equal(got, []string{sh}) {
		t.Fatalf("a new terminal from a pane running %s runs %v", sh, got)
	}
	a.focus(first)
	if err := a.split(SplitPane{}); err != nil {
		t.Fatal(err)
	}
	if got := a.argvs[a.st.Focus]; !slices.Equal(got, []string{sh}) {
		t.Fatalf("a split of a pane running %s runs %v", sh, got)
	}
}

// A shell that names itself by its program's path, as the Command
// Prompt does, shows the shell's name instead.
func TestAShellNamingItselfByItsPathShowsItsName(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	cmd := `C:\Windows\System32\cmd.exe`
	a.found = []shellfind.Shell{{ID: "cmd", Title: "Command Prompt", Path: cmd}}
	a.addPane(Pane{ID: "p1", Title: "Terminal 1"}, nil, placement{})
	a.argvs["p1"] = []string{cmd}
	for said, want := range map[string]string{
		`C:\WINDOWS\system32\cmd.exe`:          "Command Prompt",
		`C:\WINDOWS\system32\cmd.exe - ping x`: "Command Prompt - ping x",
		"vim notes.txt":                        "vim notes.txt",
	} {
		a.retitle("p1", said)
		if got := a.titleOf("p1"); got != want {
			t.Errorf("titled %q, the pane is called %q, want %q", said, got, want)
		}
	}
}
