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

// What the shell printed before keeps the colours it was printed in. A
// cell holds what it is drawn in, not which entry of the scheme it came
// from, so there is nothing to look the new one up with.
func TestWhatWasPrintedBeforeKeepsItsColours(t *testing.T) {
	a, _ := aThemedWindow(t)
	pane := onlyPaneWidget(t, a).(*term.Terminal)
	was := a.colours
	a.shells[0].out <- []byte("printed before the scheme changed")
	waitFor(t, a, "the shell to print", func() bool {
		return strings.Contains(paneText(pane), "printed before")
	})

	if err := a.takeTheme(themeNamed(t, a, "Paper")); err != nil {
		t.Fatalf("take it: %v", err)
	}

	if got := gridOfPane(t, pane).At(0, 0).FG; got != was.FG {
		t.Errorf("what was printed before is now %v, want the %v it was printed in", got, was.FG)
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
