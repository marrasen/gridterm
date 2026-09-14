package main

import (
	"strings"
	"testing"

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

// deliverFonts hands the app a scan result and lets the draw loop pick
// it up, which is what the goroutine reading the font directories does.
func deliverFonts(a *testApp, families []glyph.Family) {
	a.families = make(chan []glyph.Family, 1)
	a.families <- families
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

	// The atlas is nil in a test app, so reaching it would panic. That
	// the call returns at all is the assertion.
	if err := a.setFontFamily(""); err != nil {
		t.Errorf("switching to the font already in use: %v", err)
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
