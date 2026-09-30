package view

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/vt"
)

// The switcher's shortcut closes it again.
func TestTheSwitcherShortcutClosesItAgain(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}}})
	press := func(k gi.Key) {
		lastWindow.Input(gi.KeyPress{Key: k, Mods: gi.ModControl | gi.ModShift, Time: time.Now()})
		for range 3 {
			lastWindow.Frame(time.Second / 60)
		}
	}
	press(gi.KeyA)
	if win.sw == nil {
		t.Fatal("the switcher's shortcut did not open it")
	}
	// Held down, the key repeats, and the switcher stays.
	lastWindow.Input(gi.KeyPress{Key: gi.KeyA, Mods: gi.ModControl | gi.ModShift, Repeat: true, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if win.sw == nil {
		t.Fatal("the key repeating closed the switcher")
	}
	press(gi.KeyA)
	if win.sw != nil {
		t.Fatal("the switcher's shortcut again left it open")
	}
}

// The sidebar's shortcut opens and closes the Servers pane, in full
// screen too.
func TestTheSidebarShortcutTogglesServers(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}}})
	win.present(true, lastUI)
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.run("sidebar.toggle", lastUI)
	if in := nextIntent(t); in != (app.ToggleServers{}) || !win.presenting {
		t.Fatalf("the shortcut sent %#v, full screen %v", in, win.presenting)
	}
}

// Close Selected Row with no row, or one that cannot be closed, says
// so rather than doing nothing.
func TestCloseSelectedRowSaysWhyItDidNothing(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(withServers(app.State{Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}}}))
	was := win.toasts.Len()
	win.run("sidebar.closeRow", lastUI)
	if win.toasts.Len() != was+1 {
		t.Fatal("with no row selected, nothing was said")
	}
	row, ok := widget.RowOf[*sideRow](win.list, widget.Key("machine:"))
	if !ok {
		t.Fatalf("no heading for this computer in %v", win.list.Keys())
	}
	lastUI.Focus(row)
	if lastUI.Focused() != row {
		t.Fatal("the heading took no focus")
	}
	win.run("sidebar.closeRow", lastUI)
	if win.toasts.Len() != was+2 {
		t.Fatal("on a heading, nothing was said")
	}
}

// On "Replace notes.txt?", Tab then Enter leaves the file, as Enter
// alone does: Tab reaches Leave It before Replace.
func TestTabThenEnterLeavesTheFile(t *testing.T) {
	_, _, publish := windowStage(t)
	publish(app.State{Asks: []app.Ask{{ID: 1, Title: "Replace notes.txt?", Text: "x", Choose: []string{"Leave It", "Replace"}, FirstIsSafe: true, Also: "Do the same for the rest", No: "Stop"}}})
	for range 5 {
		lastWindow.Frame(time.Second / 60)
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyTab, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	in, ok := nextIntent(t).(app.AskAnswered)
	if !ok || len(in.Answers) == 0 || in.Answers[0] != "Leave It" {
		t.Fatalf("Tab then Enter sent %#v", in)
	}
}

// The palette's shortcut closes it again, from inside it.
func TestThePaletteShortcutClosesItAgain(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}}})
	press := func() {
		lastWindow.Input(gi.KeyPress{Key: gi.KeyK, Mods: gi.ModControl | gi.ModShift, Time: time.Now()})
		for range 3 {
			lastWindow.Frame(time.Second / 60)
		}
	}
	press()
	if !win.palette.IsOpen() {
		t.Fatal("the palette's shortcut did not open it")
	}
	press()
	if win.palette.IsOpen() {
		t.Fatal("the palette's shortcut again left it open")
	}
}

// The palette ticks a switch while it is on, as the View menu does.
func TestThePaletteTicksASwitchThatIsOn(t *testing.T) {
	win, _, publish := windowStage(t)
	ticked := func() bool {
		for i, id := range win.paletteIDs {
			if id == "sidebar.toggle" {
				return win.palette.Items[i].Checked
			}
		}
		t.Fatal("no Servers in the palette")
		return false
	}
	publish(app.State{AllPanes: []app.Pane{{ID: "ps", Kind: app.KindServers}}})
	if !ticked() {
		t.Fatal("with the Servers pane open, the palette leaves it unticked")
	}
	publish(app.State{})
	if ticked() {
		t.Fatal("with the Servers pane closed, the palette ticks it")
	}
}

// A paste that finds the clipboard cannot be read says so, rather than
// taking it for a clipboard with nothing on it.
func TestAnUnreadableClipboardIsSaid(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	lastWindow.Offscreen().SetClipboardError(errors.New("no display"))
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.terms["p1"].pasteClipboard(lastUI)
	if in, ok := nextIntent(t).(app.ClipboardUnreadable); !ok || in.Why != "No display." {
		t.Fatalf("the paste sent %#v", in)
	}
}

// A question with preformatted text keeps its lines whole, in the
// fixed-width face, and lets them be selected.
func TestAPreformattedQuestionKeepsItsLinesWhole(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Asks: []app.Ask{{ID: 1, Title: "api-key", Text: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGx0 me@desk", Preformatted: true, Yes: "Done"}}})
	var note *widget.Label
	for _, f := range formOf(win.dialog.Body).Children() {
		if l, ok := f.(*widget.Label); ok && strings.HasPrefix(l.Text, "ssh-ed25519") {
			note = l
		}
	}
	if note == nil || !note.NoWrap || !note.Selectable || note.Face.Key() != widget.MonoFont.Key() {
		t.Fatalf("the text is shown as %+v", note)
	}
}

// The divider between two file panes drags, as any split's does, and
// the new share goes to the program.
func TestTheDividerBetweenFilePanesDrags(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}, {ID: "p2", Title: "b", Kind: app.KindFiles}},
		Stage: &app.Box{ID: "s1", A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}, Share: 0.5}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}, "p2": {Path: "/", Seq: 1}}})
	for range 10 {
		lastWindow.Frame(time.Second / 60)
	}
	sp := win.splits["s1"]
	size := lastWindow.Offscreen().Size()
	y := size.H / 2
	for x := float32(0); x < size.W; x += 2 {
		lastWindow.Input(gi.PointerMove{Pos: geom.Pt(x, y)})
		lastWindow.Input(gi.PointerDown{Pos: geom.Pt(x, y), Button: gi.ButtonPrimary, Clicks: 1, Time: time.Now()})
		lastWindow.Frame(time.Second / 60)
		if !sp.Held() {
			lastWindow.Input(gi.PointerUp{Pos: geom.Pt(x, y), Button: gi.ButtonPrimary})
			lastWindow.Frame(time.Second / 60)
			continue
		}
		for len(lastWindow.Client().Intents()) > 0 {
			<-lastWindow.Client().Intents()
		}
		lastWindow.Input(gi.PointerMove{Pos: geom.Pt(x+60, y)})
		lastWindow.Input(gi.PointerUp{Pos: geom.Pt(x+60, y), Button: gi.ButtonPrimary})
		lastWindow.Frame(time.Second / 60)
		if in, ok := nextIntent(t).(app.SplitMoved); !ok || in.Split != "s1" || in.Share <= 0.5 {
			t.Fatalf("dragged right, the divider sent %#v", in)
		}
		return
	}
	t.Fatal("no divider to take hold of between the file panes")
}

// The switcher's ground is solid once it is in: the panes show only as
// its tiles, not behind them as well.
func TestTheSwitcherHidesThePanesBehindIt(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}}})
	win.openSwitcher(lastUI)
	for range 90 {
		lastWindow.Frame(time.Second / 60)
	}
	// The last rectangle the size of the window is the switcher's ground,
	// painted over everything else.
	size := lastWindow.Offscreen().Size()
	var ground *paint.RRectOp
	for _, op := range lastWindow.Offscreen().Ops() {
		if r, ok := op.(*paint.RRectOp); ok && r.Rect.Size() == size {
			ground = r
		}
	}
	if ground == nil || ground.Fill.Solid.A != 0xff {
		t.Fatalf("the ground under the switcher's tiles is %+v", ground)
	}
}

// A pane picked in the switcher that sits in a split grows into its
// place in that split, and the pane beside it into its own, rather than
// filling the stage and then giving way to the split.
func TestAPanePickedInASplitGrowsIntoItsPlace(t *testing.T) {
	win, _, publish := windowStage(t)
	split := &app.Box{ID: "s1", A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}, Share: 0.5}
	alone := &app.Box{Pane: "p3"}
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFiles}, {ID: "p2", Title: "b", Kind: app.KindFiles}, {ID: "p3", Title: "c", Kind: app.KindFiles}},
		Stage: alone, Focus: "p3", Groups: map[string]*app.Box{"p1": split, "p2": split, "p3": alone},
		Browsers: map[string]app.Browser{"p1": {Path: "/", Seq: 1}, "p2": {Path: "/", Seq: 1}, "p3": {Path: "/", Seq: 1}}})
	for range 5 {
		lastWindow.Frame(time.Second / 60)
	}
	win.openSwitcher(lastUI)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	s := win.sw
	stage, _ := lastUI.Bounds(win.stage)
	s.pick(1, lastUI)
	half := func(id string) geom.Rect {
		r, _ := placeIn(split, id, stage, lastUI)
		return r
	}
	if got := s.tiles[1].box.Target(); got != half("p2") || got.Size().W >= stage.Size().W {
		t.Fatalf("picked, p2 grows to %v, want its half %v of the stage %v", got, half("p2"), stage)
	}
	if !s.mates[s.tiles[0]] || s.tiles[0].box.Target() != half("p1") {
		t.Fatalf("p1, beside it, goes to %v, want %v", s.tiles[0].box.Target(), half("p1"))
	}
	if s.mates[s.tiles[2]] {
		t.Fatal("p3, in no split with p2, grows with it")
	}
}

// The cursor hides while the window is without the keyboard, and shows
// again once it is back.
func TestTheCursorHidesWhileTheWindowIsInactive(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	for range 5 {
		lastWindow.Frame(time.Second / 60)
	}
	term := win.terms["p1"]
	if !term.cursorShown {
		t.Fatal("with the keyboard, the pane shows no cursor")
	}
	lastWindow.Input(gi.WindowFocusLost{})
	lastWindow.Frame(time.Second / 60)
	if term.cursorShown {
		t.Fatal("with the window in the background, the pane still shows its cursor")
	}
	lastWindow.Input(gi.WindowFocusGained{})
	lastWindow.Frame(time.Second / 60)
	if !term.cursorShown {
		t.Fatal("with the keyboard back, the pane shows no cursor")
	}
}

// Always on Top keeps the window above the others, ticked in the View
// menu while it does, and the title bar has the pin for it.
func TestAlwaysOnTopPinsTheWindow(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{})
	win.run("view.pin", lastUI)
	if !lastUI.Pinned() || !lastWindow.Offscreen().Pinned() {
		t.Fatal("Always on Top left the window among the others")
	}
	ticked := false
	for m := range menus {
		for i, it := range menus[m].items {
			if it.id == "view.pin" {
				ticked = win.bar.Menus[m].Checked[i]
			}
		}
	}
	if !ticked {
		t.Fatal("pinned, the View menu does not tick Always on Top")
	}
	win.run("view.pin", lastUI)
	if lastUI.Pinned() {
		t.Fatal("Always on Top again left the window pinned")
	}
	pin := false
	var walk func(n gunim.Node)
	walk = func(n gunim.Node) {
		if c, ok := n.(*widget.WindowControls); ok && c.Pin {
			pin = true
		}
		if c, ok := n.(gunim.Composite); ok {
			for _, k := range c.Children() {
				walk(k)
			}
		}
	}
	walk(win.top)
	if !pin {
		t.Fatal("the title bar has no pin")
	}
}

// The theme picker shows the theme its highlight moves to, not the one
// it opens on, and closed with nothing picked, ends the preview.
func TestTheThemePickerPreviewsWhatItIsOn(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Themes: []string{"Dark", "Paper", "Ink"}, Theme: "Dark"})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	win.pickTheme(lastUI)
	for range 10 {
		lastWindow.Frame(time.Second / 60)
	}
	if len(lastWindow.Client().Intents()) != 0 {
		t.Fatalf("opened, the picker already sent %#v", nextIntent(t))
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyDown, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if in, ok := nextIntent(t).(app.PreviewTheme); !ok || in.Name != "Paper" {
		t.Fatalf("down one, the picker sent %#v", in)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEscape, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if in, ok := nextIntent(t).(app.PreviewTheme); !ok || in.Name != "" {
		t.Fatalf("closed, the picker sent %#v", in)
	}
}

// A dialog whose form is taller than the window fits in it, the form
// scrolling in the room the dialog can spare, and Tab still reaches
// its fields.
func TestATallDialogFitsTheWindowAndScrolls(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{})
	form := widget.NewForm()
	for range 40 {
		form.Add("Field", widget.NewTextField())
	}
	d := widget.NewDialog("Tall")
	d.Body = form
	d.SetButtons("OK", "Cancel")
	win.openDialog(d, lastUI)
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	s, ok := d.Body.(*scrollingForm)
	if !ok {
		t.Fatalf("the form is shown as %T", d.Body)
	}
	at, ok := lastUI.Bounds(s)
	size := lastWindow.Offscreen().Size()
	if !ok || at.Min.Y < 0 || at.Max.Y > size.H {
		t.Fatalf("the form stands at %v in a window %v high", at, size.H)
	}
	if len(s.Focusables()) != 40 {
		t.Fatalf("Tab reaches %d of the 40 fields", len(s.Focusables()))
	}
	fields := s.Focusables()
	// The wheel over it scrolls it, and it stays scrolled.
	before, _ := lastUI.Bounds(fields[0])
	lastWindow.Input(gi.PointerMove{Pos: at.Center()})
	lastWindow.Input(gi.Scroll{Pos: at.Center(), Delta: geom.Pt(0, -120)})
	for range 40 {
		lastWindow.Frame(time.Second / 60)
	}
	if after, _ := lastUI.Bounds(fields[0]); after.Min.Y > before.Min.Y-100 {
		t.Fatalf("the wheel moved the first field from %v to %v", before.Min.Y, after.Min.Y)
	}
	// Tab to a field out of sight brings it into view.
	lastUI.Focus(fields[0])
	for range 30 {
		lastWindow.Input(gi.KeyPress{Key: gi.KeyTab, Time: time.Now()})
	}
	for range 40 {
		lastWindow.Frame(time.Second / 60)
	}
	if got, ok := lastUI.Bounds(fields[30]); !ok || got.Min.Y < at.Min.Y || got.Max.Y > at.Max.Y {
		t.Fatalf("Tab to field 31 left it at %v, out of the form's view %v", got, at)
	}
}
