package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/ui"
)

// fontCommandPrefix is what a font family's command id starts with, so
// the commands registered for one scan can be taken away again.
const fontCommandPrefix = "font.use."

// bundledFamily is what the font menu calls the faces compiled into the
// binary, which are always available and need no file.
const bundledFamily = "Go Mono (bundled)"

// dosFamily is the other face compiled in: the IBM VGA character set, so
// a theme can ask for the look of a DOS program on a machine that has no
// such font installed.
const dosFamily = "PxPlus IBM VGA8"

// compiledIn returns the faces for a family that needs no file, the name
// to remember it by, and whether the name is one of them.
//
// Go Mono answers to the empty name and to what the Font menu calls it,
// because the menu's own wording is what somebody writing a theme will
// copy. It is remembered as the empty name either way.
func compiledIn(name string) (glyph.Fonts, string, bool) {
	switch {
	case name == "", strings.EqualFold(name, bundledFamily):
		return bundledFonts(), "", true
	case strings.EqualFold(name, dosFamily):
		return dosFonts(), dosFamily, true
	}
	return glyph.Fonts{}, "", false
}

// startFontScan reads the system's fonts on a goroutine of its own.
//
// It takes a second or two, which is far too long on the goroutine that
// draws. The result comes back through a channel the draw loop polls, so
// the widget tree and the command registry are only ever touched from
// the one goroutine allowed to touch them.
func (a *app) startFontScan() {
	// The goroutine holds the channel rather than reading the field, so
	// that starting a second scan cannot race with the first one
	// finishing.
	found := make(chan scanned, 1)
	a.families = found
	go func() {
		families, err := glyph.Monospaced()
		found <- scanned{families: families, err: err}
	}()
}

// scanned is what the goroutine reading the font directories sends back.
type scanned struct {
	families []glyph.Family
	err      error
}

// reapFontScan registers a command per installed family, once the scan
// has finished. It is called from the draw loop.
func (a *app) reapFontScan() {
	var got scanned
	select {
	case got = <-a.families:
	default:
		return
	}
	a.installed = got.families

	cmds := make([]ui.Command, 0, len(got.families)+1)
	cmds = append(cmds, ui.Command{
		ID:    fontCommandPrefix + "bundled",
		Title: "Font: " + bundledFamily,
		Run:   func() error { return a.pickFontFamily("") },
	})
	cmds = append(cmds, ui.Command{
		ID:    fontCommandID(dosFamily),
		Title: "Font: " + dosFamily,
		Run:   func() error { return a.pickFontFamily(dosFamily) },
	})
	for _, family := range got.families {
		name := family.Name
		if strings.EqualFold(name, dosFamily) {
			// The same face is compiled in and has its line already.
			// Registering a second command under one id would only log a
			// clash on every start.
			continue
		}
		cmds = append(cmds, ui.Command{
			ID:    fontCommandID(name),
			Title: "Font: " + name,
			Run:   func() error { return a.pickFontFamily(name) },
		})
	}
	// Only the ones that registered go on the menu. A line naming a
	// command that is not there is dropped by the menu anyway, so putting
	// it on would leave a gap nobody can explain.
	registered := cmds[:0:0]
	for _, cmd := range cmds {
		if err := a.root.Commands.Register(cmd); err != nil {
			// Two families whose names reduce to the same id, or a name
			// that collides with a command already registered. Neither is
			// worth losing the rest of the list over.
			a.logError(err)
			continue
		}
		registered = append(registered, cmd)
	}
	a.refreshFontMenu(registered)
	// The theme was taken before the scan finished, so a face it named
	// that had to be found on disk can only be taken now.
	a.useWantedFont()
	a.reportFontScan(got.err)
}

// reportFontScan tells the user what the font scan could not read and
// why, once the scan is over.
//
// Carrying on with the fonts that were found, rather than failing, is
// the fallback Marcus approved by asking for this dialog. It is the one
// decision everywhere fonts are read, and it is written down here and
// nowhere else. It reaches the user by two routes: the scan's own
// failures through this, and the ones met while looking for a fallback
// face through reapFontTrouble.
func (a *app) reportFontScan(err error) {
	if err == nil {
		return
	}
	a.logError(fmt.Errorf("reading the system fonts: %w", err))
	// The atlas walks the same directories when it looks for a fallback
	// face, so it is told what has been shown and does not show it
	// again.
	if a.atlas != nil {
		a.atlas.Told(sayings(err)...)
	}
	said := "These font files and directories could not be read:\n\n" + err.Error()
	if len(a.installed) > 0 {
		said += "\n\nThe fonts that were found are on the Font menu."
	}
	a.showNotice("Some fonts could not be read", said, true)
}

// reapFontTrouble shows what the search for a fallback font could not
// read. It is called from the draw loop.
//
// The search happens part way through a frame, on the first character
// the chosen font cannot draw, so there is nothing to report to at the
// time and the atlas keeps it until somebody asks.
func (a *app) reapFontTrouble() {
	if a.atlas == nil {
		return
	}
	failed := a.atlas.Trouble()
	if len(failed) == 0 {
		return
	}
	err := errors.Join(failed...)
	a.logError(fmt.Errorf("looking for a font to fall back on: %w", err))
	a.showNotice("Some fonts could not be read while looking for a character",
		"A character was not in the font in use, and these could not be read "+
			"while looking for one that has it:\n\n"+err.Error(),
		true)
}

// sayings breaks a joined error into what each of its parts says.
func sayings(err error) []string {
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return []string{err.Error()}
	}
	parts := joined.Unwrap()
	out := make([]string, len(parts))
	for i, one := range parts {
		out[i] = one.Error()
	}
	return out
}

// fontCommandID names the command that switches to a family. Family
// names hold spaces and capitals; a command id is lowercase and has
// none, so that a settings file naming one is readable.
func fontCommandID(family string) string {
	return fontCommandPrefix + strings.ToLower(strings.ReplaceAll(family, " ", "-"))
}

// refreshFontMenu puts the families on the menu bar, under their own
// title.
//
// The menu is added once the scan is done rather than left empty and
// filled in, because a title that is there but drops down nothing reads
// as a fault.
func (a *app) refreshFontMenu(cmds []ui.Command) {
	if a.bar == nil || len(cmds) == 0 {
		return
	}
	items := make([]ui.MenuItem, 0, len(cmds)+1)
	items = append(items, ui.MenuItem{Command: cmds[0].ID}, ui.MenuSeparator())
	for _, cmd := range cmds[1:] {
		items = append(items, ui.MenuItem{Command: cmd.ID})
	}
	a.addMenu(ui.MenuDef{Title: "Font", Items: items})
}

// setFontFamily swaps the typeface, by family name. An empty name goes
// back to the faces compiled into the binary.
func (a *app) setFontFamily(name string) error {
	// A face that needs no file has its name settled first, so every
	// spelling of it is recognised as the face already in use.
	fonts, settled, compiled := compiledIn(name)
	if compiled {
		name = settled
	}
	if strings.EqualFold(name, a.fontFamily) {
		return nil
	}
	if !compiled {
		family, ok := a.familyNamed(name)
		if !ok {
			return fmt.Errorf("no font family %q", name)
		}
		loaded, err := family.Load()
		if err != nil {
			return err
		}
		fonts = loaded
		// The family's own spelling, so asking for it again is
		// recognised as no change.
		name = family.Name
	}
	if err := a.atlas.SetFonts(fonts); err != nil {
		return fmt.Errorf("font %q: %w", name, err)
	}
	a.fontFamily = name
	if a.lastPixels == [2]int{} {
		// The window has not been laid out yet, which is where the theme
		// it opens on asks for a typeface. Resizing to no pixels at all
		// would settle the grid at one cell and start the first shell a
		// column wide.
		return nil
	}
	// A new typeface is a new cell box, so the window holds a different
	// number of cells and every glyph quad has to be measured again.
	a.lastSize = [2]int{0, 0}
	a.resizeTo(a.lastPixels[0], a.lastPixels[1])
	a.markDirty()
	return nil
}

// haveFontFamily reports whether a name picks out a face this window can
// draw with: one compiled in, or one the scan found installed.
func (a *app) haveFontFamily(name string) bool {
	if _, _, ok := compiledIn(name); ok {
		return true
	}
	_, ok := a.familyNamed(name)
	return ok
}

// familyNamed finds an installed family, ignoring case so a name typed
// into the palette or written in a settings file still matches.
func (a *app) familyNamed(name string) (glyph.Family, bool) {
	for _, family := range a.installed {
		if strings.EqualFold(family.Name, name) {
			return family, true
		}
	}
	return glyph.Family{}, false
}

// useWantedFont takes the typeface the theme asked for.
//
// A theme that named none leaves the window's own alone, and so does one
// that named a face this machine has no font for: the name is a wish,
// not a requirement, and a window drawn in the wrong typeface is better
// than one that refuses the theme. A face that is there and will not
// read is a different thing, and that error reaches the user.
//
// A typeface named on the command line is an instruction rather than a
// wish, and a theme does not overrule it.
//
// It is called again when the scan of the system's fonts lands, because
// a window opens on its theme before the scan has finished.
func (a *app) useWantedFont() {
	if a.fontFixed || a.fontPicked || a.wantFont == "" || !a.haveFontFamily(a.wantFont) {
		return
	}
	if err := a.setFontFamily(a.wantFont); err != nil {
		a.logError(err)
		a.reportError("The theme's typeface could not be read", err)
	}
}

// pickFontFamily is the Font menu's route into setFontFamily. It writes
// down that the user chose for themselves, so taking the same theme
// again -- which is what reloading the themes file does -- does not drag
// them back off the typeface they just picked.
func (a *app) pickFontFamily(name string) error {
	a.fontPicked = true
	return a.setFontFamily(name)
}
