package main

import (
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/secrets"
)

// secretsPane lists what is in the vault, by name, over a bar of what
// can be done with the one the cursor is on. Enter copies it. Under
// the list are the keys that open the vault.
type secretsPane struct {
	w     *window
	head  *buttonBar
	table *widget.Table
	act   *buttonBar
	keys  *widget.Table
	col   *widget.Flex
	st    Secrets
	byID  map[widget.Key]SecretItem
	// The header's buttons and the bar's.
	add, note, lock, unlock         *widget.Button
	typ, cp, reveal, change, remove *widget.Button
	keyNames                        map[widget.Key]SecretKey
}

func newSecretsPane(w *window) *secretsPane {
	p := &secretsPane{w: w, head: newButtonBar(), act: newButtonBar(), byID: map[widget.Key]SecretItem{}, keyNames: map[widget.Key]SecretKey{}}
	p.head.label.Size, p.head.label.Color = widget.DialogTitleSize, widget.Ink
	p.table = widget.NewTable(
		widget.TableColumn{Title: "Name"},
		widget.TableColumn{Title: "For", Width: 200},
		widget.TableColumn{Title: "Kind", Width: 110},
	)
	p.table.Row = func(k widget.Key) widget.TableRow {
		it := p.byID[k]
		kind := string(it.Kind)
		if it.File != "" {
			kind = "key passphrase"
		}
		return widget.TableRow{Cells: []string{it.Name, it.User, kind}}
	}
	p.table.OnActivate = func(k widget.Key, u *gunim.UI) { u.Send(p.table, CopySecret{ID: string(k)}) }
	p.keys = widget.NewTable(widget.TableColumn{Title: "Opened by"}, widget.TableColumn{Title: "", Width: 340})
	p.keys.Row = func(k widget.Key) widget.TableRow {
		s := p.keyNames[k]
		return widget.TableRow{Cells: []string{s.Name, s.Note}, Faint: s.Passphrase}
	}
	button := func(label string) *widget.Button { return widget.NewButton(label) }
	p.add, p.note, p.lock, p.unlock = button("Add Secret"), button("Add Note"), button("Lock"), button("Unlock")
	p.typ, p.cp, p.reveal, p.change, p.remove = button("Type"), button("Copy"), button("Show"), button("Change"), button("Remove")
	p.lock.On, p.unlock.On = LockSecrets{}, UnlockSecrets{}
	p.add.OnActivate(func(u *gunim.UI) { p.w.secretForm(secrets.Password, nil, u) })
	p.note.OnActivate(func(u *gunim.UI) { p.w.secretForm(secrets.Note, nil, u) })
	onRow := func(b *widget.Button, do func(SecretItem, *gunim.UI)) {
		b.OnActivate(func(u *gunim.UI) {
			if k, ok := p.table.Cursor(); ok {
				do(p.byID[k], u)
			}
		})
	}
	onRow(p.typ, func(it SecretItem, u *gunim.UI) { u.Send(p.table, TypeSecret{ID: it.ID}) })
	onRow(p.cp, func(it SecretItem, u *gunim.UI) { u.Send(p.table, CopySecret{ID: it.ID}) })
	onRow(p.reveal, func(it SecretItem, u *gunim.UI) { u.Send(p.table, RevealSecret{ID: it.ID}) })
	onRow(p.change, func(it SecretItem, u *gunim.UI) { p.w.secretForm(it.Kind, &it, u) })
	onRow(p.remove, func(it SecretItem, u *gunim.UI) { p.w.confirmRemoveSecret(it, u) })
	p.col = widget.Column(p.head, p.table, p.act, p.keys).Grow(p.table, 3).Grow(p.keys, 1)
	p.col.Cross, p.col.Gap = widget.CrossStretch, noGap
	return p
}

// show brings the pane up to date with the vault.
func (p *secretsPane) show(st Secrets, u *gunim.UI) {
	p.st = st
	clear(p.byID)
	keys := make([]widget.Key, 0, len(st.Items))
	for _, it := range st.Items {
		p.byID[widget.Key(it.ID)] = it
		keys = append(keys, widget.Key(it.ID))
	}
	p.table.SetKeys(keys, u)
	clear(p.keyNames)
	var slots []widget.Key
	for _, k := range st.Keys {
		p.keyNames[widget.Key(k.Fingerprint)] = k
		slots = append(slots, widget.Key(k.Fingerprint))
	}
	p.keys.SetKeys(slots, u)
	switch {
	case !st.Open:
		p.head.set("Secrets", u, p.unlock)
		p.act.set("Locked. Unlock to see what is in them.", u)
	case len(st.Items) == 0:
		p.head.set("Secrets", u, p.add, p.note, p.lock)
		p.act.set("No secrets yet. Add one to keep it here, locked by your key.", u)
	default:
		p.head.set("Secrets", u, p.add, p.note, p.lock)
		p.act.set(count(len(st.Items), "secret")+" · Enter copies the one selected", u, p.typ, p.cp, p.reveal, p.change, p.remove)
	}
}

// Children implements [gunim.Composite].
func (p *secretsPane) Children() []gunim.Node { return []gunim.Node{p.col} }

// Layout implements [gunim.Node].
func (p *secretsPane) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	k.Layout(gunim.Tight(c.Max))
	k.Place(geom.Point{})
	return c.Max
}

// Paint implements [gunim.Node].
func (p *secretsPane) Paint(pt *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	pt.RRect(geom.Rect{Max: box.Point()}, 0, paint.Solid(widget.Background.Get(f.Theme)))
	kids.At(0).Paint(pt)
}

// secretForm asks for a new secret of kind, or a change to old. A
// password is typed hidden, with a box to show it and a button that
// makes one up; a note is typed in the open.
func (w *window) secretForm(kind secrets.Kind, old *SecretItem, u *gunim.UI) {
	name, user, value := widget.NewTextField(), widget.NewTextField(), widget.NewTextField()
	user.Placeholder = "optional: who or what it is for"
	value.Secret = kind != secrets.Note
	label, title := "Password", "Add Secret"
	if kind == secrets.Note {
		label, title = "Note", "Add Note"
	}
	id := ""
	if old != nil {
		id, title = old.ID, "Change "+old.Name
		name.SetText(old.Name)
		user.SetText(old.User)
		value.Placeholder = "empty keeps the one saved"
	} else if m := w.machineOf(w.lastTerm); m != "" {
		// The server in front of the user, which a password typed now
		// is nearly always for.
		user.SetText(m)
	}
	form := widget.NewForm().Add("", widget.NewLabel("Only your key opens the secrets.")).Add("Name", name).Add("For", user).Add(label, value)
	d := widget.NewDialog(title)
	if kind != secrets.Note {
		reveal := widget.NewCheckbox("Show the password")
		reveal.OnFlip(func(on bool, u *gunim.UI) { value.Secret = !on; u.Invalidate() })
		form.Add("", reveal)
		d.AddAction("Make One Up", func(u *gunim.UI) {
			if made, err := secrets.NewPassword(secrets.PasswordLength); err == nil {
				value.SetText(made)
				u.Invalidate()
			}
		})
	}
	d.Body = form
	d.SetButtons("Save", "Cancel")
	d.Check = func() string {
		switch {
		case strings.TrimSpace(name.Text()) == "":
			return "A secret needs a name."
		case old == nil && value.Text() == "":
			return "There is nothing to keep yet."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent {
		return PutSecret{ID: id, Name: strings.TrimSpace(name.Text()), User: strings.TrimSpace(user.Text()), Kind: kind, Value: value.Text()}
	}
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}

// confirmRemoveSecret asks before taking a secret out of the vault.
func (w *window) confirmRemoveSecret(it SecretItem, u *gunim.UI) {
	d := widget.NewDialog("Remove " + it.Name + "?")
	d.Body = widget.NewLabel("It goes from the secrets for good.")
	d.SetButtons("Remove", "Cancel")
	d.Danger = true
	d.Accept, d.Dismiss = RemoveSecret{ID: it.ID}, DialogClosed{}
	w.openDialog(d, u)
}
