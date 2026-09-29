package view

import (
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	gi "github.com/marrasen/gunim/input"
)

// The reader is made before the first read arrives, says how far it
// has got of the size listed, and asks for nothing more meanwhile.
func TestTheReaderShowsHowFarItsFirstReadHasGot(t *testing.T) {
	win, _, publish := windowStage(t)
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "big.log", Kind: app.KindReader}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"}
	st.Readers = map[string]app.Reader{"p1": {Path: "/x/big.log", Name: "big.log", Expect: 4 << 20}}
	publish(st)
	st.Readers = map[string]app.Reader{"p1": {Path: "/x/big.log", Name: "big.log", Expect: 4 << 20, SoFar: 1 << 20}}
	publish(st)
	rd := win.readers["p1"]
	if rd == nil || rd.r == nil || !rd.r.Busy() || rd.r.SoFar() != 1<<20 {
		t.Fatal("before its first read arrived, the reader says nothing of it")
	}
	_, rows := rd.g.Size()
	found := false
	for y := range rows {
		if row := gridRow(rd, y); strings.Contains(row, "4") && strings.Contains(row, "MB") {
			found = true
		}
	}
	if !found {
		t.Fatal("the reader does not say the size it was listed as")
	}
	for len(lastWindow.Client().Intents()) > 0 {
		if in, ok := (<-lastWindow.Client().Intents()).Intent.(app.ReadAgain); ok {
			t.Fatalf("the reader asked for a read with one on its way: %#v", in)
		}
	}
}

// Ctrl+F in the reader tells the program to follow, and again to stop.
func TestCtrlFInTheReaderTellsTheProgram(t *testing.T) {
	_, _, publish := windowStage(t)
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "log.txt", Kind: app.KindReader}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"}
	st.Readers = map[string]app.Reader{"p1": {Path: "/x/log.txt", Name: "log.txt", Lines: []string{"a"}, Seq: 1}}
	publish(st)
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	for _, want := range []bool{true, false} {
		lastWindow.Input(gi.KeyPress{Key: gi.KeyF, Mods: gi.ModControl})
		lastWindow.Frame(time.Second / 60)
		var got *app.FollowFile
		for got == nil {
			if in, ok := nextIntent(t).(app.FollowFile); ok {
				got = &in
			}
		}
		if got.Pane != "p1" || got.On != want {
			t.Fatalf("Ctrl+F sent %#v, want following %v", *got, want)
		}
	}
}

// A name the program offers to save under, after one was refused,
// reaches the reader.
func TestTheReaderTakesTheNameOfferedToSaveUnder(t *testing.T) {
	win, _, publish := windowStage(t)
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "log.txt", Kind: app.KindReader}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"}
	st.Readers = map[string]app.Reader{"p1": {Path: "/x/log.txt", Name: "log.txt", Lines: []string{"a"}, Seq: 1, SaveAs: "~/log.txt"}}
	publish(st)
	st.Readers = map[string]app.Reader{"p1": {Path: "/x/log.txt", Name: "log.txt", Lines: []string{"a"}, Seq: 1, SaveAs: "~/log 2.txt"}}
	publish(st)
	if got := win.readers["p1"].r.SaveAs; got != "~/log 2.txt" {
		t.Fatalf("the reader offers to save as %q", got)
	}
}
