package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
)

// A first pane that cannot be opened leaves the window there, saying
// why, rather than the program going; the first pane opened after lets
// it go as usual.
func TestAFirstPaneThatFailsLeavesTheWindowSaying(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.ctx = t.Context()
	a.opts.command = "kakel-there-is-no-such-program"
	a.openFirstOrSay()
	if len(a.st.Panes) != 0 {
		t.Skipf("a program that is not there started a pane here: %+v", a.st.Panes)
	}
	if !slices.ContainsFunc(a.st.Notices, func(n Notice) bool { return n.Title == "Couldn't open the first pane" }) {
		t.Fatalf("it said %+v", a.st.Notices)
	}
	if a.emptyAndIdle() {
		t.Fatal("with the first pane failed, the window leaves")
	}
	a.addPane(Pane{ID: "p1", Title: "one", Kind: kindFiles}, nil, placement{})
	a.remove("p1")
	if !a.emptyAndIdle() {
		t.Fatal("once a pane has been opened and closed, the window stays")
	}
}

// -ssh with -e runs the command on the server, and the window is never
// empty with nothing on its way between the connection and the pane.
func TestSshWithACommandRunsItThere(t *testing.T) {
	a, answering := dialApp(t)
	h, _ := a.book.Lookup("srv")
	target := fmt.Sprintf("tester@%s:%d", h.Address, h.Port)
	a.opts.ssh, a.opts.command = target, "echo hi"
	a.openFirstOrSay()
	waitFor(t, a, "the command's pane", func() bool {
		answering()
		if a.emptyAndIdle() {
			t.Fatal("between the connection and the command's pane, the window would leave")
		}
		return slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return p.Command && strings.Contains(p.Title, "echo hi") })
	})
	i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.Command })
	if p := a.st.Panes[i]; a.machines.Name(p.Machine) != target {
		t.Fatalf("the command ran on %q, want %q", a.machines.Name(p.Machine), target)
	}
}
