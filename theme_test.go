package main

import (
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
)

// aThemedWindow is a window whose settings are in a file the test owns.
func aThemedWindow(t *testing.T) (*testApp, string) {
	t.Helper()
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := withSettings(t, a, path); err != nil {
		t.Fatalf("settings: %v", err)
	}
	return a, path
}

// themeNamed is one of the schemes on offer.
func themeNamed(t *testing.T, a *testApp, name string) themes.Theme {
	t.Helper()
	got, ok := themes.Named(a.theme.all(), name)
	if !ok {
		t.Fatalf("there is no scheme called %q among %v", name, themes.Names(a.theme.all()))
	}
	return got
}

// Taking a scheme draws the window in it: the window's own ground, the
// sidebar and the menu bar all change together.
func TestTakingASchemeRedrawsTheWindow(t *testing.T) {
	a, _ := aThemedWindow(t)
	paper := themeNamed(t, a, "Paper")
	want, err := paper.Palette()
	if err != nil {
		t.Fatalf("its palette: %v", err)
	}
	was := a.panel.Style

	if err := a.takeTheme(paper); err != nil {
		t.Fatalf("take it: %v", err)
	}

	if a.colours != want {
		t.Error("the window is not drawn in the scheme that was taken")
	}
	if a.g.DefaultBG != want.BG {
		t.Errorf("the window's ground is %v, want %v", a.g.DefaultBG, want.BG)
	}
	if a.panel.Style == was {
		t.Error("the sidebar kept the colours it had")
	}
	if a.bar.Style.FG != want.FG {
		t.Errorf("the menu bar writes in %v, want %v", a.bar.Style.FG, want.FG)
	}
}

// A pane takes the scheme too, so what the shell prints next is in it.
func TestAPaneTakesTheScheme(t *testing.T) {
	a, _ := aThemedWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	paper := themeNamed(t, a, "Paper")
	want, _ := paper.Palette()

	if err := a.takeTheme(paper); err != nil {
		t.Fatalf("take it: %v", err)
	}
	a.shells[0].out <- []byte("printed after the scheme changed")
	waitFor(t, a, "the shell to print", func() bool {
		return strings.Contains(paneText(pane), "printed after")
	})

	// The cell the text landed on is drawn in the new scheme.
	g := gridOfPane(t, pane)
	if got := g.At(0, 0).FG; got != want.FG {
		t.Errorf("what the shell printed is in %v, want %v", got, want.FG)
	}
	if got := g.At(0, 0).BG; got != want.BG {
		t.Errorf("it is on %v, want %v", got, want.BG)
	}
}

// What the shell printed before the change moves into the new scheme,
// so a light scheme does not leave the pane on a black ground.
func TestWhatWasPrintedBeforeMovesToTheNewScheme(t *testing.T) {
	a, _ := aThemedWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	was := a.colours
	paper := themeNamed(t, a, "Paper")
	want, _ := paper.Palette()
	a.shells[0].out <- []byte("printed before the scheme changed")
	waitFor(t, a, "the shell to print", func() bool {
		return strings.Contains(paneText(pane), "printed before")
	})

	if err := a.takeTheme(paper); err != nil {
		t.Fatalf("take it: %v", err)
	}

	c := gridOfPane(t, pane).At(0, 0)
	if c.FG != want.FG || c.BG != want.BG {
		t.Errorf("what was printed before is %v on %v, want %v on %v (it was printed in %v on %v)",
			c.FG, c.BG, want.FG, want.BG, was.FG, was.BG)
	}
}

// A named colour a pane printed moves to the same name in the new
// scheme. Two things used to break this with the real schemes: a scheme
// repeats some of its named colours further up the 256, and its ground
// colour is usually one of them.
func TestANamedColourInAPaneMovesToTheSameName(t *testing.T) {
	a, _ := aThemedWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	if err := a.takeTheme(themeNamed(t, a, "Contrast")); err != nil {
		t.Fatalf("take Contrast: %v", err)
	}
	// Red, which Contrast also holds at entry 203, and black, which is
	// Contrast's own ground colour.
	a.shells[0].out <- []byte("\x1b[31mr\x1b[30mk\x1b[0m")
	waitFor(t, a, "the shell to print", func() bool {
		return strings.Contains(paneText(pane), "rk")
	})
	paper := themeNamed(t, a, "Paper")
	want, _ := paper.Palette()

	if err := a.takeTheme(paper); err != nil {
		t.Fatalf("take Paper: %v", err)
	}

	g := gridOfPane(t, pane)
	if got := g.At(0, 0).FG; got != want.ANSI[1] {
		t.Errorf("the red is %v, want Paper's red %v", got, want.ANSI[1])
	}
	if got := g.At(1, 0).FG; got != want.ANSI[0] {
		t.Errorf("the black text is %v, want Paper's black %v", got, want.ANSI[0])
	}
}

// The choice is written down, so the next run opens on it.
func TestTheSchemeIsRememberedBetweenRuns(t *testing.T) {
	a, path := aThemedWindow(t)

	if err := a.takeTheme(themeNamed(t, a, "Paper")); err != nil {
		t.Fatalf("take it: %v", err)
	}

	again, err := settings.Load(path)
	if err != nil {
		t.Fatalf("read the settings again: %v", err)
	}
	if got, picked := again.Theme(); !picked || got != "Paper" {
		t.Errorf("the file says %q (%v)", got, picked)
	}
}

// A window opens on the scheme it was left in.
func TestAWindowOpensOnTheSchemeItWasLeftIn(t *testing.T) {
	a, _ := aThemedWindow(t)
	if err := a.theme.choose("Contrast"); err != nil {
		t.Fatalf("choose: %v", err)
	}

	got := a.startTheme()

	if got.Name != "Contrast" {
		t.Errorf("it opens on %q", got.Name)
	}
}

// One left in a scheme that has since gone from the file opens on the
// first, and says so rather than changing colour with no word about it.
func TestASchemeThatHasGoneIsSaidSo(t *testing.T) {
	a, _ := aThemedWindow(t)
	if err := a.theme.choose("Written by hand"); err != nil {
		t.Fatalf("choose: %v", err)
	}

	got := a.startTheme()

	if want := themes.Built()[0].Name; got.Name != want {
		t.Errorf("it opens on %q, want %q", got.Name, want)
	}
	if !a.logged.holds("Written by hand") {
		t.Error("nothing said which scheme had gone")
	}
}

// The schemes in the user's own file are offered after the built-in
// ones.
func TestTheFilesSchemesAreOffered(t *testing.T) {
	a, _ := aThemedWindow(t)
	dir := t.TempDir()
	body := `{"version":1,"themes":[{"name":"Mine","fg":"#fff","bg":"#000",` +
		`"ansi":["#000","#100","#200","#300","#400","#500","#600","#700",` +
		`"#800","#900","#a00","#b00","#c00","#d00","#e00","#f00"]}]}`
	if err := os.WriteFile(themes.Path(dir), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	all, err := themes.Load(themes.Path(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	a.theme.take(all)

	if err := a.openThemePick(); err != nil {
		t.Fatalf("open the picker: %v", err)
	}
	c := awaitModal(t, a, "the scheme picker", byTitle[*ui.Chooser](themeTitle))

	var offered []string
	for _, row := range c.Rows() {
		offered = append(offered, strings.TrimSpace(row.Text))
	}
	if !slices.Contains(offered, "Mine") {
		t.Errorf("it offers %v, want the scheme from the file", offered)
	}
}

// Picking one from the list draws the window in it.
func TestPickingASchemeFromTheListTakesIt(t *testing.T) {
	a, _ := aThemedWindow(t)
	want, err := themeNamed(t, a, "Paper").Palette()
	if err != nil {
		t.Fatalf("its palette: %v", err)
	}

	if err := a.openThemePick(); err != nil {
		t.Fatalf("open the picker: %v", err)
	}
	c := awaitModal(t, a, "the scheme picker", byTitle[*ui.Chooser](themeTitle))
	takeChoice(t, c, "Paper")

	if a.colours != want {
		t.Error("the window is not drawn in the scheme that was picked")
	}
}

// A scheme that could not be written down is still drawn: the window is
// in it, and the reason is what the user needs.
func TestASchemeThatCannotBeRememberedIsStillDrawn(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	a.theme.remember(settings.Unusable(errors.New("the settings file is unreadable")))
	paper := themeNamed(t, a, "Paper")
	want, _ := paper.Palette()

	err := a.takeTheme(paper)

	if err == nil {
		t.Fatal("it said nothing about a choice it could not write")
	}
	if a.colours != want {
		t.Error("the window is not drawn in the scheme that was taken")
	}
}

// gridOfPane is what a pane drew, as a grid a test can read cells from.
func gridOfPane(t *testing.T, pane *term.Terminal) *grid.Grid {
	t.Helper()
	size := pane.Size()
	g := grid.New(size.Cols, size.Rows, color.RGBA{}, color.RGBA{})
	pane.Draw(g.View())
	return g
}

// Clearing the screen after a change paints the new scheme's ground,
// because what a cleared cell is made of comes from the scheme too.
func TestClearingAfterASchemeChangePaintsTheNewGround(t *testing.T) {
	a, _ := aThemedWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	paper := themeNamed(t, a, "Paper")
	want, _ := paper.Palette()

	if err := a.takeTheme(paper); err != nil {
		t.Fatalf("take it: %v", err)
	}
	// Erase the whole screen, the way clear does.
	a.shells[0].out <- []byte("\x1b[2J")
	waitFor(t, a, "the screen to be cleared", func() bool {
		return gridOfPane(t, pane).At(5, 5).BG == want.BG
	})

	if got := gridOfPane(t, pane).At(5, 5).BG; got != want.BG {
		t.Errorf("a cleared cell is on %v, want %v", got, want.BG)
	}
}

// A pane made taller after a change fills the new rows with the new
// scheme's ground, rather than the one it was opened in.
func TestRowsAddedAfterASchemeChangeAreInIt(t *testing.T) {
	a, _ := aThemedWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	was := pane.Size()
	paper := themeNamed(t, a, "Paper")
	want, _ := paper.Palette()

	if err := a.takeTheme(paper); err != nil {
		t.Fatalf("take it: %v", err)
	}
	pane.Layout(ui.Size{Cols: was.Cols, Rows: was.Rows + 6})

	// The rows that were not there before.
	if got := gridOfPane(t, pane).At(0, was.Rows+2).BG; got != want.BG {
		t.Errorf("a row added after the change is on %v, want %v", got, want.BG)
	}
}

// Every scheme reads. The window is written in colours derived from the
// scheme's own two ends, and a scheme with a light ground broke widgets
// that took a numbered colour for "just off the background".
func TestEverySchemeReads(t *testing.T) {
	for _, theme := range themes.Built() {
		pal, err := theme.Palette()
		if err != nil {
			t.Errorf("%s: %v", theme.Name, err)
			continue
		}
		a := newTestApp(t, 80, 24)
		withDialogs(t, a)
		withPanel(t, a)
		withMenubar(t, a)
		a.colours = pal
		a.restyle()

		// Text on a ground has to be readable. 4.5 is what WCAG asks of
		// body text; these are short labels on a terminal, so the bar is
		// the 3 it asks of large text.
		const least = 3.0
		for _, pair := range []struct {
			what   string
			fg, bg color.RGBA
		}{
			{"a dialog's text field", a.formStyle().FieldFG, a.formStyle().FieldBG},
			{"a dialog's button", a.formStyle().ButtonFG, a.formStyle().ButtonBG},
			{"a notice's button", a.noticeStyle().ButtonFG, a.noticeStyle().ButtonBG},
			{"a sidebar row", a.panelStyle().FG, a.panelStyle().BG},
			{"the menu bar", a.menubarStyle().FG, a.menubarStyle().BG},
			{"the list a walk shows", pal.FG, pal.Surface()},
			{"a question on a pane", pal.FG, pal.Surface()},
			{"selected text", pal.FG, pal.Selection},
		} {
			if got := grid.Contrast(pair.fg, pair.bg); got < least {
				t.Errorf("%s: %s is %v on %v, %.2f:1, want at least %.1f",
					theme.Name, pair.what, pair.fg, pair.bg, got, least)
			}
		}
		// And the two the menu bar says different things in have to be
		// told apart.
		taken, idle := statusTakenFG(pal), statusIdleFG(pal)
		if got := grid.Contrast(taken, idle); got < 1.2 {
			t.Errorf("%s: a window being driven is %v and one only listening is %v, %.2f:1",
				theme.Name, taken, idle, got)
		}
		// The window writes its own labels in numbered colours, so those
		// have to read on the window's ground and not only in a pane.
		for _, at := range []int{1, 3, 4, 5, 6} {
			if got := grid.Contrast(pal.ANSI[at], pal.BG); got < least {
				t.Errorf("%s: colour %d is %v on the ground %v, %.2f:1, want at least %.1f",
					theme.Name, at, pal.ANSI[at], pal.BG, got, least)
			}
		}
		// Colour 8 is the dim one: notes, hints, field labels and the
		// keys beside a menu item. Dimmer than the rest on purpose, and
		// still above whatever it is read on. A surface is already a
		// step off the ground, so the bar there is lower.
		for _, on := range []struct {
			what  string
			bg    color.RGBA
			least float64
		}{
			{"the ground", pal.BG, 2.5},
			{"a surface", pal.Surface(), 2.0},
		} {
			if got := grid.Contrast(pal.ANSI[8], on.bg); got < on.least {
				t.Errorf("%s: the dim colour is %v on %s %v, %.2f:1, want at least %.1f",
					theme.Name, pal.ANSI[8], on.what, on.bg, got, on.least)
			}
		}
		// The sidebar and the menu bar are shaded towards colour 4, so a
		// scheme whose ground is already that colour has no frame at all.
		if got := grid.Contrast(sidebarFoot(pal), pal.BG); got < 1.1 {
			t.Errorf("%s: the window's frame is %v on a ground of %v, %.2f:1, and it has to read as a frame",
				theme.Name, sidebarFoot(pal), pal.BG, got)
		}
		// A chip on the menu bar picks its own ground, so what it says
		// has to read on that rather than on the bar.
		for what, fg := range map[string]color.RGBA{
			"a window only listening": idle,
			"a window being driven":   taken,
			"an agent in this window": statusAgentFG(pal),
		} {
			if got := grid.Contrast(fg, chipBG(pal)); got < 4.5 {
				t.Errorf("%s: %s is %v on the chip's %v, %.2f:1, want at least 4.5",
					theme.Name, what, fg, chipBG(pal), got)
			}
		}
	}
}

// The chips on the menu bar take the scheme. They are built only when
// what they say changes, and a scheme is not part of that, so a window
// left them in the colours of the scheme before.
func TestTheMenuBarChipsTakeTheScheme(t *testing.T) {
	a := aBarWindow(t)
	withServing(t, a, aPublicKey(t, "marcus@laptop"))
	if err := a.startServing("0", whereHere); err != nil {
		t.Fatalf("serve: %v", err)
	}
	a.updateStatus()
	if len(a.bar.Chips) != 1 {
		t.Fatalf("the bar has %d chips, want the one saying it is serving", len(a.bar.Chips))
	}

	if err := a.useTheme(themeNamed(t, a, "Paper")); err != nil {
		t.Fatalf("take it: %v", err)
	}

	if got, want := a.bar.Chips[0].FG, statusIdleFG(a.colours); got != want {
		t.Errorf("the chip is written in %v, want %v", got, want)
	}
	if got, want := a.bar.Chips[0].BG, chipBG(a.colours); got != want {
		t.Errorf("the chip sits on %v, want %v", got, want)
	}
}

// The pinned row above the sidebar and every divider already drawn take
// the scheme, so a window does not end up with two kinds of divider in
// it.
func TestThePinnedRowAndTheDividersTakeTheScheme(t *testing.T) {
	a, _ := aThemedWindow(t)
	if err := a.openPane(); err != nil {
		t.Fatalf("open a second pane: %v", err)
	}
	takeChoice(t, splitChoices(t, a, ui.Columns), "Move")
	var split *ui.Split
	for _, w := range ui.Leaves(a.root.Widget()) {
		for at := ui.ParentOf(a.root.Widget(), w); at != nil; at = ui.ParentOf(a.root.Widget(), at) {
			if s, is := at.(*ui.Split); is {
				split = s
			}
		}
	}
	if split == nil {
		t.Fatal("the split is not in the tree")
	}
	want, err := themeNamed(t, a, "Paper").Palette()
	if err != nil {
		t.Fatalf("its palette: %v", err)
	}

	if err := a.useTheme(themeNamed(t, a, "Paper")); err != nil {
		t.Fatalf("take it: %v", err)
	}

	if split.DividerBG != want.BG {
		t.Errorf("the divider between two panes is on %v, want %v", split.DividerBG, want.BG)
	}
	if a.dock.DividerBG != want.BG {
		t.Errorf("the divider beside the sidebar is on %v, want %v", a.dock.DividerBG, want.BG)
	}
	if a.side.FG != want.ANSI[6] {
		t.Errorf("the pinned row is written in %v, want %v", a.side.FG, want.ANSI[6])
	}
	if a.side.BG != a.panelStyle().BGEnd {
		t.Errorf("the pinned row sits on %v, want the sidebar's own ground %v",
			a.side.BG, a.panelStyle().BGEnd)
	}
}

// Reloading reads the file again, so a scheme can be written with the
// window open.
func TestReloadingReadsTheFileAgain(t *testing.T) {
	a, _ := aThemedWindow(t)
	dir := t.TempDir()
	a.theme.at(dir)
	writeThemes(t, dir, "Mine", "#ffffff")

	if err := a.reloadThemes(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	if _, ok := themes.Named(a.theme.all(), "Mine"); !ok {
		t.Errorf("it offers %v, want the scheme written while the window was open",
			themes.Names(a.theme.all()))
	}
}

// And a window drawn in a scheme that has just been edited is redrawn in
// it, rather than keeping the colours it read the first time.
func TestReloadingRedrawsTheWindowInTheSchemeAsItIsNow(t *testing.T) {
	a, _ := aThemedWindow(t)
	dir := t.TempDir()
	a.theme.at(dir)
	writeThemes(t, dir, "Mine", "#ffffff")
	if err := a.reloadThemes(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := a.useTheme(themeNamed(t, a, "Mine")); err != nil {
		t.Fatalf("take it: %v", err)
	}
	writeThemes(t, dir, "Mine", "#ff0000")

	if err := a.reloadThemes(); err != nil {
		t.Fatalf("reload again: %v", err)
	}

	if want := (color.RGBA{0xff, 0, 0, 0xff}); a.colours.FG != want {
		t.Errorf("the window writes in %v, want the %v the file says now", a.colours.FG, want)
	}
}

// A window drawn in a scheme the file no longer holds falls back to the
// one it would open on, rather than keeping colours nothing describes.
func TestReloadingAfterASchemeIsDeletedFallsBack(t *testing.T) {
	a, _ := aThemedWindow(t)
	dir := t.TempDir()
	a.theme.at(dir)
	writeThemes(t, dir, "Mine", "#ffffff")
	if err := a.reloadThemes(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := a.useTheme(themeNamed(t, a, "Mine")); err != nil {
		t.Fatalf("take it: %v", err)
	}
	if err := os.Remove(themes.Path(dir)); err != nil {
		t.Fatalf("delete the file: %v", err)
	}

	if err := a.reloadThemes(); err != nil {
		t.Fatalf("reload again: %v", err)
	}

	if want := a.startTheme().Name; a.theme.showing != want {
		t.Errorf("the window is drawn in %q, want %q", a.theme.showing, want)
	}
}

// A start file holds the scheme the window is in, and the window says
// where it went.
func TestWritingAStartFileSaysWhereItWent(t *testing.T) {
	a, _ := aThemedWindow(t)
	dir := t.TempDir()
	a.theme.at(dir)
	if err := a.useTheme(themeNamed(t, a, "Paper")); err != nil {
		t.Fatalf("take it: %v", err)
	}

	if err := a.writeThemeStart(); err != nil {
		t.Fatalf("write it: %v", err)
	}

	all, err := themes.Load(themes.Path(dir))
	if err != nil {
		t.Fatalf("read it back: %v", err)
	}
	if len(all) != len(themes.Built())+1 {
		t.Errorf("the file gave %v", themes.Names(all))
	}
	n := awaitModal(t, a, "the notice", byTitlePrefix[*ui.Notice]("Wrote "))
	if !strings.Contains(n.Title, themes.Path(dir)) {
		t.Errorf("it says %q, want the path it wrote", n.Title)
	}
}

// And a file that is already there is not written over, because what is
// in one is the user's.
func TestWritingAStartFileDoesNotWriteOverOne(t *testing.T) {
	a, _ := aThemedWindow(t)
	dir := t.TempDir()
	a.theme.at(dir)
	writeThemes(t, dir, "Mine", "#ffffff")
	if err := a.useTheme(themeNamed(t, a, "Paper")); err != nil {
		t.Fatalf("take it: %v", err)
	}

	err := a.writeThemeStart()

	if err == nil {
		t.Fatal("it wrote over the file")
	}
	if !strings.Contains(err.Error(), themes.Path(dir)) {
		t.Errorf("it says %q, want the path it would not write", err)
	}
}

// writeThemes puts a themes file holding one scheme in a directory.
func writeThemes(t *testing.T, dir, name, fg string) {
	t.Helper()
	body := `{"version":1,"themes":[{"name":"` + name + `","fg":"` + fg + `","bg":"#000",` +
		`"ansi":["#000","#100","#200","#300","#400","#500","#600","#700",` +
		`"#800","#900","#a00","#b00","#c00","#d00","#e00","#f00"]}]}`
	if err := os.WriteFile(themes.Path(dir), []byte(body), 0o600); err != nil {
		t.Fatalf("write the themes: %v", err)
	}
}
