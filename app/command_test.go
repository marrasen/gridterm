package app

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/session"
	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
)

func TestACommandRunsInAPaneOfItsOwnAndAgain(t *testing.T) {
	a, _ := agentApp(t)
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	line := echoCommand("command-ran")
	a.handle(RunCommand{Line: line, Keep: true})
	if len(a.st.Panes) != 2 || !a.st.Panes[1].Command || a.st.Panes[1].Title != line {
		t.Fatalf("run, the panes are %+v; notices %+v", a.st.Panes, a.st.Notices)
	}
	if len(a.st.SavedCommands) != 1 || a.st.SavedCommands[0].Line != line {
		t.Fatalf("kept, the commands are %+v", a.st.SavedCommands)
	}
	id := a.st.Panes[1].ID
	question := line + " finished. Exit 0. Run it again?"
	waitFor(t, a, "the question", func() bool { return a.terminal(id).Asking() == question })
	if err := a.startAgain(id); err != nil {
		t.Fatal(err)
	}
	// Run again, the pane asks nothing until the command has ended
	// again, so the question coming back is the second run's end.
	// Counting the output twice failed on Windows, where the first
	// run's output can be gone from the screen by then.
	if got := a.terminal(id).Asking(); got != "" {
		t.Fatalf("run again, the pane still asks %q", got)
	}
	waitFor(t, a, "it to run again", func() bool {
		return a.terminal(id).Asking() == question && strings.Contains(a.terminal(id).Text(), "command-ran")
	})
	a.handle(RunSavedCommand{Saved: a.st.SavedCommands[0]})
	if len(a.st.Panes) != 3 || a.st.Panes[2].Title != line {
		t.Fatalf("run from the saved list, the panes are %+v", a.st.Panes)
	}
}

func TestAShellCanBeKeptForNewTerminals(t *testing.T) {
	was := findShells
	plain, echoer := plainShell(), shellSaying("i-am-the-echoer")
	findShells = func() ([]shellfind.Shell, error) {
		return []shellfind.Shell{
			{ID: "login", Title: "Plain", Path: plain[0]},
			{ID: "echoer", Title: "Echoer", Path: echoer[0], Args: echoer[1:]},
		}, nil
	}
	t.Cleanup(func() { findShells = was })
	a, _ := agentApp(t)
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	a.scanShells()
	waitFor(t, a, "the shells", func() bool { return len(a.st.Shells) == 2 })
	a.handle(OpenShellNamed{ID: "echoer"})
	if len(a.st.Panes) != 2 {
		t.Fatalf("opened, the panes are %+v; notices %+v", a.st.Panes, a.st.Notices)
	}
	waitFor(t, a, "the echoer", func() bool { return strings.Contains(a.terminal(a.st.Panes[1].ID).Text(), "i-am-the-echoer") })
	a.handle(PickShell{ID: "echoer"})
	if id, ok := set.Shell(); !ok || id != "echoer" || a.st.ChosenShell != "echoer" {
		t.Fatalf("kept, the settings say %q, %v", id, ok)
	}
	a.handle(NewTerminal{})
	if len(a.st.Panes) != 3 {
		t.Fatalf("a new terminal, the panes are %+v; notices %+v", a.st.Panes, a.st.Notices)
	}
	waitFor(t, a, "the echoer again", func() bool { return strings.Contains(a.terminal(a.st.Panes[2].ID).Text(), "i-am-the-echoer") })
	a.handle(PickShell{})
	if _, ok := set.Shell(); ok {
		t.Fatal("back to the default, a shell is still kept")
	}
}

func TestASavedCommandPickedAndUntickedIsForgotten(t *testing.T) {
	a, _ := agentApp(t)
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	a.handle(RunCommand{Line: "echo kept", Keep: true})
	a.handle(RunCommand{Line: "echo kept", Forget: "echo kept"})
	if len(a.st.SavedCommands) != 0 {
		t.Fatalf("unticked, the commands are %+v", a.st.SavedCommands)
	}
}

func TestThingsSavedBeforeServersHadIDsAreGivenThem(t *testing.T) {
	a, _ := agentApp(t)
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	a.settings = set
	if err := set.KeepCommand(settings.SavedCommand{Line: "top", Host: "desk"}, mostSavedCommands); err != nil {
		t.Fatal(err)
	}
	book, err := remote.LoadBook(filepath.Join(t.TempDir(), "servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Put(remote.Host{Name: "desk", Address: "desk.example"}, ""); err != nil {
		t.Fatal(err)
	}
	a.book = book
	a.giveSavedIDs()
	h, _ := book.Lookup("desk")
	if got := set.Commands()[0].HostID; got == "" || got != h.ID {
		t.Fatalf("the command is on server id %q, want %q", got, h.ID)
	}
}

// A command on a saved server that is not connected connects first,
// then runs.
func TestACommandOnAServerNotConnectedConnectsFirst(t *testing.T) {
	a, answering := dialApp(t)
	if err := a.runCommand(RunCommand{Machine: "srv", Line: "echo hi"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the command's pane", func() bool {
		answering()
		return slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return p.Machine == "srv" && p.Command })
	})
	if a.machines.Get("srv").Conn == nil {
		t.Fatal("the command ran with no connection to srv")
	}
}

// A command on a machine that is neither saved nor a quick connection
// is refused at once, rather than dialled as an address.
func TestACommandOnAMachineNotKnownIsRefused(t *testing.T) {
	a, _ := dialApp(t)
	err := a.startCommand("web:22", command{argv: []string{"true"}}, commandStart{
		then: func(session.Session) { t.Fatal("the command started") },
	})
	if err == nil || len(a.machines.Dialing()) != 0 {
		t.Fatalf("a command on a machine not known said %v, and dialled %v", err, a.machines.Dialing())
	}
}

// A command whose connection could not be made says so to whoever
// started it, as Run Again asks again then.
func TestACommandWhoseConnectionFailsSaysSo(t *testing.T) {
	a, answering := dialApp(t)
	failed := false
	nowhere := a.machines.NewQuick("tester@127.0.0.1:1", false)
	err := a.startCommand(nowhere, command{argv: []string{"true"}}, commandStart{
		then:   func(session.Session) { t.Fatal("the command started") },
		failed: func() { failed = true },
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the failure", func() bool { answering(); return failed })
}

// A saved command whose server was removed is not run: its name may be
// another machine's now.
func TestASavedCommandOnARemovedServerIsNotRun(t *testing.T) {
	a, _ := dialApp(t)
	err := a.runSavedCommand(settings.SavedCommand{Line: "uptime", Host: "gone", HostID: "no-such-id"})
	if err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("running a command on a removed server said %v", err)
	}
	if len(a.machines.Dialing()) != 0 {
		t.Fatalf("it dialled %v", a.machines.Dialing())
	}
}

// A kept shell no longer on this machine opens the default shell, and
// says so once a run.
func TestAKeptShellThatHasGoneIsSaidOnce(t *testing.T) {
	a := fontApp(t)
	set, err := settings.Load(t.TempDir() + "/settings.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := set.PutShell("wsl:Gone"); err != nil {
		t.Fatal(err)
	}
	a.settings = set
	a.scanned = true
	for range 2 {
		if argv := a.localShell(); argv != nil {
			t.Fatalf("with the kept shell gone, a terminal starts %q", argv)
		}
	}
	if len(a.st.Notices) != 1 || a.st.Notices[0].Title != "Shell not found" || !strings.HasPrefix(a.st.Notices[0].Body, "Gone is no longer installed.") {
		t.Fatalf("the notices are %+v", a.st.Notices)
	}
}

// A command asked to be saved that could not be is not run: running it
// anyway would leave the user thinking it had been saved.
func TestACommandThatCannotBeKeptIsNotRun(t *testing.T) {
	a := fontApp(t)
	a.settings = settings.Unusable(errors.New("the settings file is unreadable"))
	err := a.runCommand(RunCommand{Line: "make deploy", Keep: true})
	if err == nil || !strings.Contains(err.Error(), "not run") {
		t.Fatalf("it said %v", err)
	}
	if len(a.st.Panes) != 0 {
		t.Fatalf("the command ran: %+v", a.st.Panes)
	}
}
