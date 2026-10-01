package view

import (
	"slices"
	"strings"
	"time"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"
)

// Use Secret: a secret picked from a list, and typed into the terminal
// used last or put on the clipboard. The secrets for the machine of the
// pane in front come first; a field finds among them all. Locked, the
// secrets are asked to open first, and the list comes once they are.

// useWait is how long after asking for the secrets to open the list
// still comes once they are open.
const useWait = 2 * time.Minute

// useSecret opens Use Secret, or asks for the secrets to open first.
func (w *Window) useSecret(u *gunim.UI) {
	switch {
	case !w.vault.Exists:
		w.toasts.Show(widget.Toast{Title: "No secrets yet", Body: "Add one with Add Secret, in Manage Secrets."}, u)
	case !w.vault.Open:
		w.useOnOpen = time.Now()
		u.Send(w, app.UnlockSecrets{})
	default:
		w.useSecretDialog(u)
	}
}

// useWhenOpen opens Use Secret once the secrets it asked to open are.
func (w *Window) useWhenOpen(u *gunim.UI) {
	if w.useOnOpen.IsZero() || !w.vault.Open {
		return
	}
	asked := w.useOnOpen
	w.useOnOpen = time.Time{}
	if time.Since(asked) < useWait && (w.dialog == nil || u.Presence(w.dialog) == gunim.Exiting) {
		w.useSecretDialog(u)
	}
}

// relatedSecret reports whether it belongs to the machine of the pane in
// front: it signs in there, or is named after it or its address.
func (w *Window) relatedSecret(it app.SecretItem) bool {
	m := w.machineOf(w.lastWorked)
	if m == machines.Local {
		return false
	}
	var words []string
	if name := strings.ToLower(w.nameOf(m)); name != "" {
		words = append(words, name)
	}
	for _, h := range w.saved {
		if machines.ID(h.ID) == m {
			words = append(words, strings.ToLower(h.Address))
		}
	}
	for _, info := range w.machineList {
		if info.ID == m && info.Target != "" {
			words = append(words, strings.ToLower(info.Target))
		}
	}
	for _, l := range it.Logins {
		_, host, _ := strings.Cut(strings.ToLower(l), "@")
		host, _, _ = strings.Cut(host, ":")
		if slices.Contains(words, host) {
			return true
		}
	}
	name := strings.ToLower(it.Name + " " + it.User)
	return slices.ContainsFunc(words, func(word string) bool { return word != "" && strings.Contains(name, word) })
}

// useSecretDialog lists the secrets to type or copy one.
func (w *Window) useSecretDialog(u *gunim.UI) {
	items := slices.Clone(w.vault.Items)
	related := map[string]bool{}
	for _, it := range items {
		related[it.ID] = w.relatedSecret(it)
	}
	slices.SortStableFunc(items, func(a, b app.SecretItem) int {
		switch {
		case related[a.ID] && !related[b.ID]:
			return -1
		case related[b.ID] && !related[a.ID]:
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	byID := map[widget.Key]app.SecretItem{}
	for _, it := range items {
		byID[widget.Key(it.ID)] = it
	}
	table := widget.NewTable(widget.TableColumn{Title: "Name"}, widget.TableColumn{Title: "For", Width: 180})
	table.Row = func(k widget.Key) widget.TableRow {
		it := byID[k]
		return widget.TableRow{Cells: []string{it.Name, secretFor(it)}, Strong: related[it.ID]}
	}
	find := widget.NewTextField()
	find.Placeholder, find.Icon = "Find a secret", icon.Search
	var keys []widget.Key
	fill := func(u *gunim.UI) {
		q := strings.ToLower(strings.TrimSpace(find.Text()))
		keys = keys[:0]
		for _, it := range items {
			if q == "" || strings.Contains(strings.ToLower(it.Name+" "+secretFor(it)), q) {
				keys = append(keys, widget.Key(it.ID))
			}
		}
		table.SetKeys(keys, u)
		if len(keys) > 0 {
			table.SetCursor(keys[0], u)
		}
	}
	find.OnEdit = func(_ string, u *gunim.UI) { fill(u) }
	picked := func(as func(id string) gunim.Intent) func() gunim.Intent {
		return func() gunim.Intent {
			if k, ok := table.Cursor(); ok {
				return as(string(k))
			}
			return nil
		}
	}
	typeIt := picked(func(id string) gunim.Intent { return app.TypeSecret{ID: id} })
	d := widget.NewDialog("Use a secret")
	d.Icon = icon.KeyRound
	d.Body = &pickerBody{find: find, table: table, keys: &keys, list: widget.NewSized(table, 460, 280)}
	d.SetButtons("Type", "Cancel")
	d.AddButton("Copy", picked(func(id string) gunim.Intent { return app.CopySecret{ID: id} }))
	d.Check = func() string {
		if _, ok := table.Cursor(); !ok {
			return "No secret is picked."
		}
		return ""
	}
	d.OnAccept = typeIt
	table.OnActivate = func(k widget.Key, u *gunim.UI) {
		d.Close(u)
		u.Send(w, app.TypeSecret{ID: string(k)})
	}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
	fill(u)
	u.Focus(find)
}

// pickerBody is Use Secret's field over its list. Up and Down move along
// the list while the field has the keyboard, so typing and picking go
// on together.
type pickerBody struct {
	find  *widget.TextField
	table *widget.Table
	keys  *[]widget.Key
	list  *widget.Sized
}

// Children implements [gunim.Composite].
func (b *pickerBody) Children() []gunim.Node { return []gunim.Node{b.find, b.list} }

// Focusables are what Tab goes through in the dialog.
func (b *pickerBody) Focusables() []gunim.Node { return []gunim.Node{b.find, b.table} }

// Layout implements [gunim.Node]: the field, the list under it.
func (b *pickerBody) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	fs := kids.At(0).Layout(gunim.Constraints{Min: geom.Sz(460, 0), Max: geom.Sz(460, 60)})
	kids.At(0).Place(geom.Pt(0, 0))
	ls := kids.At(1).Layout(gunim.Constraints{Max: geom.Sz(460, 280)})
	kids.At(1).Place(geom.Pt(0, fs.H+8))
	return c.Constrain(geom.Sz(460, fs.H+8+ls.H))
}

// Paint implements [gunim.Node].
func (b *pickerBody) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	kids.At(0).Paint(p)
	kids.At(1).Paint(p)
}

// Handle implements [gunim.Handler]: Up and Down from the field move
// along the list.
func (b *pickerBody) Handle(e input.Event, u *gunim.UI) bool {
	k, ok := e.(input.KeyPress)
	if !ok || k.Mods != 0 || u.Focused() != gunim.Node(b.find) || (k.Key != input.KeyUp && k.Key != input.KeyDown) {
		return false
	}
	keys := *b.keys
	if len(keys) == 0 {
		return true
	}
	at := 0
	if cur, ok := b.table.Cursor(); ok {
		at = max(slices.Index(keys, cur), 0)
	}
	if k.Key == input.KeyDown {
		at = min(at+1, len(keys)-1)
	} else {
		at = max(at-1, 0)
	}
	b.table.SetCursor(keys[at], u)
	return true
}
