package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/kakel/machines"
)

// A window connected to another opens a terminal and runs a command on
// a server that window reaches, and a command on that window's own
// machine: the other window opens each in a pane of its own, and this
// one watches it.
func TestTerminalsAndCommandsOpenBeyondAWindow(t *testing.T) {
	_, conn, _ := tunnelApp(t)
	a, b := connectedWindows(t)
	a.machines.At("srv").Conn = conn
	win := b.st.Windows[0].Name
	far := machines.FarID(win, "srv")

	b.handle(OpenOn{Machine: far})
	pumpBoth(t, a, b, "the terminal on the server", func() bool { return len(b.st.Panes) == 2 && len(a.st.Panes) == 3 })
	if p := b.st.Panes[1]; p.Machine != win || p.On != "srv" || b.farHost[p.ID] != "srv" {
		t.Fatalf("here the pane is %+v", p)
	}
	if p := a.st.Panes[2]; p.Machine != "srv" {
		t.Fatalf("there the pane is %+v", p)
	}
	// Split beside it, a terminal opens where it runs.
	b.st.Focus = b.st.Panes[1].ID
	b.handle(SplitPane{})
	pumpBoth(t, a, b, "the split on the server", func() bool { return len(b.st.Panes) == 3 && len(a.st.Panes) == 4 })
	if p := b.st.Panes[2]; p.On != "srv" || a.st.Panes[3].Machine != "srv" {
		t.Fatalf("split, the pane is %+v here and %+v there", p, a.st.Panes[3])
	}
	b.handle(ClosePane{Pane: b.st.Panes[2].ID})
	a.handle(ClosePane{Pane: a.st.Panes[3].ID})
	pumpBoth(t, a, b, "the split closed", func() bool { return len(b.st.Panes) == 2 && len(a.st.Panes) == 3 })

	b.handle(RunCommand{Machine: far, Line: "echo ran-far"})
	pumpBoth(t, a, b, "the command on the server", func() bool {
		return len(b.st.Panes) == 3 && b.terminal(b.st.Panes[2].ID) != nil && strings.Contains(b.terminal(b.st.Panes[2].ID).AllText(), "ran-far")
	})
	if p := b.st.Panes[2]; !p.Command || p.On != "srv" || !slices.ContainsFunc(a.st.Panes, func(q Pane) bool { return q.Command && q.Machine == "srv" }) {
		t.Fatalf("the command's pane is %+v here, and there the panes are %+v", p, a.st.Panes)
	}

	b.handle(RunCommand{Machine: win, Line: "echo ran-there"})
	pumpBoth(t, a, b, "the command on the window's machine", func() bool {
		return len(b.st.Panes) == 4 && b.terminal(b.st.Panes[3].ID) != nil && strings.Contains(b.terminal(b.st.Panes[3].ID).AllText(), "ran-there")
	})
	if p := b.st.Panes[3]; !p.Command || p.Machine != win || p.On != "" {
		t.Fatalf("the command's pane is %+v", p)
	}

	// A machine the other window has no connection to is refused there,
	// saying why in the pane here.
	b.handle(OpenOn{Machine: machines.FarID(win, "nowhere")})
	pumpBoth(t, a, b, "the refusal", func() bool {
		return len(b.st.Panes) == 5 && b.terminal(b.st.Panes[4].ID) != nil &&
			strings.Contains(b.terminal(b.st.Panes[4].ID).AllText(), "knows no machine called nowhere")
	})
	if len(a.st.Panes) != 5 {
		t.Fatalf("refused, the other window opened %+v", a.st.Panes)
	}
}
