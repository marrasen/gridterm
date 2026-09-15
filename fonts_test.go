package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/ui"
)

// fakeFamilies returns families the scan might have found, without
// reading anything off disk.
func fakeFamilies(names ...string) []glyph.Family {
	out := make([]glyph.Family, 0, len(names))
	for _, name := range names {
		f := glyph.Family{Name: name}
		f.Src[glyph.Regular] = glyph.Source{Path: name + ".ttf"}
		out = append(out, f)
	}
	return out
}

// realFamilies returns families whose files exist and parse, so a test
// can drive the whole font switch rather than stopping at the read.
func realFamilies(t *testing.T, names ...string) []glyph.Family {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gomono.ttf")
	if err := os.WriteFile(path, gomono.TTF, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	out := make([]glyph.Family, 0, len(names))
	for _, name := range names {
		f := glyph.Family{Name: name}
		f.Src[glyph.Regular] = glyph.Source{Path: path}
		out = append(out, f)
	}
	return out
}

// TestSetFontFamilyUsesTheFamilysOwnSpelling checks the name the window
// remembers. Keeping what was typed instead would make choosing the same
// family from the menu rebuild the atlas for no change.
func TestSetFontFamilyUsesTheFamilysOwnSpelling(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	deliverFonts(a, realFamilies(t, "Courier New"))

	if err := a.setFontFamily("courier new"); err != nil {
		t.Fatalf("switch font: %v", err)
	}

	if a.fontFamily != "Courier New" {
		t.Errorf("the window calls the font %q, want the family's own spelling", a.fontFamily)
	}
	// And asking again, by any spelling, is now no change at all.
	gen := a.atlas.Generation()
	if err := a.setFontFamily("COURIER NEW"); err != nil {
		t.Fatalf("switch again: %v", err)
	}
	if a.atlas.Generation() != gen {
		t.Error("asking for the font already in use rebuilt the atlas")
	}
}

// TestSetFontFamilyRebuildsTheAtlas checks the switch end to end: the
// glyphs are re-rasterised and the window re-measures itself, because a
// new typeface is a new cell box.
func TestSetFontFamilyRebuildsTheAtlas(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	deliverFonts(a, realFamilies(t, "Elsewhere"))
	gen := a.atlas.Generation()

	if err := a.setFontFamily("Elsewhere"); err != nil {
		t.Fatalf("switch font: %v", err)
	}

	if a.atlas.Generation() == gen {
		t.Error("the atlas was not rebuilt, so the old glyphs are still cached")
	}
	if err := a.setFontFamily(""); err != nil {
		t.Fatalf("back to the bundled font: %v", err)
	}
	if a.fontFamily != "" {
		t.Errorf("the window calls the font %q, want the bundled one", a.fontFamily)
	}
}

// TestChooseFontsKeepsTheBundledFacesReachable checks the flags. The
// typeface a flag names is what the window starts with, and nothing
// more: the bundled faces are read separately, so the Font menu can
// always get back to them.
func TestChooseFontsKeepsTheBundledFacesReachable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mono.ttf")
	if err := os.WriteFile(path, gomonobold.TTF, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}

	fonts, family, err := chooseFonts(path, "")
	if err != nil {
		t.Fatalf("-font %s: %v", path, err)
	}

	if family != "" {
		t.Errorf("family = %q, want none: a file names no family", family)
	}
	if len(fonts.Regular) != len(gomonobold.TTF) {
		t.Error("the file given was not what the window starts with")
	}
	// The bundled faces are untouched by any of that.
	if len(bundledFonts().Regular) != len(gomono.TTF) {
		t.Error("the bundled faces are no longer the bundled faces")
	}
}

func TestChooseFontsRefusesBothFlags(t *testing.T) {
	if _, _, err := chooseFonts("a.ttf", "Consolas"); err == nil {
		t.Error("naming a typeface twice was accepted")
	}
}

// deliverFonts hands the app a scan result and lets the draw loop pick
// it up, which is what the goroutine reading the font directories does.
func deliverFonts(a *testApp, families []glyph.Family) {
	deliverScan(a, scanned{families: families})
}

// deliverScan is deliverFonts with whatever the scan reported alongside.
func deliverScan(a *testApp, got scanned) {
	a.families = make(chan scanned, 1)
	a.families <- got
	a.reapFontScan()
}

func TestFontScanRegistersACommandPerFamily(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	before := a.root.Commands.Len()

	deliverFonts(a, fakeFamilies("Consolas", "Courier New"))

	// One per family, plus the bundled face.
	if got := a.root.Commands.Len() - before; got != 3 {
		t.Errorf("%d commands registered, want 3", got)
	}
	for _, name := range []string{"Consolas", "Courier New"} {
		id := fontCommandID(name)
		cmd, ok := a.root.Commands.Lookup(id)
		if !ok {
			t.Errorf("no command %q for family %q", id, name)
			continue
		}
		if !strings.Contains(cmd.Title, name) {
			t.Errorf("command %q is called %q, want the family in the title", id, cmd.Title)
		}
	}
	if _, ok := a.root.Commands.Lookup(fontCommandPrefix + "bundled"); !ok {
		t.Error("no command for the bundled font")
	}
}

func TestFontCommandID(t *testing.T) {
	for name, want := range map[string]string{
		"Consolas":               fontCommandPrefix + "consolas",
		"Courier New":            fontCommandPrefix + "courier-new",
		"Lucida Sans Typewriter": fontCommandPrefix + "lucida-sans-typewriter",
	} {
		if got := fontCommandID(name); got != want {
			t.Errorf("fontCommandID(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestFontScanAddsAMenu checks the families reach the menu bar, and that
// every line of it names a command that is registered.
func TestFontScanAddsAMenu(t *testing.T) {
	a := newTestApp(t, 40, 20)
	bar := withMenubar(t, a)
	before := len(bar.Menus)

	deliverFonts(a, fakeFamilies("Consolas", "Courier New"))

	if len(bar.Menus) != before+1 {
		t.Fatalf("%d menus, want one more than %d", len(bar.Menus), before)
	}
	menu := bar.Menus[len(bar.Menus)-1]
	if menu.Title != "Font" {
		t.Errorf("the new menu is called %q, want %q", menu.Title, "Font")
	}
	named := 0
	for _, item := range menu.Items {
		if item.Command == "" {
			continue
		}
		if _, ok := a.root.Commands.Lookup(item.Command); !ok {
			t.Errorf("the font menu names %q, which is not registered", item.Command)
		}
		named++
	}
	if named != 3 {
		t.Errorf("%d lines on the font menu, want 3", named)
	}
}

// TestFontScanClosesAnOpenMenuFirst checks the rule the menu bar
// documents: an open menu holds the index of the title it hangs under,
// and the list of titles is about to grow.
func TestFontScanClosesAnOpenMenuFirst(t *testing.T) {
	a := newTestApp(t, 40, 20)
	bar := withMenubar(t, a)
	if err := a.openMenu(); err != nil {
		t.Fatalf("open menu: %v", err)
	}

	deliverFonts(a, fakeFamilies("Consolas"))

	if bar.OpenIndex() != -1 {
		t.Errorf("open = %d, want the menu closed before the titles changed", bar.OpenIndex())
	}
	if len(a.modals) != 0 {
		t.Errorf("%d dialogs left open", len(a.modals))
	}
}

// TestFontScanIsPickedUpOnce checks the draw loop polling an empty
// channel, which is what it does on all but one frame.
func TestFontScanIsPickedUpOnce(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	deliverFonts(a, fakeFamilies("Consolas"))
	after := a.root.Commands.Len()

	// Every frame from now on finds nothing waiting.
	for i := 0; i < 3; i++ {
		a.reapFontScan()
	}

	if got := a.root.Commands.Len(); got != after {
		t.Errorf("%d commands, want the %d registered once", got, after)
	}
}

// TestFontScanSurvivesADuplicateName checks two families whose names
// differ only in case. One of them cannot be registered, and losing the
// rest of the list over it would be worse.
func TestFontScanSurvivesADuplicateName(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)

	deliverFonts(a, fakeFamilies("Consolas", "CONSOLAS", "Courier New"))

	if _, ok := a.root.Commands.Lookup(fontCommandID("Courier New")); !ok {
		t.Error("the family after the duplicate was lost")
	}
	if len(a.installed) != 3 {
		t.Errorf("%d families remembered, want all 3", len(a.installed))
	}
}

// TestSetFontFamilyRejectsAnUnknownName checks that a stale binding or a
// mistyped name is reported rather than quietly leaving the font alone.
func TestSetFontFamilyRejectsAnUnknownName(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	deliverFonts(a, fakeFamilies("Consolas"))

	err := a.setFontFamily("Nonesuch")

	if err == nil {
		t.Fatal("an unknown family was accepted")
	}
	if !strings.Contains(err.Error(), "Nonesuch") {
		t.Errorf("error = %v, want it to name the family", err)
	}
	if a.fontFamily != "" {
		t.Errorf("the font changed to %q anyway", a.fontFamily)
	}
}

// TestSetFontFamilyReportsAMissingFile checks the rule this codebase
// holds about disk errors. A family found at startup whose file has
// since gone must report that, not fall back to something else.
func TestSetFontFamilyReportsAMissingFile(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	deliverFonts(a, fakeFamilies("Ghost"))

	err := a.setFontFamily("Ghost")

	if err == nil {
		t.Fatal("a family whose file is gone was accepted")
	}
	if a.fontFamily != "" {
		t.Errorf("the font changed to %q despite the failure", a.fontFamily)
	}
}

// TestSetFontFamilyIgnoresTheOneInUse checks the early return. Rebuilding
// the atlas for the font already showing would throw away every glyph
// for nothing.
func TestSetFontFamilyIgnoresTheOneInUse(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	deliverFonts(a, fakeFamilies("Ghost"))
	// A family whose file does not exist, so loading it would fail. The
	// early return is what keeps this from being an error.
	a.fontFamily = "Ghost"

	if err := a.setFontFamily("Ghost"); err != nil {
		t.Errorf("switching to the family already in use: %v", err)
	}
	// And by any spelling of its name.
	if err := a.setFontFamily("ghost"); err != nil {
		t.Errorf("switching to the family already in use, in lowercase: %v", err)
	}
	if a.fontFamily != "Ghost" {
		t.Errorf("the font is now %q", a.fontFamily)
	}
}

// TestStartFontScanDeliversAResult covers the goroutine itself, which
// every other test here steps around by putting a result on the channel
// by hand.
func TestStartFontScanDeliversAResult(t *testing.T) {
	if testing.Short() {
		t.Skip("reads every font file on the system")
	}
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)

	a.startFontScan()

	// The channel is buffered, so the goroutine finishes whether or not
	// anyone is waiting. Blocking here is the test for that.
	got := <-a.families
	if got.err != nil {
		t.Fatalf("scanning the system fonts: %v", got.err)
	}
	// Put it back and let the draw loop pick it up the way it does.
	a.families <- got
	a.reapFontScan()

	// And no dialog: the font files a real machine has that cannot be
	// parsed are skipped in silence, so a window on a stock install
	// opens with nothing in front of it.
	if a.root.Modal() != nil {
		t.Fatalf("a real scan put %T in front of the user", a.root.Modal())
	}

	if len(a.installed) != len(got.families) {
		t.Errorf("%d families remembered, want the %d that were found",
			len(a.installed), len(got.families))
	}
	if _, ok := a.root.Commands.Lookup(fontCommandPrefix + "bundled"); !ok {
		t.Error("the bundled font has no command after a real scan")
	}
}

// TestFontScanReportsAFailureAndKeepsWhatItFound checks the disk-error
// rule at the one place it is deliberately not fatal: a font directory
// that will not open costs the fonts in it, not the window.
func TestFontScanReportsAFailureAndKeepsWhatItFound(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	var logged []error
	a.onError = func(err error) { logged = append(logged, err) }

	deliverScan(a, scanned{
		families: fakeFamilies("Consolas"),
		err:      errors.New("open C:/Windows/Fonts: permission denied"),
	})

	if len(logged) == 0 {
		t.Error("the failure was swallowed")
	}
	if _, ok := a.root.Commands.Lookup(fontCommandID("Consolas")); !ok {
		t.Error("the families that were found were thrown away with the failure")
	}
}

// TestFontFamilyNamesAreMatchedIgnoringCase checks a name from a
// settings file or typed into the palette.
func TestFontFamilyNamesAreMatchedIgnoringCase(t *testing.T) {
	a := newTestApp(t, 40, 20)
	withMenubar(t, a)
	deliverFonts(a, fakeFamilies("Courier New"))

	if _, ok := a.familyNamed("courier new"); !ok {
		t.Error("a family did not match its own name in lowercase")
	}
	if _, ok := a.familyNamed("Courier"); ok {
		t.Error("a partial name matched")
	}
}

// TestFontMenuLinesAreNamedByTheirCommands checks that the font menu
// draws family names rather than command ids, which is what the menu
// does for a line it cannot name.
func TestFontMenuLinesAreNamedByTheirCommands(t *testing.T) {
	a := newTestApp(t, 40, 20)
	bar := withMenubar(t, a)
	deliverFonts(a, fakeFamilies("Consolas"))

	if !bar.Open(len(bar.Menus) - 1) {
		t.Fatal("the font menu would not open")
	}
	menu, ok := a.root.Modal().(*ui.Menu)
	if !ok {
		t.Fatalf("top modal = %T, want a menu", a.root.Modal())
	}

	// Every line survived the drop that removes what cannot be named.
	if got := len(menu.Items()); got != 3 {
		t.Errorf("%d lines showing, want 3: a line was dropped as unnameable", got)
	}
}

// refusedPath returns a path the operating system will refuse to read,
// and will not merely call missing. A missing directory proves nothing
// here: the font scan is right to pass one of those over.
//
// What counts as a refusal differs. Windows rejects a name with a bar in
// it. Anywhere else a path through a regular file is not a directory, so
// the file is made and its own path returned: it, and anything under it,
// fails to be read.
//
// In a directory of its own, so that a caller listing a directory of its
// own does not find this file in it.
func refusedPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "no|such")
	}
	path := filepath.Join(dir, "notadirectory")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// unreadableFontDir points the font scan at directories the operating
// system will refuse, and returns the part of the path the reason has to
// name.
func unreadableFontDir(t *testing.T) string {
	t.Helper()
	bad := refusedPath(t)
	if runtime.GOOS == "windows" {
		t.Setenv("windir", bad)
		t.Setenv("localappdata", filepath.Join(t.TempDir(), "also|bad"))
		return bad
	}
	// The scan reads the font directories under the user's home, so a
	// home that cannot be read is two directories that cannot be read.
	t.Setenv("HOME", bad)
	return bad
}

// TestFontScanFailureReachesTheUser drives the scan the window runs and
// checks the whole reason lands in one dialog.
//
// Two directories are made unreadable, because the user gets one dialog
// however many of them failed.
func TestFontScanFailureReachesTheUser(t *testing.T) {
	a := newTestApp(t, 80, 24)
	withMenubar(t, a)
	bad := unreadableFontDir(t)

	a.startFontScan()
	// The channel is buffered, so the goroutine finishes whether or not
	// anyone is waiting. Then the draw loop picks it up the way it does.
	got := <-a.families
	if got.err == nil {
		t.Fatalf("the scan of %s reported nothing wrong", bad)
	}
	a.families <- got
	a.reapFontScan()

	n, ok := a.root.Modal().(*ui.Notice)
	if !ok {
		t.Fatalf("top dialog = %T, want a notice about the fonts", a.root.Modal())
	}
	if len(a.modals) != 1 {
		t.Errorf("%d dialogs opened, want one however many directories failed", len(a.modals))
	}
	if !n.Failure {
		t.Error("the notice is not marked as a failure, so its title is not red")
	}
	if !strings.Contains(n.Message(), bad) {
		t.Errorf("the notice does not name %s:\n%s", bad, n.Message())
	}
	if !strings.Contains(n.Message(), got.err.Error()) {
		t.Errorf("the notice does not hold the whole reason:\n%s\nwant it to contain\n%s",
			n.Message(), got.err.Error())
	}
}

// The dialog says what the list is -- the font files and directories
// that could not be read -- and points at the Font menu only when there
// is something on it.
func TestTheFontDialogSaysWhatTheListIsAndWhatIsLeft(t *testing.T) {
	for _, tc := range []struct {
		name     string
		families []glyph.Family
		menu     bool
	}{
		{name: "some families were found",
			families: fakeFamilies("Some Font"), menu: true},
		{name: "none were", menu: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(t, 80, 24)
			withMenubar(t, a)
			reason := "/usr/share/fonts: permission denied"

			deliverScan(a, scanned{families: tc.families, err: errors.New(reason)})

			n, ok := a.root.Modal().(*ui.Notice)
			if !ok {
				t.Fatalf("top dialog = %T, want a notice about the fonts", a.root.Modal())
			}
			if n.Title != "Some fonts could not be read" {
				t.Errorf("the notice is titled %q", n.Title)
			}
			if !strings.Contains(n.Message(), "These font files and directories could not be read:") {
				t.Errorf("the notice does not say what the list is:\n%s", n.Message())
			}
			if !strings.Contains(n.Message(), reason) {
				t.Errorf("the notice does not hold the reason:\n%s", n.Message())
			}
			said := "The fonts that were found are on the Font menu."
			if got := strings.Contains(n.Message(), said); got != tc.menu {
				t.Errorf("the notice points at the Font menu: %v, want %v\n%s",
					got, tc.menu, n.Message())
			}
		})
	}
}
