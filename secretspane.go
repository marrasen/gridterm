package main

import (
	"image/color"
	"slices"
	"strconv"
	"time"

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

	// button is the one along the bottom that Enter would press, and
	// buttonCols where each was last drawn, for a click. What is
	// offered changes with the row the bar is on, so a click measured
	// against a row drawn for a different one would press the wrong
	// thing: drawnChoices is what was actually on screen.
	button       int
	buttonRow    int
	buttonCols   []int
	drawnChoices []string

	// picked are the secrets ticked for removing several at once, by
	// id. Removing is the one thing that is tedious a row at a time.
	picked map[string]bool

	// top is the first line drawn, and shown how many were, so a click
	// lands on what the user was looking at. Without them the list
	// drew from the first line always and the bar walked off the
	// bottom of what was on screen: Copy and Show then answered for a
	// secret nobody could see.
	top   int
	shown int

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

	// readAt is when the vault was last read, so the pane picks up what
	// another window has written without asking the disk every frame.
	readAt time.Time
}

// How the pane is laid out: the blank column each side, and the rows
// above the list that are always there.
const (
	secretsPaneMargin = 2

	// secretsPaneTop is the heading and the blank row under it.
	secretsPaneTop = 2

	// secretsPaneButtons is the row of buttons with a blank above it
	// and another below, so they sit off the bottom edge the way a
	// dialog's do.
	secretsPaneButtons = 3
)

// secretsPaneSettles is how often the pane reads the vault again while
// it is open.
//
// One way of picking up a change rather than two. Everything that
// changes the vault does it behind a dialog that closes when it likes,
// and half of it happens in another window or another command
// altogether, so a pane that was told about its own changes would still
// need this for the rest. Often enough that the list is not stale to
// look at, rarely enough that a pane drawing sixty times a second is
// not sixty reads.
const secretsPaneSettles = time.Second

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
	now := p.app.clock()
	if !p.read || now.Sub(p.readAt) >= secretsPaneSettles {
		p.reload(vault)
	}
	p.drawList(v)
	p.drawButtons(v)
}

// forget drops everything read out of the vault.
func (p *secretsPane) forget() {
	p.items, p.keys, p.read, p.trouble = nil, nil, false, nil
	p.picked = nil
}

// reload reads the vault again. Called when the pane has nothing and
// after anything that changes what is in it.
func (p *secretsPane) reload(v *secrets.Vault) {
	items, err := v.Items()
	p.items, p.keys, p.read, p.trouble = items, v.Keys(), true, err
	p.readAt = p.app.clock()
	if err != nil {
		p.items, p.keys = nil, nil
	}
	p.at = min(max(p.at, 0), max(p.rows()-1, 0))
	// A tick on a secret that has gone is a tick on nothing. Dropped
	// here rather than checked at the moment of removing, so the row
	// count the buttons speak of is the one on screen.
	for id := range p.picked {
		if !slices.ContainsFunc(p.items, func(it secrets.Item) bool { return it.ID == id }) {
			delete(p.picked, id)
		}
	}
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

	// The buttons have the bottom rows, so the list stops above them.
	all := p.lines()
	height := max(rows-secretsPaneButtons-secretsPaneTop, 0)
	p.scrollTo(all, height)
	p.shown = min(height, max(len(all)-p.top, 0))
	for i := range p.shown {
		ln := all[p.top+i]
		y := secretsPaneTop + i
		switch {
		case ln.heading:
			p.say(v, secretsPaneMargin, y, room, ln.text, p.app.panelDimFG(), 0)
		case ln.at < 0:
			// The blank over the heading.
		default:
			p.row(v, y, room, ln.at, ln.text, ln.note)
		}
	}
}

// paneLine is one line of the list as it is drawn.
//
// The list is not the rows: between the secrets and the keys are a
// blank and a heading, which are drawn and never landed on. Building
// the lines and then taking a window of them is what lets the bar and
// the pointer agree about which line is which.
type paneLine struct {
	// at is which row this line is, counted the way the bar counts,
	// and below zero for a line the bar never lands on.
	at         int
	text, note string
	heading    bool
}

// lines is the list as it is drawn, top to bottom.
func (p *secretsPane) lines() []paneLine {
	out := make([]paneLine, 0, p.rows()+2)
	for i, it := range p.items {
		out = append(out, paneLine{at: i, text: it.Name, note: secretNote(it)})
	}
	if len(p.keys) == 0 {
		return out
	}
	// A blank row and a heading, so the keys read as what opens the
	// list above rather than as more of it.
	out = append(out, paneLine{at: -1})
	out = append(out, paneLine{at: -1, text: secretsPaneKeys, heading: true})
	for i, s := range p.keys {
		out = append(out, paneLine{
			at: len(p.items) + i, text: keyRowName(s), note: keyPaneNote(s),
		})
	}
	return out
}

// scrollTo moves the window so the row the bar is on is inside it.
func (p *secretsPane) scrollTo(all []paneLine, height int) {
	if height <= 0 {
		p.top = 0
		return
	}
	on := slices.IndexFunc(all, func(ln paneLine) bool { return ln.at == p.at })
	switch {
	case on < 0:
	case on < p.top:
		p.top = on
	case on >= p.top+height:
		p.top = on - height + 1
	}
	p.top = min(max(p.top, 0), max(len(all)-height, 0))
}

// secretsPanePicked marks a row ticked for removing. The same character
// the panel's clear button uses, which is this window's word for "this
// one goes".
const secretsPanePicked = '\u00d7'

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
	if s.ByPassphrase() {
		return passphraseRowNote
	}
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
	if i < len(p.items) && p.picked[p.items[i].ID] {
		// In the margin that is there anyway, so a row costs no width
		// for a mark it usually does not carry.
		p.sayOn(v, 0, y, secretsPaneMargin, string(secretsPanePicked), fg, bg, attr)
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

// onSecret is the secret the bar is on, and whether it is on one.
func (p *secretsPane) onSecret() (secrets.Item, bool) {
	if p.at < 0 || p.at >= len(p.items) {
		return secrets.Item{}, false
	}
	return p.items[p.at], true
}

// onKey is the key slot the bar is on, and whether it is on one.
func (p *secretsPane) onKey() (secrets.KeySlot, bool) {
	at := p.at - len(p.items)
	if at < 0 || at >= len(p.keys) {
		return secrets.KeySlot{}, false
	}
	return p.keys[at], true
}

// choices are what the pane offers for the row the bar is on, left to
// right.
//
// They change with the row, the way a job's do with its state. A key
// has its own two, which are not written yet: until they are, a key row
// offers what any row does.
func (p *secretsPane) choices() []string {
	if _, on := p.onKey(); on {
		return []string{btnAddKey, btnRemoveKey}
	}
	if _, on := p.onSecret(); !on {
		return []string{btnAddSecret, btnAddNote}
	}
	// What is done to the row first, in the order the chooser offers
	// the same things, so Copy and Show are in the same place in both.
	// Then the two that are about the list rather than the row.
	return []string{btnCopy, btnShow, btnChange, p.removeTitle(), btnAddSecret, btnAddNote}
}

// removeTitle says how many the button would take, when it is more than
// the row the bar is on.
func (p *secretsPane) removeTitle() string {
	if n := len(p.picked); n > 1 {
		return btnRemove + " " + strconv.Itoa(n)
	}
	return btnRemove
}

// drawButtons paints the row along the bottom, right aligned from the
// corner the eye lands on.
//
// Right rather than centred, which is where the job pane puts its own:
// this is a list with things to do to it, and every other list in the
// window -- the chooser, a form, a notice -- puts them there.
func (p *secretsPane) drawButtons(v grid.View) {
	cols, rows := v.Size()
	row := rows - 2
	if row < secretsPaneTop || cols <= secretsPaneMargin*2 {
		p.buttonRow = -1
		return
	}
	p.buttonRow = row
	choices := p.choices()
	if !slices.Equal(choices, p.drawnChoices) {
		// A different row offers different things, and the place the
		// bar was in among the old ones means nothing among the new:
		// Show on a secret is Remove key on the key under it.
		p.button = 0
	}
	p.button = min(max(p.button, 0), len(choices)-1)
	p.buttonCols = ui.ButtonColsInto(p.buttonCols[:0], choices, cols, secretsPaneMargin)
	p.drawnChoices = append(p.drawnChoices[:0], choices...)
	st := p.app.formStyle()
	for i, at := range p.buttonCols {
		if at < 0 {
			continue
		}
		fg, bg := st.ButtonFG, st.ButtonBG
		if i == p.button {
			fg, bg = st.ActiveFG, st.ActiveBG
		}
		ui.DrawButton(v, at, row, choices[i], fg, bg)
	}
}

// press does what the button at i says.
func (p *secretsPane) press(i int) error {
	choices := p.choices()
	if i < 0 || i >= len(choices) {
		return nil
	}
	a := p.app
	v := a.secrets
	if v == nil || v.Locked() {
		return nil
	}
	switch choices[i] {
	case btnCopy:
		return p.onTheRow(v, a.copySecret)
	case btnShow:
		return p.onTheRow(v, a.showSecret)
	case btnAddSecret:
		return p.app.askForSecret(v, secrets.Password)
	case btnAddNote:
		// The same form, with the field unmasked and no Generate on it.
		// A note typed into one line is what this gives; somewhere
		// taller to write one is worth having and is not this step.
		return p.app.askForSecret(v, secrets.Note)
	case btnAddKey:
		return p.app.chooseAKeyToAdd(v)
	case btnRemoveKey:
		return p.removeKey(v)
	case btnChange:
		return p.change(v)
	default:
		return p.remove(v)
	}
}

// removeKey takes away the slot the bar is on, once the user has been
// asked.
//
// Straight to the question rather than through the chooser, because the
// pane already knows which key: the row the bar is on is the one on
// screen. The guard before it is the chooser's own -- the last key
// cannot go, and nothing here could put it back.
func (p *secretsPane) removeKey(v *secrets.Vault) error {
	s, on := p.onKey()
	if !on {
		return nil
	}
	if p.app.onlyOneKeyOpensThem(v) {
		return nil
	}
	p.app.confirmRemoveKey(v, s)
	return nil
}

// onTheRow does something to the secret the bar is on, once it is
// still there to do it to.
//
// Copy and Show are the chooser's own, called here rather than written
// again: the same secret leaves the vault the same way whichever list
// asked for it. Show puts it in a notice, which is read and dismissed
// -- a value revealed on a row would sit on screen for as long as the
// pane did, and the pane outlives everything.
func (p *secretsPane) onTheRow(v *secrets.Vault, do func(*secrets.Vault, secrets.Item) error) error {
	it, on := p.onSecret()
	if !on {
		return nil
	}
	if !p.stillThere(v, it.ID) {
		return nil
	}
	return do(v, it)
}

// change opens the form the chooser opens, on the row the bar is on.
//
// The same form, because it is the same change. What the pane adds is
// that the list is still there afterwards.
func (p *secretsPane) change(v *secrets.Vault) error {
	return p.onTheRow(v, p.app.askToChange)
}

// stillThere reports whether a secret is in the vault now, and says so
// when it is not.
//
// Another window may have taken it away between this pane drawing the
// row and the user pressing the button. Saying that is better than
// handing on the vault's own "there is no such item", which reads as
// something having gone wrong here.
func (p *secretsPane) stillThere(v *secrets.Vault, id string) bool {
	p.reload(v)
	if slices.ContainsFunc(p.items, func(it secrets.Item) bool { return it.ID == id }) {
		return true
	}
	p.app.say(secretWentElsewhere)
	return false
}

// secretWentElsewhere is said when a row is acted on after something
// else has removed it.
const secretWentElsewhere = "That secret has been removed from somewhere else"

// remove takes away what is ticked, or the row the bar is on when
// nothing is.
func (p *secretsPane) remove(v *secrets.Vault) error {
	going := p.going()
	if len(going) == 0 {
		return nil
	}
	p.app.confirmRemoveSecrets(v, going, func() { p.forget() })
	return nil
}

// going are the secrets Remove would take: the ticked ones, or the row
// the bar is on when none are ticked.
func (p *secretsPane) going() []secrets.Item {
	var out []secrets.Item
	for _, it := range p.items {
		if p.picked[it.ID] {
			out = append(out, it)
		}
	}
	if len(out) > 0 {
		return out
	}
	if it, on := p.onSecret(); on {
		return []secrets.Item{it}
	}
	return nil
}

// tick turns the row the bar is on over, for removing several at once.
func (p *secretsPane) tick() {
	it, on := p.onSecret()
	if !on {
		return
	}
	if p.picked == nil {
		p.picked = map[string]bool{}
	}
	if p.picked[it.ID] {
		delete(p.picked, it.ID)
	} else {
		p.picked[it.ID] = true
	}
	p.app.markDirty()
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
	case input.KeyLeft, input.KeyRight:
		// The same keys as every list with things to do to it: up and
		// down pick the row, left and right pick what to do with it.
		n := len(p.choices())
		step := 1
		if ev.Key == input.KeyLeft {
			step = -1
		}
		p.button = ((p.button+step)%n + n) % n
		p.app.markDirty()
		return true, nil
	case input.KeySpace:
		if ev.Kind != input.KeyPress {
			return true, nil
		}
		p.tick()
		return true, nil
	case input.KeyEnter:
		if ev.Kind != input.KeyPress {
			return true, nil
		}
		return true, p.press(p.button)
	}
	return false, nil
}

// HandleMouse presses the button under the pointer, and puts the bar on
// the row that was clicked.
func (p *secretsPane) HandleMouse(ev input.MouseEvent) (bool, error) {
	if ev.Kind != input.MousePress || ev.Button != input.MouseLeft {
		return false, nil
	}
	if ev.Row == p.buttonRow && p.buttonRow >= 0 {
		choices := p.choices()
		if !slices.Equal(choices, p.drawnChoices) {
			// What is offered has changed since the row was drawn, so
			// where the pointer went is not what it went to. The next
			// frame draws the new row and a second click presses what
			// it says.
			p.app.markDirty()
			return true, nil
		}
		for i, at := range p.buttonCols {
			if at < 0 || i >= len(choices) {
				continue
			}
			if ev.Col >= at && ev.Col < at+ui.ButtonWidth(choices[i]) {
				p.button = i
				return true, p.press(i)
			}
		}
		return true, nil
	}
	// Against the lines that were drawn, and only those. Mapping the
	// row straight onto an index into everything put the bar on a
	// secret that was never on screen -- a click on the blank under a
	// short list chose one further down than the pane had ever shown.
	if row := ev.Row - secretsPaneTop; row >= 0 && row < p.shown {
		all := p.lines()
		if at := p.top + row; at < len(all) && all[at].at >= 0 {
			p.at = all[at].at
			p.app.markDirty()
			return true, nil
		}
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
