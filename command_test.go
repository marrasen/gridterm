package main

import (
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
	if a.conns["srv"] == nil {
		t.Fatal("the command ran with no connection to srv")
	}
}

// A command on a machine whose connection is kept under another name,
// as "web:22" is kept as "web", says so rather than connecting again
// and again.
func TestACommandConnectedUnderAnotherNameSaysSo(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	a.conns["web"] = a.conns["srv"]
	t.Cleanup(func() { delete(a.conns, "web") })
	failed := false
	err := a.startCommand("web:22", command{argv: []string{"true"}}, commandStart{
		then:   func(session.Session) { t.Fatal("the command started") },
		failed: func() { failed = true },
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the refusal", func() bool { return failed })
}

// A command whose connection could not be made says so to whoever
// started it, as Run Again asks again then.
func TestACommandWhoseConnectionFailsSaysSo(t *testing.T) {
	a, answering := dialApp(t)
	failed := false
	err := a.startCommand("127.0.0.1:1", command{argv: []string{"true"}}, commandStart{
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
	if len(a.dialing) != 0 {
		t.Fatalf("it dialled %v", a.dialing)
	}
}

// A command on a saved kakel window is refused before connecting.
func TestACommandOnASavedWindowIsRefused(t *testing.T) {
	a, _ := dialApp(t)
	if err := a.book.Put(remote.Host{Name: "box", Address: "127.0.0.1", Port: 1, Window: true}, ""); err != nil {
		t.Fatal(err)
	}
	err := a.runCommand(RunCommand{Machine: "box", Line: "uptime"})
	if err == nil || !strings.Contains(err.Error(), "kakel window") {
		t.Fatalf("a command on a saved window said %v", err)
	}
	if len(a.dialing) != 0 {
		t.Fatalf("it dialled %v", a.dialing)
	}
}
