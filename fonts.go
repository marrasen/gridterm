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
		Run:   func() error { return a.setFontFamily("") },
	})
	for _, family := range got.families {
		name := family.Name
		cmds = append(cmds, ui.Command{
			ID:    fontCommandID(name),
			Title: "Font: " + name,
			Run:   func() error { return a.setFontFamily(name) },
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
	// Whatever is open holds the index of the title it hangs under, and
	// the list is about to grow.
	a.bar.Close()

	items := make([]ui.MenuItem, 0, len(cmds)+1)
	items = append(items, ui.MenuItem{Command: cmds[0].ID}, ui.MenuSeparator())
	for _, cmd := range cmds[1:] {
		items = append(items, ui.MenuItem{Command: cmd.ID})
	}
	a.bar.Menus = append(a.bar.Menus, ui.MenuDef{Title: "Font", Items: items})
}

// setFontFamily swaps the typeface, by family name. An empty name goes
// back to the faces compiled into the binary.
func (a *app) setFontFamily(name string) error {
	if strings.EqualFold(name, a.fontFamily) {
		return nil
	}
	fonts := bundledFonts()
	if name != "" {
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
	// A new typeface is a new cell box, so the window holds a different
	// number of cells and every glyph quad has to be measured again.
	a.lastSize = [2]int{0, 0}
	a.resizeTo(a.lastPixels[0], a.lastPixels[1])
	a.markDirty()
	return nil
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
