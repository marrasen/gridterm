package app

import (
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
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
	pumpBoth(t, a, b, "the split on the server", func() bool {
		return len(b.st.Panes) == 3 && len(a.st.Panes) == 4 && b.terminal(b.st.Panes[2].ID) != nil &&
			strings.Contains(b.terminal(b.st.Panes[2].ID).AllText(), "READY")
	})
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

	// A command that fails there says so here, with its status.
	b.handle(RunCommand{Machine: win, Line: "false"})
	pumpBoth(t, a, b, "the failed command", func() bool {
		if len(b.st.Panes) != 5 {
			return false
		}
		tm := b.terminal(b.st.Panes[4].ID)
		return tm != nil && tm.Asking() == "false finished. Exit 1. Run it again?"
	})
	b.handle(ClosePane{Pane: b.st.Panes[4].ID})
	pumpBoth(t, a, b, "the failed command closed", func() bool { return len(b.st.Panes) == 4 })

	// A machine the other window has no connection to is refused there,
	// saying why in the pane here.
	b.handle(OpenOn{Machine: machines.FarID(win, "nowhere")})
	pumpBoth(t, a, b, "the refusal", func() bool {
		return len(b.st.Panes) == 5 && b.terminal(b.st.Panes[4].ID) != nil &&
			strings.Contains(b.terminal(b.st.Panes[4].ID).AllText(), "knows no machine called nowhere")
	})
	panes := len(a.st.Panes)

	// One it knows but is not connected to is refused as well, rather
	// than connected to, which would ask its questions over there; and
	// a kakel window beyond it is its own to open on.
	idle := a.machines.NewQuick("idle.example", false)
	beyond := a.machines.NewQuick("beyond.example:7777", true)
	for i, key := range []machines.ID{idle, beyond} {
		b.handle(OpenOn{Machine: machines.FarID(win, string(key))})
		pumpBoth(t, a, b, "the refusal of "+string(key), func() bool {
			n := len(b.st.Panes)
			return n == 5+i+1 && b.terminal(b.st.Panes[n-1].ID) != nil &&
				strings.Contains(b.terminal(b.st.Panes[n-1].ID).AllText(), "kakel:")
		})
	}
	if len(a.st.Panes) != panes || len(a.machines.Dialing()) != 0 {
		t.Fatalf("refused, the other window opened %+v and dials %v", a.st.Panes, a.machines.Dialing())
	}
}

// A tunnel through a window reaches what a server beyond it reaches,
// and what the window's own machine does; letting go of the window
// takes them along.
func TestATunnelGoesThroughAWindow(t *testing.T) {
	_, conn, echo := tunnelApp(t)
	a, b := connectedWindows(t)
	a.machines.At("srv").Conn = conn
	win := b.st.Windows[0].Name
	ping := func(id string) {
		t.Helper()
		c, err := net.Dial("tcp", b.tunnels[id].Forwarder().Addr())
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		done := make(chan error, 1)
		go func() {
			if _, err := c.Write([]byte("ping")); err != nil {
				done <- err
				return
			}
			buf := make([]byte, 4)
			_, err := io.ReadFull(c, buf)
			if err == nil && !strings.EqualFold(string(buf), "ping") {
				err = fmt.Errorf("read back %q", buf)
			}
			done <- err
		}()
		pumpBoth(t, a, b, "the echo", func() bool {
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
				return true
			default:
				return false
			}
		})
	}
	for _, on := range []machines.ID{machines.FarID(win, "srv"), win} {
		if err := b.openTunnel(OpenTunnel{Machine: on, Tunnel: remote.Tunnel{Kind: remote.LocalForward, Listen: "127.0.0.1:0", Target: echo}}); err != nil {
			t.Fatal(err)
		}
		ping(b.st.Tunnels[len(b.st.Tunnels)-1].ID)
	}
	if err := b.openTunnel(OpenTunnel{Machine: win, Tunnel: remote.Tunnel{Kind: remote.RemoteForward, Listen: "127.0.0.1:0", Target: echo}, Sure: true}); err == nil {
		t.Fatal("a window listened for a tunnel from here")
	}
	if err := b.disconnectWindow(win); err != nil {
		t.Fatal(err)
	}
	pumpBoth(t, a, b, "the window to go", func() bool { return b.machines.Get(win).Window == nil })
	if len(b.st.Tunnels) != 0 || len(b.tunnels) != 0 {
		t.Fatalf("let go of, the window leaves tunnels %+v", b.st.Tunnels)
	}
}
