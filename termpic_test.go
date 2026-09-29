package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"

	"github.com/marrasen/kakel/vt"
)

// printed is a session whose program prints once and then waits.
type printed struct {
	typed
	out []byte
}

func (s *printed) Read(p []byte) (int, error) {
	s.mu.Lock()
	if len(s.out) > 0 {
		n := copy(p, s.out)
		s.out = s.out[n:]
		s.mu.Unlock()
		return n, nil
	}
	s.mu.Unlock()
	return s.typed.Read(p)
}

func TestAPictureInTheOutputIsDrawnOverItsCells(t *testing.T) {
	win, sh, publish := windowStage(t)
	var file bytes.Buffer
	if err := png.Encode(&file, image.NewRGBA(image.Rect(0, 0, 40, 20))); err != nil {
		t.Fatal(err)
	}
	out := "before\r\n\x1b]1337;File=inline=1;width=4;height=2:" + base64.StdEncoding.EncodeToString(file.Bytes()) + "\x07after\r\n"
	s := &printed{typed: typed{done: make(chan struct{})}, out: []byte(out)}
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(s, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	publish(State{Panes: []Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &Box{Pane: "p1"}, Focus: "p1"})

	deadline := time.Now().Add(5 * time.Second)
	for len(sh.Get("p1").T.Pictures()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the terminal never took the picture")
		}
		time.Sleep(10 * time.Millisecond)
	}
	lastWindow.Frame(time.Second / 60)
	at := sh.Get("p1").T.Pictures()[0]
	var drawn *paint.ImageOp
	for _, op := range lastWindow.Offscreen().Ops() {
		if im, ok := op.(*paint.ImageOp); ok {
			drawn = im
		}
	}
	if drawn == nil {
		t.Fatal("the picture was never painted")
	}
	if w, h := drawn.Image.Size(); w != 40 || h != 20 {
		t.Fatalf("painted a %dx%d picture, want the 40x20 one", w, h)
	}
	cell := win.terms["p1"].cells.CellSize()
	want := geom.Rc(float32(at.Col)*cell.W, float32(at.Top)*cell.H, 4*cell.W, 2*cell.H)
	if drawn.Rect != want || at.Top != 1 {
		t.Fatalf("painted in %v, want the four by two cells under the first line, %v", drawn.Rect, want)
	}
}

func TestTheWindowIsNamedAfterThePaneInFront(t *testing.T) {
	win, sh, publish := windowStage(t)
	s := &printed{typed: typed{done: make(chan struct{})}, out: []byte("\x1b]2;vim notes.txt\x07")}
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(s, vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	deadline := time.Now().Add(5 * time.Second)
	for sh.Get("p1").T.Title() == "" {
		if time.Now().After(deadline) {
			t.Fatal("the program's title never arrived")
		}
		time.Sleep(10 * time.Millisecond)
	}
	panes := []Pane{{ID: "p1", Title: "Terminal 1"}, {ID: "p2", Title: "files", Kind: kindFiles}}
	publish(State{Panes: panes, Stage: &Box{Pane: "p1"}, Focus: "p1"})
	if got := lastWindow.Offscreen().Title(); got != "kakel — vim notes.txt" {
		t.Fatalf("the window is called %q", got)
	}
	if win.bar.Title != "kakel" || win.bar.Subtitle != "vim notes.txt" {
		t.Fatalf("the title bar says %q and %q", win.bar.Title, win.bar.Subtitle)
	}
	// A pane that is no terminal goes by its own title.
	publish(State{Panes: panes, Stage: &Box{Pane: "p2"}, Focus: "p2"})
	if got := lastWindow.Offscreen().Title(); got != "kakel — files" {
		t.Fatalf("on a file pane, the window is called %q", got)
	}
	// A terminal the user named goes by that name.
	panes[0].Named = true
	publish(State{Panes: panes, Stage: &Box{Pane: "p1"}, Focus: "p1"})
	if got := lastWindow.Offscreen().Title(); got != "kakel — Terminal 1" {
		t.Fatalf("on a named terminal, the window is called %q", got)
	}
}
