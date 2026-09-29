package app

import (
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
)

func TestEachLogHasOnePane(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	sh := screen.NewShells()
	a := newApp(w.Client(), sh)
	a.ctx = t.Context()
	t.Cleanup(func() {
		for _, p := range a.st.Panes {
			a.remove(p.ID)
		}
	})
	a.handle(ShowLog{})
	a.handle(ShowLog{})
	if len(a.st.Panes) != 1 || a.st.Panes[0].Kind != KindLog || a.st.Panes[0].Title != "Window Log" {
		t.Fatalf("asked twice for the window log, the panes are %+v", a.st.Panes)
	}
	a.handle(ShowLog{Machine: "srv"})
	if len(a.st.Panes) != 1 || len(a.st.Notices) != 1 || !strings.Contains(a.st.Notices[0].Body, "srv") {
		t.Fatalf("with no connection log for srv, the panes are %+v and the notices %+v", a.st.Panes, a.st.Notices)
	}
	logLine(a.account("srv"), badly, "no route")
	a.handle(ShowLog{Machine: "srv"})
	if len(a.st.Panes) != 2 || a.st.Panes[1].Machine != "srv" || a.st.Focus != a.st.Panes[1].ID {
		t.Fatalf("srv's log opened as %+v, focus on %q", a.st.Panes, a.st.Focus)
	}
	a.handle(FocusPane{Pane: a.st.Panes[0].ID})
	a.handle(ShowLog{Machine: "srv"})
	if len(a.st.Panes) != 2 || a.st.Focus != a.st.Panes[1].ID {
		t.Fatalf("asked again, srv's log is %+v, focus on %q", a.st.Panes, a.st.Focus)
	}
}

// What the window says in a notice is in the Window Log too.
func TestANoticeIsInTheWindowLog(t *testing.T) {
	a, _ := agentApp(t)
	// The window's log is where the log package writes, as main sets.
	var got strings.Builder
	log.SetOutput(&got)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	a.notify("That didn't work", "start the shell: the directory name is invalid", "")
	if !strings.Contains(got.String(), "That didn't work: start the shell: the directory name is invalid") {
		t.Fatalf("the log says %q", got.String())
	}
}

// What a server said is written into the log clean, a line at a time,
// and looking at a log does not say it is connecting again.
func TestALogLineIsCleanAndAskingForTheLogSaysNothing(t *testing.T) {
	a := newApp(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), screen.NewShells())
	l := a.account("srv")
	logLine(l, badly, "disconnected: \x1b]0;owned\x07bye\r\nsee you\n")
	a.account("srv")
	logLine(a.dialLog("srv"), "", "connecting to srv")
	var got []string
	r := l.Open()
	defer r.Close()
	buf := make([]byte, 4096)
	for len(got) < 4 {
		n, err := r.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimRight(string(buf[:n]), "\n"), "\n") {
			got = append(got, strings.TrimSuffix(line[len("15:04:05  "):], "\r"))
		}
	}
	want := []string{"\x1b[" + badly + "mdisconnected: ]0;ownedbye\x1b[0m", "\x1b[" + badly + "msee you\x1b[0m", "connecting again", "connecting to srv"}
	if !slices.Equal(got, want) {
		t.Fatalf("the log reads %q, want %q", got, want)
	}
}

// A log's pane is searched as a terminal's is: Find in Scrollback opens
// what it holds in a reader.
func TestALogIsSearchedLikeATerminal(t *testing.T) {
	a, _ := agentApp(t)
	logLine(a.account("srv"), badly, "no route to srv")
	a.handle(ShowLog{Machine: "srv"})
	id := a.st.Panes[len(a.st.Panes)-1].ID
	waitFor(t, a, "the log line", func() bool { return strings.Contains(a.shells.Get(id).T.AllText(), "no route to srv") })
	a.handle(ShowScrollback{Pane: id})
	p := a.st.Panes[len(a.st.Panes)-1]
	if p.Kind != KindReader || !slices.ContainsFunc(a.st.Readers[p.ID].Lines, func(l string) bool { return strings.Contains(l, "no route to srv") }) {
		t.Fatalf("Find in Scrollback on the log opened %+v, holding %q", p, a.st.Readers[p.ID].Lines)
	}
}

// The Window Log tells what happened on a good day too: panes opened
// and closed, and how a shell ended.
func TestTheWindowLogTellsOfPanes(t *testing.T) {
	var got strings.Builder
	log.SetOutput(&got)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	a, _ := agentApp(t)
	id := a.st.Panes[0].ID
	title := a.st.Panes[0].Title
	a.closePane(id)
	waitFor(t, a, "the pane to close", func() bool { return !a.has(id) })
	for _, want := range []string{"opened " + strconv.Quote(title), "closed " + strconv.Quote(title)} {
		if !strings.Contains(got.String(), want) {
			t.Fatalf("the log says %q, with no %q", got.String(), want)
		}
	}
}

// A line is cleaned on its way into the Window Log of what a terminal
// would act on, as it may carry what a far end said.
func TestTheWindowLogIsCleaned(t *testing.T) {
	var got strings.Builder
	l := log.New(plainLog{&got}, "", 0)
	l.Printf("closed %s", "\x1b]0;owned\x07bye\x1b[2J")
	if got.String() != "closed ]0;ownedbye[2J\n" {
		t.Fatalf("the log got %q", got.String())
	}
}

// A command's line may hold a password, so the log names no more than
// that it was a command.
func TestTheWindowLogKeepsACommandLineOut(t *testing.T) {
	a, _ := agentApp(t)
	said := a.paneForLog(Pane{Title: "mysql --password=hunter2", Command: true})
	if strings.Contains(said, "hunter2") || !strings.Contains(said, "a command") {
		t.Fatalf("a command pane is logged as %q", said)
	}
}
