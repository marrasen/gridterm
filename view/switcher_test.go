package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"

	"github.com/marrasen/kakel/vfs"
	"github.com/marrasen/kakel/vt"
)

// switcherStage puts three terminal panes in a window, p1 on stage, and
// opens the switcher over them.
func switcherStage(t *testing.T) *Window {
	t.Helper()
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	var panes []app.Pane
	for _, id := range []string{"p1", "p2", "p3"} {
		sh.Set(id, screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.Get(id).T.Close() })
		panes = append(panes, app.Pane{ID: id, Title: "Terminal " + id})
	}
	publish(app.State{Panes: panes, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	win.run("view.switcher", lastUI)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.sw == nil {
		t.Fatal("the switcher did not open")
	}
	return win
}

// Picking a pane, the others fade as it grows, rather than standing in
// their places until the overview goes.
func TestThePanesNotPickedFadeAsThePickGrows(t *testing.T) {
	win := switcherStage(t)
	sw := win.sw
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight})
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
	for i, tl := range sw.tiles {
		if i == sw.picked {
			continue
		}
		if a := tl.fade.Value(); a > 0.05 {
			t.Fatalf("a third of a second after the pick, %s is still at %.2f", tl.id, a)
		}
	}
}

// A pane that is no terminal shows in the switcher too, drawn small as
// it was last drawn.
func TestTheSwitcherShowsAFilePane(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p2", screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p2").T.Close() })
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "srv", Kind: app.KindFiles}, {ID: "p2", Title: "Terminal 2"}},
		Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/srv", Entries: []vfs.Entry{{Name: "a.txt"}, {Name: "b.txt"}}, Seq: 1}}})
	for range 10 {
		lastWindow.Frame(time.Second / 60)
	}
	if d := win.drawings["p1"]; d == nil || d.Recording().Empty() {
		t.Fatal("the file pane's drawing was not kept")
	}
	win.run("view.switcher", lastUI)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	small := false
	for _, op := range lastWindow.Offscreen().Ops() {
		if tx, ok := op.(*paint.TextOp); ok && tx.Transform.A < 0.9 && tx.Transform.A > 0 {
			small = true
		}
	}
	if !small {
		t.Fatal("the switcher shows no text drawn small: the file pane's tile is empty")
	}
}

// The pane picked is live the moment it is picked: it is asked onto the
// stage at once, with the keyboard, and grows while the switcher keeps
// the stage covered, and stays until it has grown.
func TestThePanePickedIsLiveAtOnce(t *testing.T) {
	win := switcherStage(t)
	sw := win.sw
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight})
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	in, ok := nextIntent(t).(app.FocusPane)
	if !ok || in.Pane != sw.tiles[sw.picked].id {
		t.Fatalf("at the pick, it asked for %#v", in)
	}
	// The stage stays covered while the pane grows: what grows is the
	// pane, not a copy over it.
	covered := false
	for _, op := range lastWindow.Offscreen().Ops() {
		if r, ok := op.(*paint.RRectOp); ok && r.Rect == sw.stageAt && r.Fill.Solid.A == 0xff {
			covered = true
		}
	}
	if sw.stageAt.Empty() || !covered {
		t.Fatalf("growing, the stage at %v is not covered", sw.stageAt)
	}
	if !sw.tiles[sw.picked].box.Active() || lastUI.Presence(sw) != gunim.Exiting {
		t.Fatal("the switcher went before the pane grew into place")
	}
}

// Opening, the stage is covered from the first frame: the tile standing
// where the pane stands is the pane, and the pane does not show beside
// it, fading.
func TestTheSwitcherCoversTheStageAtOnce(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}}})
	for range 5 {
		lastWindow.Frame(time.Second / 60)
	}
	win.openSwitcher(lastUI)
	lastWindow.Frame(time.Second / 60)
	sw := win.sw
	for _, op := range lastWindow.Offscreen().Ops() {
		if r, ok := op.(*paint.RRectOp); ok && r.Rect == sw.stageAt && r.Fill.Solid.A == 0xff {
			return
		}
	}
	t.Fatalf("on its first frame, the switcher leaves the stage at %v showing", sw.stageAt)
}

// While panes show their titles, a tile is the pane with its caption
// line: it starts where both stand, and lands where both will stand, so
// nothing jumps as the switcher hands the stage back.
func TestASwitcherTileCarriesThePanesCaption(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	var panes []app.Pane
	for _, id := range []string{"p1", "p2"} {
		sh.Set(id, screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.Get(id).T.Close() })
		panes = append(panes, app.Pane{ID: id, Title: "Terminal " + id})
	}
	st := app.State{Panes: panes, Stage: &app.Box{Pane: "p1"}, Focus: "p1", PaneTitles: true}
	publish(st)
	for range 5 {
		lastWindow.Frame(time.Second / 60)
	}
	slot, ok := lastUI.Bounds(win.captions["p1"])
	if !ok {
		t.Fatal("p1 has no caption line")
	}
	win.openSwitcher(lastUI)
	sw := win.sw
	if got := sw.tiles[0].box.Value(); got != slot || !sw.tiles[0].titled {
		t.Fatalf("p1's tile starts at %v, want its pane and caption at %v", got, slot)
	}
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	sw.pick(1, lastUI)
	st.Stage, st.Focus = &app.Box{Pane: "p2"}, "p2"
	publish(st)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	// p2 was never on stage before: its tile has the caption too, and
	// the terminal under it stands where the tile's pane landed.
	got := sw.tiles[1].box.Value()
	got.Min.Y += captionHeight
	want, _ := lastUI.Bounds(win.terms["p2"])
	if !sw.tiles[1].titled || got != want {
		t.Fatalf("p2's tile landed with its pane at %v (titled %v), but p2 stands at %v", got, sw.tiles[1].titled, want)
	}
}

// A file pane's caption names where it runs alone: the path line under
// it names the folder, and the two said the folder twice.
func TestAFilePanesCaptionNamesOnlyItsMachine(t *testing.T) {
	win, _, _ := windowStage(t)
	if got := win.captionOf(app.Pane{ID: "p1", Title: "tmp", Kind: app.KindFiles}); got != "This computer" {
		t.Fatalf("a file pane here is captioned %q", got)
	}
	if got := win.captionOf(app.Pane{ID: "p2", Title: "Terminal 1"}); got != "This computer: Terminal 1" {
		t.Fatalf("a terminal here is captioned %q", got)
	}
}
