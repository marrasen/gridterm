package main

import (
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
	a.families = make(chan []glyph.Family, 1)
	go func() { a.families <- glyph.Monospaced() }()
}

// reapFontScan registers a command per installed family, once the scan
// has finished. It is called from the draw loop.
func (a *app) reapFontScan() {
	var found []glyph.Family
	select {
	case found = <-a.families:
	default:
		return
	}
	a.installed = found

	cmds := make([]ui.Command, 0, len(found)+1)
	cmds = append(cmds, ui.Command{
		ID:    fontCommandPrefix + "bundled",
		Title: "Font: " + bundledFamily,
		Run:   func() error { return a.setFontFamily("") },
	})
	for _, family := range found {
		name := family.Name
		cmds = append(cmds, ui.Command{
			ID:    fontCommandID(name),
			Title: "Font: " + name,
			Run:   func() error { return a.setFontFamily(name) },
		})
	}
	for _, cmd := range cmds {
		if err := a.root.Commands.Register(cmd); err != nil {
			// Two families whose names differ only in case, or a name
			// that collides with a command already registered. Neither is
			// worth losing the rest of the list over.
			a.logError(err)
		}
	}
	a.refreshFontMenu(cmds)
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
	if name == a.fontFamily {
		return nil
	}
	fonts := a.bundled
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
