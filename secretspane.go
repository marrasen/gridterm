package main

import (
	"image/color"
	"strconv"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/input"
	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/ui"
)

// secretsPane is what is in the vault, on screen: every secret by name,
// and under them the keys that open it.
//
// A pane rather than a dialog, for the reason the connection log and
// the file work are panes. Show Secrets is a list that goes away on the
// first pick, so changing three things means asking three times and
// never seeing the whole. The rule this follows is that a dialog asks
// and a pane shows.
//
// It is not a terminal, and that is load-bearing rather than incidental.
// handPane and windows.watching both take a *term.Terminal, so a pane
// of this type cannot be handed to an agent or to a window that has
// taken this one over. Anything here is on screen for whoever is
// sitting at this machine and for nobody else.
type secretsPane struct {
	app  *app
	size ui.Size

	// at is the row the bar is on, counted over the secrets and the
	// keys together: the list is one thing to walk.
	at int

	// items and keys are the vault as it was last read, and read says
	// whether they have been. Held rather than asked for on every
	// frame, because Items goes to the disk to pick up what another
	// window has written, and a pane draws sixty times a second.
	items []secrets.Item
	keys  []secrets.KeySlot
	read  bool

	// trouble is what the last read failed with, and nil when it did
	// not. Drawn where the list would be: a pane that quietly showed
	// nothing would read as an empty vault.
	trouble error
}

// How the pane is laid out: the blank column each side, and the rows
// above the list that are always there.
const (
	secretsPaneMargin = 2

	// secretsPaneTop is the heading and the blank row under it.
	secretsPaneTop = 2
)

// newSecretsPane opens a pane on the vault.
func newSecretsPane(a *app) *secretsPane { return &secretsPane{app: a} }

// Layout takes the room it is given.
func (p *secretsPane) Layout(size ui.Size) { p.size = size }

// Draw paints the pane.
//
// The vault's locked state is read every frame, which costs nothing,
// and what is in it only when it has to be. A vault that has been
// locked since the last frame drops what was read from it here, names
// included: locking the keys is the user saying they want none of it on
// screen.
func (p *secretsPane) Draw(v grid.View) {
	vault := p.app.secrets
	if vault == nil || vault.Locked() {
		p.forget()
		p.drawLocked(v)
		return
	}
	if !p.read {
		p.reload(vault)
	}
	p.drawList(v)
}

// forget drops everything read out of the vault.
func (p *secretsPane) forget() {
	p.items, p.keys, p.read, p.trouble = nil, nil, false, nil
}

// reload reads the vault again. Called when the pane has nothing and
// after anything that changes what is in it.
func (p *secretsPane) reload(v *secrets.Vault) {
	items, err := v.Items()
	p.items, p.keys, p.read, p.trouble = items, v.Keys(), true, err
	if err != nil {
		p.items, p.keys = nil, nil
	}
	p.at = min(max(p.at, 0), max(p.rows()-1, 0))
}

// rows is how many lines the bar can be on: every secret and every key.
func (p *secretsPane) rows() int { return len(p.items) + len(p.keys) }

// drawLocked says the vault is shut and how to open it.
func (p *secretsPane) drawLocked(v grid.View) {
	cols, rows := v.Size()
	v.Fill(grid.Cell{Rune: ' ', FG: p.app.colours.FG, BG: p.app.colours.BG, Width: 1})
	if rows < 1 {
		return
	}
	room := max(cols-secretsPaneMargin*2, 0)
	p.say(v, secretsPaneMargin, 0, room, manageSecretsTitle, p.app.colours.FG, grid.AttrBold)
	if rows > secretsPaneTop {
		p.say(v, secretsPaneMargin, secretsPaneTop, room, secretsLocked,
			p.app.panelDimFG(), 0)
	}
}

// secretsLocked is what the pane says while the vault is shut.
//
// It names the one thing that opens them again, which is any of the
// commands: they unlock the vault on the way to what they do.
const secretsLocked = `Locked. Choose "` + showSecretsTitle + `" to open them.`

// drawList paints the heading, the secrets, and the keys under them.
func (p *secretsPane) drawList(v grid.View) {
	cols, rows := v.Size()
	v.Fill(grid.Cell{Rune: ' ', FG: p.app.colours.FG, BG: p.app.colours.BG, Width: 1})
	if rows < 1 {
		return
	}
	room := max(cols-secretsPaneMargin*2, 0)
	p.say(v, secretsPaneMargin, 0, room, p.heading(), p.app.colours.FG, grid.AttrBold)

	if p.trouble != nil {
		if rows > secretsPaneTop {
			p.say(v, secretsPaneMargin, secretsPaneTop, room, p.trouble.Error(),
				p.app.onFrame(p.app.colours.ANSI[1]), 0)
		}
		return
	}
	if p.rows() == 0 {
		if rows > secretsPaneTop {
			p.say(v, secretsPaneMargin, secretsPaneTop, room,
				`Nothing here yet. Choose "`+addSecretTitle+`" to add one.`,
				p.app.panelDimFG(), 0)
		}
		return
	}

	y := secretsPaneTop
	for i, it := range p.items {
		if y >= rows {
			return
		}
		p.row(v, y, room, i, it.Name, secretNote(it))
		y++
	}
	if len(p.keys) == 0 {
		return
	}
	// A blank row and a heading, so the keys read as what opens the
	// list above rather than as more of it.
	y++
	if y < rows {
		p.say(v, secretsPaneMargin, y, room, secretsPaneKeys, p.app.panelDimFG(), 0)
		y++
	}
	for i, s := range p.keys {
		if y >= rows {
			return
		}
		p.row(v, y, room, len(p.items)+i, keyRowName(s), keyPaneNote(s))
		y++
	}
}

// secretsPaneKeys heads the keys under the secrets.
const secretsPaneKeys = "Keys that open them"

// keyPaneNote is the right-hand side of a key's row here: whether the
// key is on this machine, and nothing else.
//
// Not the fingerprint, which the chooser's row carries. A key file's
// path is long enough that the fingerprint after it is the first thing
// off the end of the row, and a fingerprint is the one thing on screen
// that has to be read whole or not at all. Where a key has to be told
// apart by its fingerprint, keyRowName already uses it as the name.
func keyPaneNote(s secrets.KeySlot) string {
	if onThisMachine(s) {
		return "on this machine"
	}
	return ""
}

// heading says what the pane is and how much is in it.
func (p *secretsPane) heading() string {
	switch n := len(p.items); n {
	case 0:
		return manageSecretsTitle
	case 1:
		return manageSecretsTitle + " — 1 secret"
	default:
		return manageSecretsTitle + " — " + strconv.Itoa(n) + " secrets"
	}
}

// row draws one line: its name, and its note along the right.
func (p *secretsPane) row(v grid.View, y, room, i int, name, note string) {
	fg, bg, attr := p.app.colours.FG, p.app.colours.BG, grid.Attr(0)
	if i == p.at {
		fg, bg = p.app.activeFG(), p.app.activeBG()
		// The whole width, so the bar reads as a line rather than as a
		// word picked out.
		v.Sub(0, y, p.widthOf(v), 1).Fill(grid.Cell{Rune: ' ', FG: fg, BG: bg, Width: 1})
	}
	p.sayOn(v, secretsPaneMargin, y, room, name, fg, bg, attr)
	if note == "" {
		return
	}
	// Right along the row, and only what is left after the name.
	at := secretsPaneMargin + grid.StringWidth(name) + 2
	width := room + secretsPaneMargin - at
	if width <= 0 {
		return
	}
	if grid.StringWidth(note) > width {
		// Dropped rather than trimmed. A note here is a path or a
		// fingerprint, and half of either is worse than none: a
		// fingerprint cut in the middle is one somebody can compare
		// against another and believe matched.
		return
	}
	noteFG := p.app.panelDimFG()
	if i == p.at {
		noteFG = fg
	}
	p.sayOn(v, secretsPaneMargin+room-grid.StringWidth(note), y, width, note, noteFG, bg, 0)
}

// widthOf is how wide the view is, for a bar drawn the whole way.
func (p *secretsPane) widthOf(v grid.View) int {
	cols, _ := v.Size()
	return cols
}

// say writes one line on the pane's own background.
func (p *secretsPane) say(v grid.View, x, y, room int, text string,
	fg color.RGBA, attr grid.Attr) {

	p.sayOn(v, x, y, room, text, fg, p.app.colours.BG, attr)
}

// sayOn writes one line on a background of its own, for a row the bar
// is on.
func (p *secretsPane) sayOn(v grid.View, x, y, room int, text string,
	fg, bg color.RGBA, attr grid.Attr) {

	if room <= 0 {
		return
	}
	v.SetString(x, y, grid.Trim(text, room), fg, bg, attr)
}

// HandleKey moves the bar. Everything else travels on: a pane shares
// the screen, so it swallows only what it uses.
func (p *secretsPane) HandleKey(ev input.Event) (bool, error) {
	if ev.Kind != input.KeyPress && ev.Kind != input.KeyRepeat {
		return false, nil
	}
	if ev.Mods != 0 {
		return false, nil
	}
	switch ev.Key {
	case input.KeyUp:
		p.move(-1)
		return true, nil
	case input.KeyDown:
		p.move(1)
		return true, nil
	case input.KeyHome:
		p.at = 0
		p.app.markDirty()
		return true, nil
	case input.KeyEnd:
		p.at = max(p.rows()-1, 0)
		p.app.markDirty()
		return true, nil
	}
	return false, nil
}

// move walks the bar, stopping at each end rather than wrapping: the
// secrets and the keys are one list with two parts, and coming off the
// bottom onto the top would hide that.
func (p *secretsPane) move(by int) {
	if p.rows() == 0 {
		return
	}
	p.at = min(max(p.at+by, 0), p.rows()-1)
	p.app.markDirty()
}

// showSecretsPane opens the pane on the vault, or goes to the one
// already open.
func (a *app) showSecretsPane() error {
	return a.withOpenSecrets(couldNotOpenTheSecrets, func(*secrets.Vault) error {
		if a.secretsPane != nil {
			a.focus(a.secretsPane)
			a.secretsPane.forget()
			return nil
		}
		pane := newSecretsPane(a)
		if err := a.placePane(pane); err != nil {
			return err
		}
		a.secretsPane = pane
		a.secretsRow = &conns.Entry{
			Host:   conns.Local,
			Kind:   conns.Secrets,
			Label:  secretsTitle,
			Reveal: func() { a.focus(pane) },
			Close:  func() error { return a.closePane(pane) },
		}
		a.registry.Add(a.secretsRow)
		return nil
	})
}

// couldNotOpenTheSecrets heads whatever went wrong on the way to the
// pane. The same words the list uses, because it is the same failure.
const couldNotOpenTheSecrets = "Could not open the secrets"

// forgetSecretsPane takes a closed pane off the window's record of it
// and off the sidebar.
func (a *app) forgetSecretsPane(w ui.Widget) {
	pane, is := w.(*secretsPane)
	if !is || pane != a.secretsPane {
		return
	}
	a.registry.Drop(a.secretsRow)
	a.secretsPane, a.secretsRow = nil, nil
}
