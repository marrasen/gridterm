package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/mcp"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui/term"
)

// A real editor, in a real pane, driven over the real protocol.
//
// Every other test about a list of steps runs against a pane that
// answers at once and always says the same thing. What is left to go
// wrong here is timing -- a wait that ends early, an echo matched
// instead of an output, keys landing in the program that was there a
// moment ago -- and a fake pane has no timing.
//
// So this one goes the whole way down: JSON-RPC in, the MCP server, the
// agent protocol, the window, a pty, a shell, vim. Then it reads the
// file off the disk, which is the one end nothing here can fake.
//
// Off by default: it wants vim and a shell, which the Windows runner
// has neither of, and it takes a real editor's time. Run it with
//
//	GRIDTERM_REAL_VIM=1 go test -run TestAnAgentEditsAFileWithVim .
func TestAnAgentEditsAFileWithVim(t *testing.T) {
	if os.Getenv("GRIDTERM_REAL_VIM") == "" {
		t.Skip("set GRIDTERM_REAL_VIM=1 to drive a real vim in a real pane")
	}
	vim, err := exec.LookPath("vim")
	if err != nil {
		t.Skipf("no vim on this machine: %v", err)
	}
	dir := t.TempDir()

	a := newTestApp(t, 90, 30)
	withDialogs(t, a)
	withPanel(t, a)
	pane := realShellPane(t, a, dir)
	code := handedOverPane(t, a, pane)
	call := talkToMCP(t, a)

	call(t, "use_session_code", map[string]any{"code": code})
	name := onePaneName(t, call(t, "list_panes", map[string]any{}))

	// The whole editing session in one call, the way the tool describes
	// it: wait for the prompt, start vim, wait for vim's own word for a
	// file that is not there yet, type three lines, write and quit, wait
	// for the prompt to come back.
	said := call(t, "send_keys", map[string]any{
		"pane": name,
		"steps": []string{
			"until:$",
			"type:" + vim + " notes.md",
			"key:Enter",
			"until:[New",
			"type:ione",
			"key:Enter",
			"type:two",
			"key:Enter",
			"type:three",
			"key:Escape",
			"type::wq",
			"key:Enter",
			"until:$",
		},
		"timeout_ms": 10000,
	})
	if strings.Contains(said, "Stopped at step") {
		t.Fatalf("the list stopped:\n%s", said)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "notes.md"))
	if err != nil {
		t.Fatalf("read what vim wrote: %v\n\nthe pane said:\n%s", err, said)
	}
	if got := string(raw); got != "one\ntwo\nthree\n" {
		t.Errorf("the file holds %q\n\nthe pane said:\n%s", got, said)
	}
}

// realShellPane opens a pane on a real shell in dir, rather than on the
// pipe the rest of the tests use, and focuses it.
func realShellPane(t *testing.T, a *testApp, dir string) *term.Terminal {
	t.Helper()
	was := a.newShell
	a.newShell = func(argv []string, dir string, cols, rows int) (session.Session, error) {
		return session.StartLocal(session.LocalConfig{
			Command: []string{"/bin/sh"},
			Dir:     dir,
			// A shell with nothing of whoever is running the tests in
			// it: a prompt to wait for, and no vimrc of theirs to open
			// the editor on something this does not expect.
			Env:  []string{"PS1=$ ", "HOME=" + dir, "ENV=", "VIMINIT=set nocompatible|set noswapfile"},
			Cols: cols, Rows: rows,
		})
	}
	defer func() { a.newShell = was }()

	pane, err := a.localTerminalIn(nil, dir)
	if err != nil {
		t.Fatalf("a pane on a real shell: %v", err)
	}
	if err := a.placePane(pane); err != nil {
		t.Fatalf("place it: %v", err)
	}
	return pane
}

// talkToMCP starts an MCP server on the window, the way the real one
// runs, and hands back a way to call one of its tools.
func talkToMCP(t *testing.T, a *testApp) func(*testing.T, string, map[string]any) string {
	t.Helper()
	toServer, fromTest := io.Pipe()
	fromServer, toTest := io.Pipe()
	panes := mcp.NewWindow()
	done := make(chan error, 1)
	go func() { done <- mcp.Serve(t.Context(), toServer, toTest, panes) }()
	t.Cleanup(func() {
		_ = fromTest.Close()
		_ = panes.Close()
		<-done
	})
	in := bufio.NewReaderSize(fromServer, 1<<16)
	id := 0

	return func(t *testing.T, tool string, args map[string]any) string {
		t.Helper()
		id++
		asked, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": id, "method": "tools/call",
			"params": map[string]any{"name": tool, "arguments": args},
		})
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		var said string
		// The window answers from the goroutine that draws, so it has
		// to be pumped while the call is in flight.
		offWindow(t, a, "the MCP server to answer "+tool, func() error {
			if _, err := fromTest.Write(append(asked, '\n')); err != nil {
				return err
			}
			line, err := in.ReadBytes('\n')
			if err != nil {
				return err
			}
			var answer struct {
				Result struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
					IsError bool `json:"isError"`
				} `json:"result"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(line, &answer); err != nil {
				return err
			}
			for _, c := range answer.Result.Content {
				said += c.Text
			}
			switch {
			case answer.Error != nil:
				t.Errorf("%s: %s", tool, answer.Error.Message)
			case answer.Result.IsError && tool != "send_keys":
				// send_keys says its own failures, which the test reads.
				t.Errorf("%s: %s", tool, said)
			}
			return nil
		})
		return said
	}
}

// onePaneName is the pane in what list_panes answered: the name before
// the first colon of its one line.
func onePaneName(t *testing.T, said string) string {
	t.Helper()
	name, _, found := strings.Cut(strings.TrimSpace(said), ":")
	if !found || name == "" || strings.Contains(name, "\n") {
		t.Fatalf("list_panes said %q", said)
	}
	return name
}
