package main

import (
	"strings"
	"testing"
	"time"

	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/vfs"
)

// The count of Serve's tries goes on as what is served is shown again,
// so a window that tried again hears of its own try.
func TestServesTriesAreNotCountedAgainFromNothing(t *testing.T) {
	a, _ := agentApp(t)
	a.st.Serving.Tries = 3
	a.showServing()
	if a.st.Serving.Tries != 3 {
		t.Fatalf("shown again, the tries are %d, want 3", a.st.Serving.Tries)
	}
}

// Another window asking for a command to run again hears at once that
// it did, however soon the command is over.
func TestStartAgainForAnotherWindowAnswersOnceItRan(t *testing.T) {
	a, _ := agentApp(t)
	if err := a.runCommand(RunCommand{Line: "true"}); err != nil {
		t.Fatal(err)
	}
	id := a.st.Focus
	waitFor(t, a, "the command to end", func() bool { return a.terminal(id).Exited() && a.endings[id] > 0 })
	done := make(chan error, 1)
	go func() { done <- a.startAgainFor(remoteAttached(id)) }()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case f := <-a.events:
			f()
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return
		case <-deadline:
			t.Fatal("the answer took ten seconds")
		}
	}
}

// A Go To in a file pane made again, as moved to another window, is
// numbered past the ones the program has answered.
func TestGoToIsNumberedPastTheOnesAnswered(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(State{Panes: []Pane{{ID: "p1", Title: "one", Kind: kindFiles}}, Stage: &Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]Browser{"p1": {Path: "/", Seq: 1, WentTo: 3}}})
	win.browsers["p1"].askGoToWith("/x", "", lastUI)
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	for {
		if in, ok := nextIntent(t).(GoTo); ok {
			if in.Ask <= 3 {
				t.Fatalf("Go To is numbered %d, past none of the 3 answered", in.Ask)
			}
			return
		}
	}
}

// Files opened on a server before it was saved otherwise are not used
// after, and a shell there that ended asks again when starting it again
// is refused.
func TestAServerSavedOtherwiseRefusesItsFilesAndAsksAgain(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	opened := false
	if err := a.withFiles("srv", func(vfs.FS) { opened = true }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the files", func() bool { return opened })
	id := a.st.Panes[0].ID
	a.shells.get(id).close() // its shell ends; the connection stays
	waitFor(t, a, "the shell to end", func() bool { return a.terminal(id).Exited() })
	h, _ := a.book.Lookup("srv")
	h.Address, h.Port = "127.0.0.1", 1
	if err := a.saveServer(SaveServer{Host: h, Under: "srv"}); err != nil {
		t.Fatal(err)
	}
	if err := a.withFiles("srv", func(vfs.FS) { t.Fatal("the files as they were were used") }); err == nil || !strings.Contains(err.Error(), "Disconnect it first") {
		t.Fatalf("its files said %v", err)
	}
	ends := a.endings[id]
	if err := a.startAgain(id); err == nil {
		t.Fatal("started again on the server as it was")
	}
	if a.endings[id] <= ends {
		t.Fatal("refused, the pane did not ask again")
	}
}

// remoteAttached is what another window asks by, for pane id here.
func remoteAttached(id string) serve.Attached { return serve.Attached{ID: id} }
