package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/input"
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
	a.terminal(id).Paste("clear; echo https://example.com/docs\r")
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
	a.terminal(id).Paste("clear; echo " + file + "\r")
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
	a.terminal(id).Paste("seq 1 300\r")
	waitFor(t, a, "the numbers", func() bool { return strings.Contains(a.terminal(id).AllText(), "\n300\n") })
	a.handle(ShowScrollback{Pane: id})
	if len(a.st.Panes) != 2 || a.st.Panes[1].Kind != kindReader {
		t.Fatalf("the panes are %+v", a.st.Panes)
	}
	r := a.st.Readers[a.st.Panes[1].ID]
	text := strings.Join(r.Lines, "\n")
	if !r.Find || !strings.Contains(text, "\n1\n2\n3\n") || !strings.Contains(text, "\n300\n") {
		t.Fatalf("the reader holds %d lines, find %v", len(r.Lines), r.Find)
	}
}
