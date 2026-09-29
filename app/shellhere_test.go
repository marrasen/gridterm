package app

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/kakel/screen"

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
	a := newApp(w.Client(), screen.NewShells())
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
	a := newApp(w.Client(), screen.NewShells())
	cmd := `C:\Windows\System32\cmd.exe`
	a.found = []shellfind.Shell{{ID: "cmd", Title: "Command Prompt", Path: cmd}}
	a.addPane(Pane{ID: "p1", Title: "Terminal 1"}, nil, Placement{})
	a.argvs["p1"] = []string{cmd}
	for said, want := range map[string]string{
		`C:\WINDOWS\system32\cmd.exe`:                "Command Prompt",
		`C:\WINDOWS\system32\cmd.exe - ping x`:       "Command Prompt - ping x",
		"vim notes.txt":                              "vim notes.txt",
		`Administrator: C:\WINDOWS\system32\cmd.exe`: "Administrator: Command Prompt",
	} {
		a.retitle("p1", said)
		if got := a.titleOf("p1"); got != want {
			t.Errorf("titled %q, the pane is called %q, want %q", said, got, want)
		}
	}
}

// A pane on a server is named as its program says: the path of a shell
// here is nothing to it.
func TestAServersPaneKeepsTheTitleItsProgramGives(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	cmd := `C:\Windows\System32\cmd.exe`
	a.found = []shellfind.Shell{{ID: "cmd", Title: "Command Prompt", Path: cmd}}
	a.addPane(Pane{ID: "p1", Title: "Terminal 1", Machine: "srv"}, nil, Placement{})
	a.retitle("p1", cmd)
	if got := a.titleOf("p1"); got != cmd {
		t.Fatalf("a pane on a server is called %q", got)
	}
}

// What a pane runs is kept without the folder a WSL shell was started
// in, so starting it again goes to where it is now, not back there.
func TestAPaneKeepsItsShellWithoutTheFolderItStartedIn(t *testing.T) {
	got := withoutFolder([]string{"wsl.exe", "-d", "Ubuntu", "--cd", "/home/x"})
	if !slices.Equal(got, []string{"wsl.exe", "-d", "Ubuntu"}) {
		t.Fatalf("kept %v", got)
	}
	if got := withoutFolder([]string{"cmd.exe"}); !slices.Equal(got, []string{"cmd.exe"}) {
		t.Fatalf("kept %v", got)
	}
}

// A shell that names this computer with its domain, or without, is on
// this computer.
func TestThisMachineIsKnownWithOrWithoutItsDomain(t *testing.T) {
	name, err := os.Hostname()
	if err != nil {
		t.Skip(err)
	}
	short, _, _ := strings.Cut(name, ".")
	for host, want := range map[string]bool{
		"": true, "localhost": true, short: true, short + ".example.org": true, strings.ToUpper(short): true, "not-" + short: false, "10.0.0.1": false,
	} {
		if got := isThisMachine(host); got != want {
			t.Errorf("%q is this machine: %v, want %v", host, got, want)
		}
	}
}
