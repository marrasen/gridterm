package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/kakel/input"
)

// ctrlClick clicks with Ctrl down on where text is on pane id's
// screen, once it is there.
func ctrlClick(t *testing.T, a *app, id, text string) {
	t.Helper()
	term := a.terminal(id)
	var row, col int
	waitFor(t, a, text+" on the screen", func() bool {
		for i, line := range strings.Split(term.Text(), "\n") {
			// The command line echoes it too; the output is the line
			// that starts with it.
			if strings.HasPrefix(line, text) {
				row, col = i, 2
				return true
			}
		}
		return false
	})
	// The window draws the screen, which is where links are found.
	a.shells.get(id).draw()
	_, _ = term.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row, Mods: input.ModCtrl})
	_, _ = term.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: col, Row: row, Mods: input.ModCtrl})
}

func TestCtrlClickOpensAnAddressInTheBrowser(t *testing.T) {
	a, _ := agentApp(t)
	opened := make(chan string, 1)
	was := openInBrowser
	openInBrowser = func(at string) error { opened <- at; return nil }
	t.Cleanup(func() { openInBrowser = was })
	id := a.st.Panes[0].ID
	a.terminal(id).Paste(clearAndEcho("https://example.com/docs") + "\r")
	ctrlClick(t, a, id, "https://example.com/docs")
	var got string
	waitFor(t, a, "the browser", func() bool {
		select {
		case got = <-opened:
			return true
		default:
			return false
		}
	})
	if got != "https://example.com/docs" {
		t.Fatalf("the browser was given %q", got)
	}
}

func TestCtrlClickOpensAFileInTheReader(t *testing.T) {
	a, _ := agentApp(t)
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := a.st.Panes[0].ID
	widen(a, id)
	a.terminal(id).Paste(clearAndEcho(file) + "\r")
	ctrlClick(t, a, id, file)
	waitFor(t, a, "the reader", func() bool {
		for _, r := range a.st.Readers {
			if r.Path == file && len(r.Lines) >= 3 {
				return true
			}
		}
		return false
	})
}

func TestOnlyWebAndMailLinksOpen(t *testing.T) {
	for at, ok := range map[string]bool{
		"https://example.com": true, "mailto:a@example.com": true,
		"file:///etc/passwd": false, "javascript:alert(1)": false, "": false,
	} {
		if err := linkIsOpenable(at); (err == nil) != ok {
			t.Errorf("%q: %v", at, err)
		}
	}
	if target, ok := serviceOnTheFarEnd("srv", "http://localhost:3000/app"); !ok || target != "127.0.0.1:3000" {
		t.Errorf("a server's localhost:3000 goes to %q, %v", target, ok)
	}
	if _, ok := serviceOnTheFarEnd("", "http://localhost:3000/app"); ok {
		t.Error("this machine's localhost went through a tunnel")
	}
}

func TestAServersLocalAddressOpensThroughATunnel(t *testing.T) {
	a, _, echo := tunnelApp(t)
	opened := make(chan string, 2)
	was := openInBrowser
	openInBrowser = func(at string) error { opened <- at; return nil }
	t.Cleanup(func() { openInBrowser = was })
	_, port, _ := strings.Cut(echo, ":")
	if err := a.openLink("srv", "http://localhost:"+port+"/app?x=1"); err != nil {
		t.Fatal(err)
	}
	got := <-opened
	if len(a.st.Tunnels) != 1 || !strings.HasSuffix(got, "/app?x=1") || strings.Contains(got, ":"+port+"/") {
		t.Fatalf("opened %q over tunnels %+v", got, a.st.Tunnels)
	}
	// A second click goes through the same tunnel.
	if err := a.openLink("srv", "http://127.0.0.1:"+port+"/other"); err != nil {
		t.Fatal(err)
	}
	<-opened
	if len(a.st.Tunnels) != 1 {
		t.Fatalf("the second click opened another tunnel: %+v", a.st.Tunnels)
	}
}

func TestTheScrollbackOpensInAReaderToSearch(t *testing.T) {
	a, _ := agentApp(t)
	id := a.st.Panes[0].ID
	// Typed at the prompt, so the numbers start a line of their own.
	waitFor(t, a, "the prompt", func() bool { return strings.Contains(a.terminal(id).Text(), promptEnd()) })
	a.terminal(id).Paste(countTo("300") + "\r")
	waitFor(t, a, "the numbers", func() bool { return strings.Contains(a.terminal(id).AllText(), "\n300\n") })
	a.handle(ShowScrollback{Pane: id})
	if len(a.st.Panes) != 2 || a.st.Panes[1].Kind != kindReader {
		t.Fatalf("the panes are %+v", a.st.Panes)
	}
	r := a.st.Readers[a.st.Panes[1].ID]
	text := strings.Join(r.Lines, "\n")
	// The last number may end the text, the prompt after it not come yet.
	if !r.Find || !strings.Contains(text, "\n1\n2\n3\n") || !strings.Contains(text+"\n", "\n300\n") {
		t.Fatalf("the reader holds %d lines, find %v, starting %q", len(r.Lines), r.Find, r.Lines[:5])
	}
	if strings.ContainsAny(filepath.Base(r.SaveAs), `/:`) || !strings.HasSuffix(r.SaveAs, " scrollback.txt") {
		t.Fatalf("a save is offered at %q", r.SaveAs)
	}
	// Asked again, the same viewer comes forward with its find open
	// again, and reading it again reads what the pane has said since.
	reader := a.st.Panes[1].ID
	a.st.Focus = id
	a.terminal(id).Paste("echo later-line\r")
	waitFor(t, a, "the echo", func() bool { return strings.Contains(a.terminal(id).AllText(), "\nlater-line\n") })
	a.handle(ShowScrollback{Pane: id})
	if r := a.st.Readers[reader]; len(a.st.Panes) != 2 || a.st.Focus != reader || r.FindAgain != 1 {
		t.Fatalf("asked again, the panes are %+v, the focus on %s, the reader %+v", a.st.Panes, a.st.Focus, r.FindAgain)
	}
	a.handle(ReadAgain{Pane: reader})
	if r := a.st.Readers[reader]; !slices.Contains(r.Lines, "later-line") {
		t.Fatalf("read again, the reader ends %q", r.Lines[len(r.Lines)-3:])
	}
}

// A path a shell on a far Windows machine prints from the top of a
// drive is looked for at the top of that drive.
func TestAWindowsPathFromTheTopOfADriveStandsAlone(t *testing.T) {
	for p, abs := range map[string]bool{
		`C:\Users\x`: true, "d:/x": true, `C:\`: true,
		"C:": false, "a:b.jar": false, "notes.txt": false, "/home/x": false, "1:/x": false,
	} {
		if windowsAbs(p) != abs {
			t.Errorf("%q from the top of a drive: %v, want %v", p, !abs, abs)
		}
	}
}

// A path in a pane on a server is found and opened with no file pane
// open there: the server's files are opened for it.
func TestAPathOnAServerOpensWithNoFilePaneThere(t *testing.T) {
	a, answering := dialApp(t)
	a.handle(OpenOn{Machine: "srv"})
	waitFor(t, a, "a shell on the server", func() bool { answering(); return oneShell(a) })
	if a.fsFor("srv") != nil {
		t.Fatal("the server's files were open before anything asked for them")
	}
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Asked again each frame, as the pointer rests on the path.
	var at string
	var found bool
	waitFor(t, a, "the path to be found", func() bool {
		at, _, found = a.findFar("srv", file, "")
		return found
	})
	if at != file {
		t.Fatalf("found %q, want %q", at, file)
	}
	if err := a.openPath("srv", at, false, 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the reader", func() bool {
		for _, r := range a.st.Readers {
			if r.Path == file && len(r.Lines) >= 2 {
				return true
			}
		}
		return false
	})
	if len(a.st.Notices) != 0 {
		t.Fatalf("it said %+v", a.st.Notices)
	}
	// The connection gone, what the server said is forgotten.
	_ = a.machines.Get("srv").Conn.Close()
	waitFor(t, a, "the connection to go", func() bool { return a.machines.Get("srv").Conn == nil && a.fsFor("srv") == nil })
	a.far.mu.Lock()
	defer a.far.mu.Unlock()
	if len(a.far.known) != 0 {
		t.Fatalf("after the connection went, it still knew %v", a.far.known)
	}
}

// A path in a pane on a server that is not connected is not looked
// for, and connecting is left to the user.
func TestAPathOnAServerNotConnectedIsNotLookedFor(t *testing.T) {
	a, _ := dialApp(t)
	for range 3 {
		if _, _, found := a.findFar("srv", "/etc/hostname", ""); found {
			t.Fatal("found a path on a server not connected")
		}
		waitFor(t, a, "the ask to finish", func() bool {
			a.far.mu.Lock()
			defer a.far.mu.Unlock()
			return len(a.far.asking) == 0
		})
	}
	if len(a.machines.Dialing()) != 0 || len(a.machines.Connected()) != 0 || len(a.st.Asks) != 0 {
		t.Fatalf("looking for a path connected: dialing %v, connections %v, asks %v", a.machines.Dialing(), a.machines.Connected(), a.st.Asks)
	}
	a.far.mu.Lock()
	defer a.far.mu.Unlock()
	if len(a.far.known) != 0 {
		t.Fatalf("not knowing was remembered: %v", a.far.known)
	}
}
