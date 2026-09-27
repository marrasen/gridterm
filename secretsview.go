package main

import (
	"strconv"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/gridterm/remote"
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
	opens *buttonBar
	keys  *widget.Table
	col   *widget.Flex
	st    Secrets
	byID  map[widget.Key]SecretItem
	// The header's buttons and the bar's.
	add, note, lock, unlock         *widget.Button
	typ, cp, reveal, change, remove *widget.Button
	addKey, addPass, removeKey      *widget.Button
	keyNames                        map[widget.Key]SecretKey
}

func newSecretsPane(w *window) *secretsPane {
	p := &secretsPane{w: w, head: newButtonBar(), act: newButtonBar(), opens: newButtonBar(), byID: map[widget.Key]SecretItem{}, keyNames: map[widget.Key]SecretKey{}}
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
	p.keys = widget.NewTable(widget.TableColumn{Title: "Key"}, widget.TableColumn{Title: "", Width: 340})
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
	p.remove.OnActivate(func(u *gunim.UI) {
		// The ones marked with Space, as gridterm removes several at
		// once, or the one under the cursor.
		var picked []SecretItem
		for _, k := range p.table.Marked() {
			picked = append(picked, p.byID[k])
		}
		if len(picked) > 1 {
			p.w.confirmRemoveSecrets(picked, u)
			return
		}
		if len(picked) == 1 {
			p.w.confirmRemoveSecret(picked[0], u)
			return
		}
		if k, ok := p.table.Cursor(); ok {
			p.w.confirmRemoveSecret(p.byID[k], u)
		}
	})
	p.addKey, p.addPass, p.removeKey = button("Add Key"), button("Add Passphrase"), button("Remove")
	p.addKey.On = AddSecretsKey{}
	p.addPass.OnActivate(func(u *gunim.UI) { p.w.passphraseForm(p.st, u) })
	p.removeKey.OnActivate(func(u *gunim.UI) {
		if k, ok := p.keys.Cursor(); ok {
			p.w.confirmRemoveKey(p.st, p.keyNames[k], u)
		}
	})
	p.col = widget.Column(p.head, p.table, p.act, p.opens, p.keys).Grow(p.table, 3).Grow(p.keys, 1)
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
	if st.Open {
		p.opens.set("What opens them", u, p.addKey, p.addPass, p.removeKey)
	} else {
		p.opens.set("", u)
	}
	switch {
	case !st.Open:
		p.head.set("Secrets", u, p.unlock)
		p.act.set("Locked. Unlock to see what is in them.", u)
	case len(st.Items) == 0:
		p.head.set(secretsHeading(st), u, p.add, p.note, p.lock)
		p.act.set("No secrets yet. Add one to keep it here, locked by your key.", u)
	default:
		p.head.set(secretsHeading(st), u, p.add, p.note, p.lock)
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
		d.AddAction("Generate", func(u *gunim.UI) {
			if made, err := secrets.NewPassword(secrets.PasswordLength); err == nil {
				value.SetText(made)
				value.Flash()
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

// confirmRemoveSecrets asks before removing several secrets at once.
func (w *window) confirmRemoveSecrets(items []SecretItem, u *gunim.UI) {
	names := make([]string, len(items))
	ids := make([]string, len(items))
	for i, it := range items {
		names[i], ids[i] = it.Name, it.ID
	}
	d := widget.NewDialog("Remove " + count(len(items), "secret") + "?")
	d.Body = widget.NewLabel(strings.Join(names, ", ") + ". They go from the secrets for good.")
	d.SetButtons("Remove "+strconv.Itoa(len(items)), "Cancel")
	d.Danger = true
	d.Accept, d.Dismiss = RemoveSecrets{IDs: ids}, DialogClosed{}
	w.openDialog(d, u)
}

// passphraseForm asks for a passphrase that opens the secrets where
// none of their keys is.
func (w *window) passphraseForm(st Secrets, u *gunim.UI) {
	if st.Passphrase {
		w.toasts.Show(widget.Toast{Title: "A passphrase opens the secrets already", Body: "Remove it first to set another."}, u)
		return
	}
	pass, again := widget.NewTextField(), widget.NewTextField()
	pass.Secret, again.Secret = true, true
	d := widget.NewDialog("Add Secrets Passphrase")
	d.Body = widget.NewForm().
		Add("", widget.NewLabel("A way in where none of their keys is. Anyone with a copy of the secrets can try passphrases against them, so make it long.")).
		Add("Passphrase", pass).Add("Again", again)
	d.SetButtons("Add", "Cancel")
	d.Check = func() string {
		switch {
		case pass.Text() == "":
			return "Type a passphrase."
		case pass.Text() != again.Text():
			return "The two passphrases differ."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent { return AddSecretsPassphrase{Passphrase: pass.Text()} }
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}

// confirmRemoveKey asks before a key, or the passphrase, stops opening
// the secrets, saying what still opens them after.
func (w *window) confirmRemoveKey(st Secrets, k SecretKey, u *gunim.UI) {
	if len(st.Keys) < 2 {
		w.toasts.Show(widget.Toast{Title: "Only one key opens the secrets", Body: "Add another first, so something still opens them."}, u)
		return
	}
	d := widget.NewDialog("Remove " + k.Name + "?")
	d.Body = widget.NewLabel(k.Removing)
	d.SetButtons("Remove", "Cancel")
	d.Danger = true
	d.Accept, d.Dismiss = RemoveSecretsKey{Fingerprint: k.Fingerprint}, DialogClosed{}
	w.openDialog(d, u)
}

// exportForm asks where to write the secrets, in plain text, for
// another manager to read.
func (w *window) exportForm(u *gunim.UI) {
	path := widget.NewTextField()
	path.SetText("~/secrets.csv")
	d := widget.NewDialog("Export Secrets")
	d.Body = widget.NewForm().
		Add("", widget.NewLabel("Every secret goes into the file in plain text. Anyone who can read the file can read them all.")).
		Add("File", path)
	d.SetButtons("Export", "Cancel")
	d.Danger = true
	d.Check = func() string {
		if strings.TrimSpace(path.Text()) == "" {
			return "Type where the file goes."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent { return ExportSecrets{Path: path.Text()} }
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}

// importForm asks for a CSV file another manager wrote, and what to do
// with a secret that is here already.
func (w *window) importForm(u *gunim.UI) {
	path := widget.NewTextField()
	path.Placeholder = "a CSV file, such as ~/Downloads/passwords.csv"
	dup := widget.NewDropdown(keepBoth, skipThem, replace)
	dup.Label = "One already here"
	d := widget.NewDialog("Import Secrets")
	d.Body = widget.NewForm().Add("File", path).Add("Already here", dup)
	d.SetButtons("Import", "Cancel")
	d.Check = func() string {
		if strings.TrimSpace(path.Text()) == "" {
			return "Type where the file is."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent {
		return ImportSecrets{Path: path.Text(), Duplicates: []string{keepBoth, skipThem, replace}[max(0, min(dup.Selected, 2))]}
	}
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}

// makeKeyDialog asks where to write a new SSH key, and with what
// passphrase: one typed, or, with the secrets there, one made up and
// kept in them.
func (w *window) makeKeyDialog(u *gunim.UI) {
	path, comment, pass, again := widget.NewTextField(), widget.NewTextField(), widget.NewTextField(), widget.NewTextField()
	if at, err := remote.DefaultKeyPath(); err == nil {
		path.SetText(at)
	}
	comment.Placeholder, pass.Placeholder = "optional", "optional"
	pass.Secret, again.Secret = true, true
	form := widget.NewForm().
		Add("", widget.NewLabel("Creates an ed25519 key pair. The public half is saved beside it, as the file's name with .pub.")).
		Add("File", path).Add("Comment", comment)
	var generate *widget.Checkbox
	if w.secretsExist {
		generate = widget.NewCheckbox("Make up a passphrase and keep it in the secrets")
		form.Add("", generate)
	}
	form.Add("Passphrase", pass).Add("Again", again)
	made := func() bool { return generate != nil && generate.On }
	d := widget.NewDialog("New SSH Key")
	d.Body = form
	d.SetButtons("Create", "Cancel")
	d.Check = func() string {
		switch {
		case strings.TrimSpace(path.Text()) == "":
			return "Type where the key goes."
		case !made() && pass.Text() != again.Text():
			return "The two passphrases differ."
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent {
		return MakeKey{Path: path.Text(), Comment: comment.Text(), Passphrase: pass.Text(), Generate: made()}
	}
	d.Dismiss = DialogClosed{}
	w.openDialog(d, u)
}

// secretsHeading is the pane's heading: the secrets, and the terminal
// waiting for one, when one is.
func secretsHeading(st Secrets) string {
	if st.Waiting != "" {
		return "Secrets — " + st.Waiting + " is waiting for one"
	}
	return "Secrets"
}
