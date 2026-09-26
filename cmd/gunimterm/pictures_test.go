package main

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"

	"github.com/marrasen/gridterm/pasted"
)

// onClipboard puts a picture on the clipboard as the program reads it,
// for the length of a test.
func onClipboard(t *testing.T, img image.Image) {
	t.Helper()
	was := readPicture
	readPicture = func() (image.Image, bool, error) { return img, img != nil, nil }
	t.Cleanup(func() { readPicture = was })
}

// localPane is a program side with one local pane, p1, running argv,
// which prints out and whose typing is recorded.
func localPane(t *testing.T, out string, argv ...string) (*app, *printed) {
	t.Helper()
	t.Setenv("TMPDIR", t.TempDir())
	w := gunim.NewOffscreen(geom.Sz(400, 300), nil)
	a := newApp(w.Client(), &shells{m: map[string]*shell{}})
	a.ctx = t.Context()
	sess := &printed{typed: typed{done: make(chan struct{})}, out: []byte(out)}
	quiet := shellHooks{output: func() {}, title: func(string) {}, exit: func() {}, clipboard: func(string) {}}
	a.addPane(Pane{ID: "p1", Title: "Terminal 1"}, openShell(sess, a.palette, quiet), placement{})
	a.argvs["p1"] = argv
	t.Cleanup(func() { a.remove("p1") })
	return a, sess
}

func TestPasteImageAsFileTypesThePathOfThePicture(t *testing.T) {
	a, sess := localPane(t, "", "/bin/bash")
	onClipboard(t, image.NewRGBA(image.Rect(0, 0, 3, 2)))
	a.handle(PasteImage{})
	waitFor(t, a, "the path typed", func() bool { return strings.Contains(sess.sent(), ".png") })
	path := strings.NewReplacer("\x1b[200~", "", "\x1b[201~", "").Replace(sess.sent())
	if filepath.Base(filepath.Dir(path)) != pasted.DirName {
		t.Fatalf("typed %q, want a file in %s", sess.sent(), pasted.DirName)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil || img.Bounds().Dx() != 3 || img.Bounds().Dy() != 2 {
		t.Fatalf("the file holds %v, %v", img.Bounds(), err)
	}
}

func TestAPasteWithOnlyAPicturePressesPasteForAProgram(t *testing.T) {
	// A program that reads the clipboard itself, as Claude Code does,
	// on the full screen, which says a program is running.
	a, sess := localPane(t, "\x1b[?1049h", "/bin/bash")
	waitFor(t, a, "the program's screen", func() bool { return a.terminal("p1").RunningAProgram() })
	onClipboard(t, image.NewRGBA(image.Rect(0, 0, 3, 2)))
	a.handle(PastePicture{Pane: "p1"})
	waitFor(t, a, "paste pressed", func() bool { return sess.sent() != "" })
	if got := sess.sent(); got != "\x16" {
		t.Fatalf("the program was sent %q, want Ctrl+V", got)
	}
}

func TestAShellAtItsPromptIsHandedAFileInstead(t *testing.T) {
	// bash reads Ctrl+V at its prompt as quoting the next key.
	a, sess := localPane(t, "", "/bin/bash")
	onClipboard(t, image.NewRGBA(image.Rect(0, 0, 3, 2)))
	a.handle(PastePicture{Pane: "p1"})
	waitFor(t, a, "the path typed", func() bool { return strings.Contains(sess.sent(), ".png") })
	if strings.Contains(sess.sent(), "\x16") {
		t.Fatalf("the shell was sent Ctrl+V: %q", sess.sent())
	}
}

func TestAnEmptyClipboardPastesNothing(t *testing.T) {
	a, sess := localPane(t, "", "/bin/bash")
	onClipboard(t, nil)
	a.handle(PastePicture{Pane: "p1"})
	a.handle(PasteImage{Pane: "p1"})
	waitFor(t, a, "the notice", func() bool { return len(a.st.Notices) > 0 })
	if sess.sent() != "" || len(a.st.Notices) != 1 {
		t.Fatalf("with nothing to paste, the pane was sent %q, and the notices are %+v", sess.sent(), a.st.Notices)
	}
}

// connectedWindows is a served window, a, and a second, b, connected
// to it, with a terminal on a open in b as its first pane.
func connectedWindows(t *testing.T) (a, b *app) {
	t.Helper()
	a, _ = agentApp(t)
	dir := t.TempDir()
	a.serving.hostKey, a.serving.allowed = filepath.Join(dir, "host_key"), filepath.Join(dir, "authorized_keys")
	b, keyFile := clientOf(t, a)
	b.handle(ConnectWindow{Addr: a.st.Serving.Addr, KeyFile: keyFile})
	pumpBoth(t, a, b, "the question about the host key", func() bool { return len(b.st.Asks) > 0 })
	b.handle(AskAnswered{ID: b.st.Asks[0].ID, Yes: true})
	pumpBoth(t, a, b, "a terminal on the window", func() bool { return len(b.st.Panes) == 1 })
	pumpBoth(t, a, b, "the pane on the first window", func() bool { return len(a.st.Panes) == 2 })
	return a, b
}

func TestAPictureGoesOnTheClipboardOfTheWindowItIsPastedInto(t *testing.T) {
	// Taken on the goroutine serving the other window.
	took := make(chan []byte, 1)
	was := takePicture
	takePicture = func(png []byte) error { took <- png; return nil }
	t.Cleanup(func() { takePicture = was })
	a, b := connectedWindows(t)

	onClipboard(t, image.NewRGBA(image.Rect(0, 0, 5, 4)))
	b.handle(PastePicture{Pane: b.st.Panes[0].ID})
	var got []byte
	pumpBoth(t, a, b, "the picture on the first window's clipboard", func() bool {
		select {
		case got = <-took:
			return true
		default:
			return false
		}
	})
	img, err := png.Decode(bytes.NewReader(got))
	if err != nil || img.Bounds().Dx() != 5 || img.Bounds().Dy() != 4 {
		t.Fatalf("the first window took %v, %v", img, err)
	}

	// As a file, it is written on the window's machine, and its path
	// typed into the shell there.
	b.handle(PasteImage{Pane: b.st.Panes[0].ID})
	there := a.st.Panes[1].ID
	pumpBoth(t, a, b, "the path typed there", func() bool {
		if len(b.st.Notices) > 0 {
			t.Fatalf("pasting as a file said %+v", b.st.Notices)
		}
		// The path wraps at the screen's edge, so its end is looked for.
		return strings.Contains(a.terminal(there).Text(), ".png")
	})
}

// A pane attached from a window, running on a server that window
// reached, has its picture written on that server, through the window.
func TestAPictureReachesTheServerAPaneOnAWindowRunsOn(t *testing.T) {
	// The test server's files start in the folder the test runs in.
	far := t.TempDir()
	t.Chdir(far)
	_, conn, _ := tunnelApp(t)
	a, b := connectedWindows(t)
	a.conns["srv"] = conn
	if err := a.open("srv", placement{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the pane on the server", func() bool { return len(a.st.Panes) == 3 })
	onSrv := a.st.Panes[2].ID
	pumpBoth(t, a, b, "the server's pane listed", func() bool {
		a.publish()
		for _, w := range b.st.Windows {
			for _, o := range w.Open {
				if o.ID == onSrv {
					return true
				}
			}
		}
		return false
	})
	b.handle(AttachWindow{Window: b.st.Windows[0].Name, ID: onSrv})
	pumpBoth(t, a, b, "the pane attached", func() bool { return len(b.st.Panes) == 2 })
	id := b.st.Panes[1].ID
	if b.farHost[id] != "srv" {
		t.Fatalf("attached, the pane runs on %q, want srv", b.farHost[id])
	}

	onClipboard(t, image.NewRGBA(image.Rect(0, 0, 3, 2)))
	b.handle(PastePicture{Pane: id})
	pumpBoth(t, a, b, "the path typed on the server", func() bool {
		if len(b.st.Notices) > 0 {
			t.Fatalf("pasting said %+v", b.st.Notices)
		}
		return strings.Contains(a.terminal(onSrv).Text(), ".png")
	})
	ents, err := os.ReadDir(filepath.Join(far, pasted.DirName))
	if err != nil || len(ents) != 1 {
		t.Fatalf("on the server: %v, %v", ents, err)
	}
}
