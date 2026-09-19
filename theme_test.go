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
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/themes"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
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

// themeNamed is one of the themes on offer.
func themeNamed(t *testing.T, a *testApp, name string) themes.Theme {
	t.Helper()
	got, ok := themes.Named(a.theme.all(), name)
	if !ok {
		t.Fatalf("there is no theme called %q among %v", name, themes.Names(a.theme.all()))
	}
	return got
}

// Taking a theme draws the window in it: the window's own ground, the
// sidebar and the menu bar all change together.
func TestTakingAThemeRedrawsTheWindow(t *testing.T) {
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
		t.Error("the window is not drawn in the theme that was taken")
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

// A pane takes the theme too, so what the shell prints next is in it.
func TestAPaneTakesTheTheme(t *testing.T) {
	a, _ := aThemedWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	paper := themeNamed(t, a, "Paper")
	want, _ := paper.Palette()

	if err := a.takeTheme(paper); err != nil {
		t.Fatalf("take it: %v", err)
	}
	a.shells[0].out <- []byte("printed after the theme changed")
	waitFor(t, a, "the shell to print", func() bool {
		return strings.Contains(paneText(pane), "printed after")
	})

	// The cell the text landed on is drawn in the new theme.
	g := gridOfPane(t, pane)
	if got := g.At(0, 0).FG; got != want.FG {
		t.Errorf("what the shell printed is in %v, want %v", got, want.FG)
	}
	if got := g.At(0, 0).BG; got != want.BG {
		t.Errorf("it is on %v, want %v", got, want.BG)
	}
}

// What the shell printed before the change moves into the new theme,
// so a light theme does not leave the pane on a black ground.
func TestWhatWasPrintedBeforeMovesToTheNewTheme(t *testing.T) {
	a, _ := aThemedWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	was := a.colours
	paper := themeNamed(t, a, "Paper")
	want, _ := paper.Palette()
	a.shells[0].out <- []byte("printed before the theme changed")
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
// theme. Two things used to break this with the real themes: a theme
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
func TestTheThemeIsRememberedBetweenRuns(t *testing.T) {
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

// A window opens on the theme it was left in.
func TestAWindowOpensOnTheThemeItWasLeftIn(t *testing.T) {
	a, _ := aThemedWindow(t)
	if err := a.theme.choose("Contrast"); err != nil {
		t.Fatalf("choose: %v", err)
	}

	got := a.startTheme()

	if got.Name != "Contrast" {
		t.Errorf("it opens on %q", got.Name)
	}
}

// One left in a theme that has since gone from the file opens on the
// first, and says so rather than changing colour with no word about it.
func TestAThemeThatHasGoneIsSaidSo(t *testing.T) {
	a, _ := aThemedWindow(t)
	if err := a.theme.choose("Written by hand"); err != nil {
		t.Fatalf("choose: %v", err)
	}

	got := a.startTheme()

	if want := themes.Built()[0].Name; got.Name != want {
		t.Errorf("it opens on %q, want %q", got.Name, want)
	}
	if !a.logged.holds("Written by hand") {
		t.Error("nothing said which theme had gone")
	}
}

// The themes in the user's own file are offered after the built-in
// ones.
func TestTheFilesThemesAreOffered(t *testing.T) {
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
	c := awaitModal(t, a, "the theme picker", byTitle[*ui.Chooser](themeTitle))

	var offered []string
	for _, row := range c.Rows() {
		offered = append(offered, strings.TrimSpace(row.Text))
	}
	if !slices.Contains(offered, "Mine") {
		t.Errorf("it offers %v, want the theme from the file", offered)
	}
}

// Picking one from the list draws the window in it.
func TestPickingAThemeFromTheListTakesIt(t *testing.T) {
	a, _ := aThemedWindow(t)
	want, err := themeNamed(t, a, "Paper").Palette()
	if err != nil {
		t.Fatalf("its palette: %v", err)
	}

	if err := a.openThemePick(); err != nil {
		t.Fatalf("open the picker: %v", err)
	}
	c := awaitModal(t, a, "the theme picker", byTitle[*ui.Chooser](themeTitle))
	takeChoice(t, c, "Paper")

	if a.colours != want {
		t.Error("the window is not drawn in the theme that was picked")
	}
}

// A theme that could not be written down is still drawn: the window is
// in it, and the reason is what the user needs.
func TestAThemeThatCannotBeRememberedIsStillDrawn(t *testing.T) {
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
		t.Error("the window is not drawn in the theme that was taken")
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

// Clearing the screen after a change paints the new theme's ground,
// because what a cleared cell is made of comes from the theme too.
func TestClearingAfterAThemeChangePaintsTheNewGround(t *testing.T) {
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
// theme's ground, rather than the one it was opened in.
func TestRowsAddedAfterAThemeChangeAreInIt(t *testing.T) {
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

// Every theme reads. The window is written in colours derived from the
// theme's own two ends, and a theme with a light ground broke widgets
// that took a numbered colour for "just off the background".
func TestEveryThemeReads(t *testing.T) {
	for _, theme := range themes.Built() {
		pal, err := theme.Palette()
		if err != nil {
			t.Errorf("%s: %v", theme.Name, err)
			continue
		}
		look, err := theme.Look()
		if err != nil {
			t.Errorf("%s: %v", theme.Name, err)
			continue
		}
		a := newTestApp(t, 80, 24)
		withDialogs(t, a)
		withPanel(t, a)
		withMenubar(t, a)
		a.colours, a.look = pal, look
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
		// A dialog with a ground of its own has to be readable on it. One
		// with no ground of its own takes the frosted panel behind it,
		// which is the window's own and checked above.
		if bg := a.formStyle().BG; bg.A == 0xff {
			for what, fg := range map[string]color.RGBA{
				"a dialog":               a.formStyle().FG,
				"a dialog label":         a.formStyle().LabelFG,
				"a menu":                 a.menuStyle().FG,
				"a dialog's rule":        a.formStyle().BorderFG,
				"why a dialog failed":    a.formStyle().ErrorFG,
				"why a notice failed":    a.noticeStyle().FailureFG,
				"the letters found":      a.paletteStyle().MatchFG,
				"a key beside an item":   a.menuStyle().ChordFG,
				"a machine's name":       a.headingFG(),
				"what a connection does": a.frameDimFG(),
			} {
				if got := grid.Contrast(fg, bg); got < least {
					t.Errorf("%s: %s is %v on the dialog's %v, %.2f:1, want at least %.1f",
						theme.Name, what, fg, bg, got, least)
				}
			}
			// A button is marked by its ground alone, so that ground has
			// to be told from the dialog it sits on.
			for what, on := range map[string]color.RGBA{
				"a button":                 a.buttonBG(),
				"the button Enter presses": a.activeBG(),
			} {
				if got := grid.Contrast(on, bg); got < 1.5 {
					t.Errorf("%s: %s sits on %v and the dialog on %v, %.2f:1, want at least 1.5",
						theme.Name, what, on, bg, got)
				}
			}
		}
		// The sidebar and the menu bar are shaded towards colour 4, so a
		// theme whose ground is already that colour has no frame at all.
		if got := grid.Contrast(a.sidebarFoot(), pal.BG); got < 1.1 {
			t.Errorf("%s: the window's frame is %v on a ground of %v, %.2f:1, and it has to read as a frame",
				theme.Name, a.sidebarFoot(), pal.BG, got)
		}
		// A chip on the menu bar picks its own ground, so what it says
		// has to read on that rather than on the bar.
		for what, fg := range map[string]color.RGBA{
			"a window only listening": idle,
			"a window being driven":   taken,
			"an agent in this window": statusAgentFG(pal),
		} {
			if got := grid.Contrast(fg, a.chipBG()); got < 4.5 {
				t.Errorf("%s: %s is %v on the chip's %v, %.2f:1, want at least 4.5",
					theme.Name, what, fg, a.chipBG(), got)
			}
		}
	}
}

// The chips on the menu bar take the theme. They are built only when
// what they say changes, and a theme is not part of that, so a window
// left them in the colours of the theme before.
func TestTheMenuBarChipsTakeTheTheme(t *testing.T) {
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
	if got, want := a.bar.Chips[0].BG, a.chipBG(); got != want {
		t.Errorf("the chip sits on %v, want %v", got, want)
	}
}

// The pinned row above the sidebar and every divider already drawn take
// the theme, so a window does not end up with two kinds of divider in
// it.
func TestThePinnedRowAndTheDividersTakeTheTheme(t *testing.T) {
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

// Reloading reads the file again, so a theme can be written with the
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
		t.Errorf("it offers %v, want the theme written while the window was open",
			themes.Names(a.theme.all()))
	}
}

// And a window drawn in a theme that has just been edited is redrawn in
// it, rather than keeping the colours it read the first time.
func TestReloadingRedrawsTheWindowInTheThemeAsItIsNow(t *testing.T) {
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

// A window drawn in a theme the file no longer holds falls back to the
// one it would open on, rather than keeping colours nothing describes.
func TestReloadingAfterAThemeIsDeletedFallsBack(t *testing.T) {
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

// A start file holds the theme the window is in, and the window says
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

// writeThemes puts a themes file holding one theme in a directory.
func writeThemes(t *testing.T, dir, name, fg string) {
	t.Helper()
	body := `{"version":1,"themes":[{"name":"` + name + `","fg":"` + fg + `","bg":"#000",` +
		`"ansi":["#000","#100","#200","#300","#400","#500","#600","#700",` +
		`"#800","#900","#a00","#b00","#c00","#d00","#e00","#f00"]}]}`
	if err := os.WriteFile(themes.Path(dir), []byte(body), 0o600); err != nil {
		t.Fatalf("write the themes: %v", err)
	}
}

// A theme that says nothing about its frame gets the window's own
// derived furniture, which is what every theme had before a theme could
// write one down.
func TestAThemeWithNoFrameBlockLeavesTheFurnitureDerived(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.useTheme(themeNamed(t, a, "Dark")); err != nil {
		t.Fatalf("take it: %v", err)
	}

	if a.look.Set {
		t.Fatalf("Dark says %+v about its frame, and it names none", a.look)
	}
	// No ground of its own, so the frosted glass behind it shows through.
	if got := a.formStyle().BG; got.A != 0 {
		t.Errorf("a dialog paints itself on %v, want nothing so the glass shows", got)
	}
	if got := a.formStyle().Rule; got != ui.BorderSingle {
		t.Errorf("a dialog's rule is %v, want the single-line one", got)
	}
	if a.frost() == nil {
		t.Error("there is no glass behind a dialog")
	}
	if got, want := a.formStyle().BorderFG, a.colours.ANSI[8]; got != want {
		t.Errorf("the rule is %v, want the dim colour %v it always was", got, want)
	}
}

// A theme that wrote its frame down gets that frame: its own colours on
// the furniture, a double rule, and an opaque box with no glass behind
// it.
func TestAThemeWithAFrameBlockGetsTheFrameItAskedFor(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)

	if err := a.useTheme(themeNamed(t, a, "Turbo")); err != nil {
		t.Fatalf("take it: %v", err)
	}

	if !a.look.Set {
		t.Fatal("Turbo names a frame and the window read none")
	}
	// An opaque box, so there is nothing for glass to show through.
	if got := a.formStyle().BG; got != a.look.BG {
		t.Errorf("a dialog paints itself on %v, want the frame's own %v", got, a.look.BG)
	}
	if a.frost() != nil {
		t.Error("there is glass behind a flat dialog, and nothing could see through it")
	}
	if got := a.formStyle().Rule; got != ui.BorderDouble {
		t.Errorf("a dialog's rule is %v, want the double-line one", got)
	}
	// The furniture, not the window's ground: a dialog, a menu, the bar
	// and the sidebar are one frame and take one set of colours.
	for what, got := range map[string]color.RGBA{
		"a dialog":     a.formStyle().FG,
		"a notice":     a.noticeStyle().FG,
		"a menu":       a.menuStyle().FG,
		"the menu bar": a.menubarStyle().FG,
		"the sidebar":  a.panelStyle().FG,
	} {
		if got != a.look.FG {
			t.Errorf("%s is written in %v, want the frame's own %v", what, got, a.look.FG)
		}
	}
	if got := a.menubarStyle().BG; got != a.look.BG {
		t.Errorf("the menu bar sits on %v, want the frame's own %v", got, a.look.BG)
	}
	// And a solid shadow, because a box with no glass in it has no light
	// to let through either.
	if got := a.formStyle().ShadowBG; got.A != 0xff {
		t.Errorf("a flat dialog casts %v, want a solid shadow", got)
	}
}

// Going back to a theme with no frame block takes the frame away again,
// so a window is not left with the last theme's furniture on it.
func TestLeavingAFrameThemeGivesTheDerivedFurnitureBack(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.useTheme(themeNamed(t, a, "Turbo")); err != nil {
		t.Fatalf("take Turbo: %v", err)
	}
	if err := a.useTheme(themeNamed(t, a, "Dark")); err != nil {
		t.Fatalf("take Dark: %v", err)
	}

	if a.look.Set {
		t.Errorf("the window kept %+v from the theme before", a.look)
	}
	if got := a.formStyle().Rule; got != ui.BorderSingle {
		t.Errorf("a dialog's rule is %v, want the single-line one back", got)
	}
	if a.frost() == nil {
		t.Error("the glass did not come back behind a dialog")
	}
}

// A colour lifted onto the frame keeps its hue. It is moved towards the
// frame's own text only as far as it has to go to be read, so a cyan
// heading still reads as cyan rather than as one more line of text.
func TestAColourLiftedOntoTheFrameIsStillToldFromTheText(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withPanel(t, a)
	if err := a.useTheme(themeNamed(t, a, "Turbo")); err != nil {
		t.Fatalf("take it: %v", err)
	}

	for what, raw := range map[string]color.RGBA{
		"a machine's name":     a.colours.ANSI[6],
		"why something failed": a.colours.ANSI[1],
		"the letters found":    a.colours.ANSI[3],
		"a remote window":      a.colours.ANSI[5],
	} {
		got := a.onFrame(raw)
		if on := grid.Contrast(got, a.look.BG); on < 3.0 {
			t.Errorf("%s is %v on the frame's %v, %.2f:1, want at least 3.0",
				what, got, a.look.BG, on)
		}
		// Lifted all the way would land on the frame's own text, and the
		// colour would stop saying anything.
		if apart := grid.Contrast(got, a.look.FG); apart < 1.5 {
			t.Errorf("%s came out %v, %.2f:1 from the frame's own %v, so it says nothing",
				what, got, apart, a.look.FG)
		}
	}
}

// A theme that wrote its frame down casts a boxy shadow under each
// button, the way a DOS program drew one. Every other theme casts none.
func TestOnlyAFrameThemeCastsButtonShadows(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)

	if err := a.useTheme(themeNamed(t, a, "Turbo")); err != nil {
		t.Fatalf("take Turbo: %v", err)
	}
	if got := a.formStyle().ButtonShadowBG; got.A != 0xff {
		t.Errorf("a button on a frame theme casts %v, and it has to cast a solid one", got)
	}
	if got := a.noticeStyle().ButtonShadowBG; got.A != 0xff {
		t.Errorf("a notice's button on a frame theme casts %v", got)
	}

	if err := a.useTheme(themeNamed(t, a, "Dark")); err != nil {
		t.Fatalf("take Dark: %v", err)
	}
	if got := a.formStyle().ButtonShadowBG; got.A != 0 {
		t.Errorf("a button on a theme with no frame casts %v, and it casts none", got)
	}
}

// A theme that names a typeface takes it, and one that names none leaves
// the window's own alone.
func TestAThemeTakesTheTypefaceItNames(t *testing.T) {
	a := newTestApp(t, 80, 24)

	if err := a.useTheme(themeNamed(t, a, "Turbo")); err != nil {
		t.Fatalf("take Turbo: %v", err)
	}
	if got := a.fontFamily; got != dosFamily {
		t.Errorf("the window is drawn in %q, want the %q Turbo names", got, dosFamily)
	}

	// Dark names none, so the window keeps what it has rather than being
	// dragged back to the face it opened on.
	if err := a.useTheme(themeNamed(t, a, "Dark")); err != nil {
		t.Fatalf("take Dark: %v", err)
	}
	if got := a.fontFamily; got != dosFamily {
		t.Errorf("a theme naming no typeface changed it to %q", got)
	}
}

// A theme naming a typeface this machine has no font for keeps the one
// the window is already drawn in, rather than refusing the theme.
func TestAThemeNamingAMissingTypefaceKeepsTheOne(t *testing.T) {
	a := newTestApp(t, 80, 24)
	if err := a.setFontFamily(dosFamily); err != nil {
		t.Fatalf("start on the DOS face: %v", err)
	}

	theme := themeNamed(t, a, "Dark")
	theme.Font = "No Such Family At All"
	if err := a.useTheme(theme); err != nil {
		t.Fatalf("a theme naming a typeface that is not here was refused: %v", err)
	}

	if got := a.fontFamily; got != dosFamily {
		t.Errorf("the window is drawn in %q, want the %q it had", got, dosFamily)
	}
	if got := a.colours.BG; got != paletteOf(t, theme).BG {
		t.Error("the theme's colours were not taken")
	}
}

// A typeface a theme names that has to be found on disk is taken when
// the scan lands, because a window opens on its theme before the scan
// has finished.
func TestATypefaceFoundLaterIsStillTaken(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withMenubar(t, a)

	theme := themeNamed(t, a, "Dark")
	theme.Font = "Consolas"
	if err := a.useTheme(theme); err != nil {
		t.Fatalf("take it: %v", err)
	}
	if a.fontFamily == "Consolas" {
		t.Fatal("the family was found before the scan ran, so this proves nothing")
	}

	deliverFonts(a, realFamilies(t, "Consolas", "Courier New"))

	if got := a.fontFamily; got != "Consolas" {
		t.Errorf("the window is drawn in %q, want the %q the theme named", got, "Consolas")
	}
}

// paletteOf reads a theme's palette or fails the test.
func paletteOf(t *testing.T, theme themes.Theme) vt.Palette {
	t.Helper()
	p, err := theme.Palette()
	if err != nil {
		t.Fatalf("%s: %v", theme.Name, err)
	}
	return p
}

// A typeface named on the command line beats the one a theme asks for.
// The flag is an instruction and the theme's name is a wish.
func TestACommandLineTypefaceBeatsTheThemes(t *testing.T) {
	a := newTestApp(t, 80, 24)
	a.fontFixed = true

	if err := a.useTheme(themeNamed(t, a, "Turbo")); err != nil {
		t.Fatalf("take Turbo: %v", err)
	}

	if got := a.fontFamily; got == dosFamily {
		t.Errorf("the theme changed the typeface to %q over the one the flag named", got)
	}
	if got, want := a.colours.BG, paletteOf(t, themeNamed(t, a, "Turbo")).BG; got != want {
		t.Errorf("the window's ground is %v, want the theme's %v: only the typeface is pinned", got, want)
	}
}

// A theme may name the face compiled in by the name the Font menu shows,
// which is the one somebody writing a theme would copy.
func TestAThemeCanNameTheBundledFaceTheWayTheMenuDoes(t *testing.T) {
	a := newTestApp(t, 80, 24)
	if err := a.setFontFamily(dosFamily); err != nil {
		t.Fatalf("start on the DOS face: %v", err)
	}

	theme := themeNamed(t, a, "Dark")
	theme.Font = bundledFamily
	if err := a.useTheme(theme); err != nil {
		t.Fatalf("take it: %v", err)
	}

	if got := a.fontFamily; got != "" {
		t.Errorf("the window is drawn in %q, want the face compiled in", got)
	}
}

// A typeface the user picked for themselves survives the theme being
// taken again, which is what reloading the themes file does.
func TestReloadingTheThemesKeepsTheTypefaceThatWasPicked(t *testing.T) {
	a := newTestApp(t, 80, 24)

	if err := a.useTheme(themeNamed(t, a, "Turbo")); err != nil {
		t.Fatalf("take Turbo: %v", err)
	}
	if err := a.pickFontFamily(""); err != nil {
		t.Fatalf("pick the bundled face: %v", err)
	}

	// The same theme again, the way a reload takes it.
	if err := a.useTheme(themeNamed(t, a, "Turbo")); err != nil {
		t.Fatalf("take Turbo again: %v", err)
	}

	if got := a.fontFamily; got != "" {
		t.Errorf("the window went back to %q, and the typeface was picked by hand", got)
	}
}

// A theme naming a typeface does not leave its colours behind, whatever
// the typeface does.
func TestATypefaceThatWillNotReadStillLeavesTheColours(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withMenubar(t, a)
	// A family whose file is not there, so loading it fails.
	deliverFonts(a, fakeFamilies("Ghost"))

	theme := themeNamed(t, a, "Paper")
	theme.Font = "Ghost"
	if err := a.useTheme(theme); err != nil {
		t.Fatalf("a theme whose typeface will not read was refused: %v", err)
	}

	if got, want := a.colours.BG, paletteOf(t, theme).BG; got != want {
		t.Errorf("the window's ground is %v, want the theme's %v", got, want)
	}
	if a.g != nil && a.g.DefaultBG != paletteOf(t, theme).BG {
		t.Error("the grid kept the colours of the theme before, so restyle was skipped")
	}
}

// A theme naming a typeface does not shrink the window it is taken in.
//
// A window opens on its theme before it has been laid out, and the
// typeface swap used to resize the grid to the pixels it had, which were
// none. The grid settled at one cell and the first shell was started a
// column wide, so its banner and its prompt were laid out for a window
// that never existed.
func TestTakingATypefaceBeforeTheWindowHasASizeKeepsTheGrid(t *testing.T) {
	a := newTestApp(t, 80, 24)
	a.lastPixels = [2]int{}
	a.lastSize = [2]int{80, 24}

	if err := a.setFontFamily(dosFamily); err != nil {
		t.Fatalf("take the DOS face: %v", err)
	}

	if got := a.lastSize; got != [2]int{80, 24} {
		t.Errorf("the window is %v cells, want the %v it had", got, [2]int{80, 24})
	}
	if got := a.fontFamily; got != dosFamily {
		t.Errorf("the typeface is %q, want it taken all the same", got)
	}
}

// Taking a different theme overrules a typeface picked by hand, because
// the new theme is a fresh choice about how the window should look.
func TestADifferentThemeOverrulesAPickedTypeface(t *testing.T) {
	a := newTestApp(t, 80, 24)

	if err := a.useTheme(themeNamed(t, a, "Dark")); err != nil {
		t.Fatalf("take Dark: %v", err)
	}
	if err := a.pickFontFamily(""); err != nil {
		t.Fatalf("pick the bundled face: %v", err)
	}

	if err := a.useTheme(themeNamed(t, a, "Turbo")); err != nil {
		t.Fatalf("take Turbo: %v", err)
	}

	if got := a.fontFamily; got != dosFamily {
		t.Errorf("the window is drawn in %q, want the %q Turbo names", got, dosFamily)
	}
}

// A theme can name the sidebar apart from the rest of its frame, and
// then the menu bar keeps the frame's own colour.
//
// Marcus wanted the list of what is open to read as a panel beside the
// window rather than as more of the bar above it.
func TestAThemeCanNameTheSidebarApartFromTheBar(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	withMenubar(t, a)
	if err := a.useTheme(themeNamed(t, a, "Turbo")); err != nil {
		t.Fatalf("take Turbo: %v", err)
	}

	bar, side := a.menubarStyle().BG, a.panelStyle().BG
	if bar != a.look.BG {
		t.Errorf("the menu bar sits on %v, want the frame's own %v", bar, a.look.BG)
	}
	if side != a.look.SidebarBG {
		t.Errorf("the sidebar sits on %v, want the sidebar's own %v", side, a.look.SidebarBG)
	}
	if got := grid.Contrast(bar, side); got < 1.3 {
		t.Errorf("the bar sits on %v and the sidebar on %v, %.2f:1, and they have to be told apart",
			bar, side, got)
	}
	// Both ends of the sidebar, because it is one flat colour under a
	// theme that wrote its frame down.
	if got := a.panelStyle().BGEnd; got != a.look.SidebarBG {
		t.Errorf("the foot of the sidebar is %v, want the sidebar's own %v", got, a.look.SidebarBG)
	}
}

// A theme that names no sidebar colour draws one frame in one colour all
// the way round the window, which is what every theme did before there
// was a sidebar colour to name.
func TestAFrameWithNoSidebarColourIsOneColourAllRound(t *testing.T) {
	for _, raw := range []themes.Theme{
		{Name: "Flat", FG: "#ffffff", BG: "#101010",
			Frame: &themes.Frame{FG: "#000000", BG: "#c0c0c0"}},
	} {
		look, err := raw.Look()
		if err != nil {
			t.Fatalf("read the frame: %v", err)
		}
		if look.SidebarBG != look.BG || look.SidebarFG != look.FG {
			t.Errorf("the sidebar came out %v on %v, want the frame's own %v on %v",
				look.SidebarFG, look.SidebarBG, look.FG, look.BG)
		}
	}
}

// What the sidebar says reads on the sidebar's own ground, which is not
// the ground the rest of the frame is drawn on.
func TestWhatTheSidebarSaysReadsOnTheSidebar(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withDialogs(t, a)
	withPanel(t, a)
	if err := a.useTheme(themeNamed(t, a, "Turbo")); err != nil {
		t.Fatalf("take Turbo: %v", err)
	}
	on := a.sidebarBG()

	// A row's own text, which is the sidebar's own and has to be read.
	if got := grid.Contrast(a.panelStyle().FG, on); got < 4.5 {
		t.Errorf("a row is %v on the sidebar's %v, %.2f:1, want at least 4.5",
			a.panelStyle().FG, on, got)
	}
	// And what is lifted out of the palette onto it, which is held to
	// the three the lift itself aims for.
	for what, fg := range map[string]color.RGBA{
		"a machine's name":     a.headingFG(),
		"a note beside a row":  a.frameDimFG(),
		"a busy connection":    a.stateFG(meter.Active, panelNow),
		"a settled connection": a.stateFG(meter.Settled, panelNow),
		"a window over there":  a.onSidebar(a.colours.ANSI[5]),
	} {
		if got := grid.Contrast(fg, on); got < 3.0 {
			t.Errorf("%s is %v on the sidebar's %v, %.2f:1, want at least 3.0", what, fg, on, got)
		}
		// Lifted all the way would land on the sidebar's own text.
		if got := grid.Contrast(fg, a.sidebarFG()); got < 1.4 {
			t.Errorf("%s came out %v, %.2f:1 from the sidebar's own %v, so it says nothing",
				what, fg, got, a.sidebarFG())
		}
	}
	// The row in front is marked by its ground alone, so that ground has
	// to be told from the sidebar.
	if got := grid.Contrast(a.currentBG(), on); got < 1.2 {
		t.Errorf("the row in front sits on %v and the sidebar on %v, %.2f:1",
			a.currentBG(), on, got)
	}
}
