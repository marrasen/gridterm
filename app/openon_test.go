package app

import (
	"fmt"
	"io"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/agent"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/settings"
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

// The connection log a window keeps for a server beyond it shows here,
// and follows what is written to it; one it keeps none of is refused.
func TestAFarMachinesLogShowsThroughItsWindow(t *testing.T) {
	_, conn, _ := tunnelApp(t)
	a, b := connectedWindows(t)
	a.machines.At("srv").Conn = conn
	logLine(a.account("srv"), "", "first line of srv")
	win := b.st.Windows[0].Name
	far := machines.FarID(win, "srv")
	// Asked twice before it lands, as a double click does: one pane.
	b.handle(ShowLog{Machine: far})
	b.handle(ShowLog{Machine: far})
	// A log's pane is read as its shell, which terminal leaves out.
	shows := func(pane, text string) func() bool {
		return func() bool {
			sh := b.shells.Get(pane)
			return sh != nil && strings.Contains(sh.T.AllText(), text)
		}
	}
	pumpBoth(t, a, b, "the log's pane", func() bool { return len(b.st.Panes) == 2 && len(b.farLogs) == 0 })
	log := b.st.Panes[1]
	if log.Kind != KindLog || log.Machine != win || log.On != "srv" {
		t.Fatalf("the log's pane is %+v", log)
	}
	pumpBoth(t, a, b, "the line so far", shows(log.ID, "first line of srv"))
	logLine(a.machines.Get("srv").Log, "", "a line written since")
	pumpBoth(t, a, b, "the line since", shows(log.ID, "a line written since"))
	// Asked again, it goes to the pane it has.
	b.handle(ShowLog{Machine: far})
	if len(b.st.Panes) != 2 || b.st.Focus != log.ID {
		t.Fatalf("asked again, the panes are %+v", b.st.Panes)
	}

	// Its window gone, the log's pane closes, as a log's does whose
	// reading has ended: there is no program in it to start again.
	defer func() {
		if err := b.disconnectWindow(win); err != nil {
			t.Fatal(err)
		}
		pumpBoth(t, a, b, "the log's pane to close", func() bool { return !b.has(log.ID) })
	}()

	// One it keeps no log of is refused, saying why.
	b.handle(ShowLog{Machine: machines.FarID(win, "nowhere")})
	pumpBoth(t, a, b, "the refusal", func() bool {
		return slices.ContainsFunc(b.st.Notices, func(n Notice) bool { return strings.Contains(n.Body, "keeps no connection log") })
	})
	if len(b.st.Panes) != 2 {
		t.Fatalf("refused, the panes are %+v", b.st.Panes)
	}
}

// A window asks another to close its connection to a server beyond it:
// that window says who asked, and what was open on the server ends
// here too. One it is not connected to is refused, saying why.
func TestAWindowDisconnectsAServerBeyondAnother(t *testing.T) {
	_, conn, _ := tunnelApp(t)
	a, b := connectedWindows(t)
	a.machines.At("srv").Conn = conn
	win := b.st.Windows[0].Name
	far := machines.FarID(win, "srv")
	b.handle(OpenOn{Machine: far})
	pumpBoth(t, a, b, "the terminal on the server", func() bool { return len(b.st.Panes) == 2 && len(a.st.Panes) == 3 })

	b.handle(Disconnect{Machine: far})
	pumpBoth(t, a, b, "the server let go of", func() bool {
		// Closed: the connection was made by another app here, which
		// hears it close; this one only holds it.
		return conn.Closed() && b.st.Panes[1].Ended &&
			slices.ContainsFunc(b.st.Notices, func(n Notice) bool { return n.Title == "Disconnected srv through "+b.machines.Name(win) })
	})
	if !slices.ContainsFunc(a.st.Notices, func(n Notice) bool { return strings.HasSuffix(n.Title, " disconnected srv") }) {
		t.Fatalf("the other window said %+v", a.st.Notices)
	}
	if a.machines.Get("srv").Dropped {
		t.Fatal("let go of on purpose, the server is kept as dropped")
	}

	a.machines.At("srv").Conn = nil
	b.handle(Disconnect{Machine: far})
	pumpBoth(t, a, b, "the refusal", func() bool {
		return slices.ContainsFunc(b.st.Notices, func(n Notice) bool {
			return n.Title == "Couldn't disconnect srv through "+b.machines.Name(win) && strings.Contains(n.Body, "srv")
		})
	})
}

// asAgentBoth runs f as an agent's calls come, running both windows'
// sides until it is done: an agent working through b works in a too.
func asAgentBoth(t *testing.T, a, b *app, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	pumpBoth(t, a, b, "the agent's calls", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
}

// An agent handed a pane that runs on a server beyond a window reads
// it, types into it, is told where it runs, and opens another there;
// one handed a pane on the window's own machine opens one there.
func TestAnAgentWorksInPanesThroughAWindow(t *testing.T) {
	_, conn, _ := tunnelApp(t)
	a, b := connectedWindows(t)
	a.machines.At("srv").Conn = conn
	win := b.st.Windows[0].Name
	b.handle(OpenOn{Machine: machines.FarID(win, "srv")})
	pumpBoth(t, a, b, "the terminal on the server", func() bool { return len(b.st.Panes) == 2 && len(a.st.Panes) == 3 })
	onWin, onFar := b.st.Panes[0].ID, b.st.Panes[1].ID
	pumpBoth(t, a, b, "the server's shell", func() bool { return strings.Contains(b.terminal(onFar).AllText(), "READY") })
	b.handle(SharePane{Pane: onWin})
	b.handle(SharePane{Pane: onFar})
	for _, p := range []string{onWin, onFar} {
		b.handle(SetAgentMay{Pane: p, May: settings.AgentMay{OpenMore: true, Restart: true}})
	}
	t.Cleanup(func() { _ = b.stopSharing() })
	var c *agent.Client
	var sh agent.Share
	var err error
	asAgentBoth(t, a, b, func() {
		if c, err = agent.Dial(b.st.Share.Code); err == nil {
			sh, err = c.Use(b.st.Share.Code)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	byLabel := map[string]agent.Pane{}
	for _, p := range sh.Panes {
		byLabel[p.Label] = p
	}
	name := b.machines.Name(win)
	far, ok := byLabel["Terminal 3 on srv through "+name]
	if !ok {
		t.Fatalf("the agent is told of %+v", sh.Panes)
	}

	// It types into the server's shell, which echoes, and reads it back.
	asAgentBoth(t, a, b, func() { err = c.Send(far.ID, "hello-far", []string{"Enter"}) })
	if err != nil {
		t.Fatal(err)
	}
	var look agent.Look
	pumpBoth(t, a, b, "the echo", func() bool { return strings.Contains(b.terminal(onFar).AllText(), "hello-far") })
	asAgentBoth(t, a, b, func() { look, err = c.Read(far.ID, 0) })
	if err != nil || !strings.Contains(look.Screen, "hello-far") {
		t.Fatalf("it read %q, %v", look.Screen, err)
	}

	// Ended, it starts again there, on the server.
	asAgentBoth(t, a, b, func() { err = c.Send(far.ID, "bye\n", nil) })
	if err != nil {
		t.Fatal(err)
	}
	pumpBoth(t, a, b, "the server's shell to end", func() bool { return b.terminal(onFar).Exited() })
	// The pane there that this one shows.
	thereID := ""
	for fid, pane := range b.machines.Get(win).Window.Bound {
		if pane == onFar {
			thereID = fid
		}
	}
	restarts := a.restarts[thereID]
	var again agent.Pane
	asAgentBoth(t, a, b, func() { again, err = c.Restart(far.ID) })
	if err != nil || again.Ended {
		t.Fatalf("restarted, it is %+v, %v", again, err)
	}
	// Started again in the same pane there, on the server.
	pumpBoth(t, a, b, "the server's shell again", func() bool {
		return !b.terminal(onFar).Exited() && a.restarts[thereID] > restarts
	})
	if thereID == "" || a.machineOf(thereID) != "srv" {
		t.Fatalf("this pane shows %q there, on %q", thereID, a.machineOf(thereID))
	}

	// It asks the user to type something there, and hears that they did.
	answered := make(chan bool, 1)
	go func() {
		ok, err := c.Secret(far.ID, "the sudo password", 10*time.Second)
		answered <- ok && err == nil
	}()
	pumpBoth(t, a, b, "the ask on the pane", func() bool { return b.terminal(onFar).AskedForASecret() })
	_, _ = b.terminal(onFar).HandleKey(input.Event{Kind: input.Text, Rune: 'Q', NormalText: true})
	_, _ = b.terminal(onFar).HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEnter})
	var typed bool
	pumpBoth(t, a, b, "the answer", func() bool {
		select {
		case typed = <-answered:
			return true
		default:
			return false
		}
	})
	if !typed {
		t.Fatal("the user typed, and the agent was told they did not")
	}
	pumpBoth(t, a, b, "what was typed, on the server", func() bool { return strings.Contains(a.terminal(a.st.Panes[2].ID).AllText(), "Q") })

	// Another pane there, on the server, and on the window's machine.
	var opened agent.Pane
	asAgentBoth(t, a, b, func() { opened, err = c.Open(far.ID) })
	if err != nil || opened.Label != "Terminal 4 on srv through "+name {
		t.Fatalf("opened %+v, %v", opened, err)
	}
	if p := b.st.Panes[len(b.st.Panes)-1]; p.On != "srv" || !slices.ContainsFunc(a.st.Panes, func(q Pane) bool { return q.Machine == "srv" && q.ID != a.st.Panes[2].ID }) {
		t.Fatalf("the new pane is %+v here, and there are %+v", p, a.st.Panes)
	}
	own, ok := byLabel["Terminal 2 on "+name]
	if !ok {
		t.Fatalf("the agent is told of %+v", sh.Panes)
	}
	// What a command it typed on the window's machine printed.
	asAgentBoth(t, a, b, func() { err = c.Send(own.ID, "echo out-$((6*7))", []string{"Enter"}) })
	if err != nil {
		t.Fatal(err)
	}
	pumpBoth(t, a, b, "the command's output", func() bool { return strings.Contains(b.terminal(onWin).AllText(), "out-42") })
	asAgentBoth(t, a, b, func() { look, err = c.Output(own.ID, 0) })
	if err != nil || !strings.Contains(look.Screen, "out-42") {
		t.Fatalf("its output read %q, %v", look.Screen, err)
	}
	there := len(a.st.Panes)
	asAgentBoth(t, a, b, func() { opened, err = c.Open(own.ID) })
	if err != nil || opened.Label != "Terminal 5 on "+name {
		t.Fatalf("opened on the window %+v, %v", opened, err)
	}
	if p := b.st.Panes[len(b.st.Panes)-1]; p.Machine != win || p.On != "" || len(a.st.Panes) != there+1 || a.st.Panes[there].Machine != machines.Local {
		t.Fatalf("the new pane is %+v here, and there %+v", p, a.st.Panes)
	}

	// The window let go of the server: an agent's restart is refused
	// there, as it would be here, rather than dialled.
	asAgentBoth(t, a, b, func() { err = c.Send(far.ID, "bye\n", nil) })
	if err != nil {
		t.Fatal(err)
	}
	pumpBoth(t, a, b, "the server's shell to end again", func() bool { return b.terminal(onFar).Exited() })
	a.machines.At("srv").Conn = nil
	asAgentBoth(t, a, b, func() { _, err = c.Restart(far.ID) })
	if err == nil {
		t.Fatal("restarted on a server the window has let go of")
	}
	if !slices.ContainsFunc(b.st.Notices, func(n Notice) bool { return strings.Contains(n.Body, "Reconnect to srv from that window first") }) || len(a.machines.Dialing()) != 0 {
		t.Fatalf("refused, it said %v, with notices %+v, and the window dials %v", err, b.st.Notices, a.machines.Dialing())
	}

	// Nor does the user's own Reconnect dial there: it says to reconnect
	// from that window.
	notices := len(b.st.Notices)
	if err := b.startAgain(onFar); err != nil {
		t.Fatal(err)
	}
	pumpBoth(t, a, b, "the refusal of the user's reconnect", func() bool {
		return len(b.st.Notices) > notices && strings.Contains(b.st.Notices[len(b.st.Notices)-1].Body, "Reconnect to srv from that window first")
	})
	if len(a.machines.Dialing()) != 0 || !b.terminal(onFar).Exited() {
		t.Fatalf("the window dials %v, and the pane ended %v", a.machines.Dialing(), b.terminal(onFar).Exited())
	}
}

// A program another window ran that gave no exit status, as one killed
// by a signal, is said as having stopped, as that window says it; one
// that gave a status says that status.
func TestAnEndingFromAnotherWindowReadsAsItsOwn(t *testing.T) {
	if status, known := exitStatus(&serve.SignalError{Signal: "KILL"}, true); known {
		t.Fatalf("stopped by a signal, it reads as status %d", status)
	}
	if status, known := exitStatus(&serve.ExitError{Status: 3}, true); !known || status != 3 {
		t.Fatalf("with status 3, it reads as %d, %v", status, known)
	}
}
