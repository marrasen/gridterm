package view

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/conf"
	"github.com/marrasen/kakel/machines"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/secrets"
	"github.com/marrasen/kakel/words"
)

// secretsPane lists what is in the vault, by name, over a bar of what
// can be done with the one the cursor is on. Enter copies it. Under
// the list are the keys that open the vault.
type secretsPane struct {
	w     *Window
	head  *buttonBar
	table *widget.Table
	act   *buttonBar
	opens *buttonBar
	keys  *widget.Table
	col   *widget.Flex
	// find narrows the list to the secrets whose name or what they are
	// for holds what is typed in it.
	find *widget.TextField
	st   app.Secrets
	byID map[widget.Key]app.SecretItem
	// The header's buttons and the bar's.
	add, note, lock, unlock         *widget.Button
	typ, cp, reveal, change, remove *widget.Button
	addKey, addPass, removeKey      *widget.Button
	keyNames                        map[widget.Key]app.SecretKey
}

func newSecretsPane(w *Window) *secretsPane {
	p := &secretsPane{w: w, head: newButtonBar(), act: newButtonBar(), opens: newButtonBar(), byID: map[widget.Key]app.SecretItem{}, keyNames: map[widget.Key]app.SecretKey{}}
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
		return widget.TableRow{Cells: []string{it.Name, secretFor(it), kind}}
	}
	p.find = widget.NewTextField()
	p.find.Placeholder, p.find.Icon, p.find.Clearable = "Find a secret", icon.Search, true
	p.find.OnEdit = func(_ string, u *gunim.UI) { p.show(p.st, u) }
	p.table.OnActivate = func(k widget.Key, u *gunim.UI) { u.Send(p.table, app.CopySecret{ID: string(k)}) }
	// The fingerprint in a column of its own, wide enough for all of
	// it: half of one can seem to match the wrong key.
	p.keys = widget.NewTable(widget.TableColumn{Title: "Key"}, widget.TableColumn{Title: "", Width: 150},
		widget.TableColumn{Title: "Fingerprint", Width: 470})
	p.keys.Row = func(k widget.Key) widget.TableRow {
		s := p.keyNames[k]
		return widget.TableRow{Cells: []string{s.Name, s.Note, keyFingerprint(s)}, Faint: s.Passphrase}
	}
	button := iconButton
	p.add, p.note = button(icon.Plus, "Add Secret"), button(icon.StickyNote, "Add Note")
	p.lock, p.unlock = button(icon.Lock, "Lock"), button(icon.LockOpen, "Unlock")
	p.typ, p.cp, p.reveal = button(icon.Keyboard, "Type"), button(icon.Copy, "Copy"), button(icon.Eye, "Show")
	p.change, p.remove = button(icon.Pencil, "Change"), button(icon.Trash2, "Remove")
	p.lock.On, p.unlock.On = app.LockSecrets{}, app.UnlockSecrets{}
	p.add.OnActivate(func(u *gunim.UI) { p.w.secretForm(secrets.Password, nil, u) })
	p.note.OnActivate(func(u *gunim.UI) { p.w.secretForm(secrets.Note, nil, u) })
	onRow := func(b *widget.Button, do func(app.SecretItem, *gunim.UI)) {
		b.OnActivate(func(u *gunim.UI) {
			if k, ok := p.table.Cursor(); ok {
				do(p.byID[k], u)
			}
		})
	}
	onRow(p.typ, func(it app.SecretItem, u *gunim.UI) { u.Send(p.table, app.TypeSecret{ID: it.ID}) })
	onRow(p.cp, func(it app.SecretItem, u *gunim.UI) { u.Send(p.table, app.CopySecret{ID: it.ID}) })
	onRow(p.reveal, func(it app.SecretItem, u *gunim.UI) { u.Send(p.table, app.RevealSecret{ID: it.ID}) })
	onRow(p.change, func(it app.SecretItem, u *gunim.UI) { p.w.secretForm(it.Kind, &it, u) })
	p.remove.OnActivate(func(u *gunim.UI) {
		// The ones marked with Space, or the one under the cursor.
		var picked []app.SecretItem
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
	p.addKey, p.addPass, p.removeKey = button(icon.KeyRound, "Add Key"), button(icon.RectangleEllipsis, "Add Passphrase"),
		button(icon.Trash2, "Remove")
	p.addKey.On = app.AddSecretsKey{}
	p.addPass.OnActivate(func(u *gunim.UI) { p.w.passphraseForm(p.st, u) })
	p.removeKey.OnActivate(func(u *gunim.UI) {
		if k, ok := p.keys.Cursor(); ok {
			p.w.confirmRemoveKey(p.st, p.keyNames[k], u)
		}
	})
	p.col = widget.Column(p.head, p.find, p.table, p.act, p.opens, p.keys).Grow(p.table, 3).Grow(p.keys, 1)
	p.col.Cross, p.col.Gap = widget.CrossStretch, noGap
	return p
}

// show brings the pane up to date with the vault.
func (p *secretsPane) show(st app.Secrets, u *gunim.UI) {
	p.st = st
	clear(p.byID)
	keys := make([]widget.Key, 0, len(st.Items))
	finding := strings.ToLower(strings.TrimSpace(p.find.Text()))
	for _, it := range st.Items {
		p.byID[widget.Key(it.ID)] = it
		if finding != "" && !strings.Contains(strings.ToLower(it.Name+" "+secretFor(it)), finding) {
			continue
		}
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
		p.act.set("Locked", u)
	case len(st.Items) == 0:
		p.head.set(secretsHeading(st), u, p.add, p.note, p.lock)
		p.act.set("No secrets yet", u)
	default:
		p.head.set(secretsHeading(st), u, p.add, p.note, p.lock)
		switch shown := len(keys); {
		case shown == len(st.Items):
			p.act.set(words.Count(len(st.Items), "secret")+" · Enter copies the one selected", u, p.typ, p.cp, p.reveal, p.change, p.remove)
		case shown == 0:
			p.act.set("No matches in "+words.Count(len(st.Items), "secret"), u)
		default:
			p.act.set(strconv.Itoa(shown)+" of "+words.Count(len(st.Items), "secret")+" · Enter copies the one selected", u, p.typ, p.cp, p.reveal, p.change, p.remove)
		}
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
func (w *Window) secretForm(kind secrets.Kind, old *app.SecretItem, u *gunim.UI) {
	name, user, value := widget.NewTextField(), widget.NewTextField(), widget.NewTextField()
	user.Placeholder = "Optional"
	value.Secret = true
	// A note is lines of text, and a password one line, hidden.
	var field gunim.Node = value
	text := value.Text
	label, title, missing := "Password", "Add Secret", "Enter a password"
	if kind == secrets.Note {
		note := widget.NewTextArea()
		field, text = note, note.Text
		label, title, missing = "Note", "Add Note", "Enter a note"
		if old != nil {
			note.Placeholder = "Unchanged"
		}
	}
	id := ""
	if old != nil {
		id, title = old.ID, "Change "+old.Name
		name.SetText(old.Name)
		user.SetText(old.User)
		value.Placeholder = "Unchanged"
	} else if m := w.machineOf(w.lastTerm); m != "" {
		// The server in front of the user, which a password typed now
		// is nearly always for.
		user.SetText(w.nameOf(m))
	}
	// Who or what it is for, completed from the servers' names.
	servers := w.serverNames()
	user.OnEdit = func(text string, u *gunim.UI) { user.Ghost = restOf(false, servers, text) }
	form := widget.NewForm().Add("Name", name).Add("For", user).Add(label, field)
	d := widget.NewDialog(title)
	if kind != secrets.Note {
		reveal := widget.NewCheckbox("Show password")
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
			return "Enter a name"
		case old == nil && text() == "":
			return missing
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent {
		return app.PutSecret{ID: id, Name: strings.TrimSpace(name.Text()), User: strings.TrimSpace(user.Text()), Kind: kind, Value: text()}
	}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// confirmRemoveSecret asks before taking a secret out of the vault.
func (w *Window) confirmRemoveSecret(it app.SecretItem, u *gunim.UI) {
	d := widget.NewDialog("Remove " + it.Name + "?")
	d.Body = widget.NewLabel("This can't be undone.")
	d.SetButtons("Remove", "Cancel")
	d.Danger = true
	d.Accept, d.Dismiss = app.RemoveSecret{ID: it.ID}, app.DialogClosed{}
	w.openDialog(d, u)
}

// confirmRemoveSecrets asks before removing several secrets at once.
func (w *Window) confirmRemoveSecrets(items []app.SecretItem, u *gunim.UI) {
	names := make([]string, len(items))
	ids := make([]string, len(items))
	for i, it := range items {
		names[i], ids[i] = it.Name, it.ID
	}
	d := widget.NewDialog("Remove " + words.Count(len(items), "secret") + "?")
	d.Body = widget.NewLabel(strings.Join(names, ", ") + ". This can't be undone.")
	d.SetButtons("Remove "+strconv.Itoa(len(items)), "Cancel")
	d.Danger = true
	d.Accept, d.Dismiss = app.RemoveSecrets{IDs: ids}, app.DialogClosed{}
	w.openDialog(d, u)
}

// passphraseForm asks for a passphrase that opens the secrets where
// none of their keys is.
func (w *Window) passphraseForm(st app.Secrets, u *gunim.UI) {
	if st.Passphrase {
		w.toasts.Show(widget.Toast{Title: "The secrets already have a passphrase", Body: "Remove it to add another."}, u)
		return
	}
	pass, again := widget.NewTextField(), widget.NewTextField()
	pass.Secret, again.Secret = true, true
	d := widget.NewDialog("Add Secrets Passphrase")
	d.Body = widget.NewForm().
		Add("", widget.NewLabel("Unlocks your secrets on a computer without your SSH keys. Anyone with a copy of your secrets can try to guess it, so make it long.")).
		Add("Passphrase", pass).Add("Confirm", again)
	d.SetButtons("Add", "Cancel")
	d.Check = func() string {
		switch {
		case pass.Text() == "":
			return "Enter a passphrase"
		case pass.Text() != again.Text():
			return "Passphrases don't match"
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent { return app.AddSecretsPassphrase{Passphrase: pass.Text()} }
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// confirmRemoveKey asks before a key, or the passphrase, stops opening
// the secrets, saying what still opens them after.
func (w *Window) confirmRemoveKey(st app.Secrets, k app.SecretKey, u *gunim.UI) {
	if len(st.Keys) < 2 {
		w.toasts.Show(widget.Toast{Title: "Only one key unlocks the secrets", Body: "Add another key before removing this one."}, u)
		return
	}
	d := widget.NewDialog("Remove " + k.Name + "?")
	d.Body = widget.NewLabel(k.Removing)
	d.SetButtons("Remove", "Cancel")
	d.Danger = true
	d.Accept, d.Dismiss = app.RemoveSecretsKey{Fingerprint: k.Fingerprint}, app.DialogClosed{}
	w.openDialog(d, u)
}

// exportForm asks where to write the secrets, in plain text, for
// another manager to read.
func (w *Window) exportForm(u *gunim.UI) {
	// Nothing filled in: that there is no path until one is typed is
	// what keeps this from happening by accident. A question naming the
	// file follows.
	path := widget.NewTextField()
	path.Placeholder = "~/secrets.csv"
	completesPaths(path)
	d := widget.NewDialog("Export Secrets")
	d.Body = widget.NewForm().
		Add("", widget.NewLabel("The file holds every secret in plain text.")).
		Add("File", path)
	d.SetButtons("Export…", "Cancel")
	d.Check = func() string {
		if strings.TrimSpace(path.Text()) == "" {
			return "Enter a file"
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent { return app.ExportSecrets{Path: path.Text()} }
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// importForm asks for a CSV file another manager wrote, and what to do
// with a secret that is here already.
func (w *Window) importForm(u *gunim.UI) {
	path := widget.NewTextField()
	path.Placeholder = "~/Downloads/passwords.csv"
	completesPaths(path)
	dup := widget.NewDropdown(app.KeepBoth, app.SkipThem, app.Replace)
	dup.Label = "Duplicates"
	d := widget.NewDialog("Import Secrets")
	d.Body = widget.NewForm().Add("File", path).Add("Duplicates", dup)
	d.SetButtons("Import", "Cancel")
	d.Check = func() string {
		if strings.TrimSpace(path.Text()) == "" {
			return "Enter a file"
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent {
		return app.ImportSecrets{Path: path.Text(), Duplicates: []string{app.KeepBoth, app.SkipThem, app.Replace}[max(0, min(dup.Selected, 2))]}
	}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// makeKeyDialog asks where to write a new SSH key, and with what
// passphrase: one typed, or, with the secrets there, one made up and
// kept in them.
func (w *Window) makeKeyDialog(u *gunim.UI) {
	path, comment, pass, again := widget.NewTextField(), widget.NewTextField(), widget.NewTextField(), widget.NewTextField()
	if at, err := remote.DefaultKeyPath(); err == nil {
		path.SetText(at)
	}
	completesPaths(path)
	comment.Placeholder, pass.Placeholder = "Optional", "Optional"
	pass.Secret, again.Secret = true, true
	form := widget.NewForm().
		Add("", widget.NewLabel("Creates an ed25519 key pair. The public key is saved beside it, with .pub added to the name.")).
		Add("File", path).Add("Comment", comment)
	var generate *widget.Checkbox
	if w.secretsExist {
		generate = widget.NewCheckbox("Generate a passphrase and save it in your secrets")
		// Ticked, the passphrase fields take nothing: they are emptied
		// and disabled.
		generate.OnFlip(func(on bool, u *gunim.UI) {
			if on {
				pass.SetText("")
				again.SetText("")
			}
			pass.Disabled, again.Disabled = on, on
			u.Invalidate()
		})
		form.Add("", generate)
	}
	form.Add("Passphrase", pass).Add("Confirm", again)
	made := func() bool { return generate != nil && generate.On }
	d := widget.NewDialog("New SSH Key")
	d.Body = form
	d.SetButtons("Create", "Cancel")
	d.Check = func() string {
		switch {
		case strings.TrimSpace(path.Text()) == "":
			return "Enter a file"
		case !made() && pass.Text() != again.Text():
			return "Passphrases don't match"
		}
		// What would make it fail, said now, while what was typed is
		// still here to change.
		at, err := conf.Tilde(path.Text())
		if err == nil {
			err = remote.KeyPathProblem(at)
		}
		if err != nil {
			return words.UpperFirst(err.Error())
		}
		return ""
	}
	d.OnAccept = func() gunim.Intent {
		return app.MakeKey{Path: path.Text(), Comment: comment.Text(), Passphrase: pass.Text(), Generate: made()}
	}
	d.Dismiss = app.DialogClosed{}
	w.openDialog(d, u)
}

// secretsHeading is the pane's heading: the secrets, and the terminal
// waiting for one, when one is.
func secretsHeading(st app.Secrets) string {
	if st.Waiting != "" {
		return "Secrets — " + st.Waiting + " is waiting for one"
	}
	return "Secrets"
}

// secretFor is who or what a secret is for, as the list shows it: the
// key a passphrase opens by its whole path, which tells two keys with
// the same file name apart.
func secretFor(it app.SecretItem) string {
	if it.File != "" {
		return it.File
	}
	return it.User
}

// keyFingerprint is a key's fingerprint as the list shows it: none for
// the passphrase.
func keyFingerprint(k app.SecretKey) string {
	if k.Passphrase {
		return ""
	}
	return k.Fingerprint
}

// serverNames are the names a secret may be for: the saved servers',
// and the machines connected.
func (w *Window) serverNames() []string {
	var out []string
	for _, h := range w.saved {
		out = append(out, h.Name)
	}
	for _, m := range w.machines() {
		if n := w.nameOf(m); m != machines.Local && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

// completesPaths has a field that takes a path on this machine offer
// the rest of a file's or folder's name as it is typed, as Go To does.
func completesPaths(f *widget.TextField) {
	var c pathCompleter
	f.OnEdit = func(text string, _ *gunim.UI) { f.Ghost = c.rest(text) }
}

// pathCompleter completes paths on this machine, reading each folder
// once while it is typed in, rather than on every key: a folder of
// thousands, or one on a slow mount, would hold up the window each time.
type pathCompleter struct {
	dir   string
	names []string
}

// rest is what completes typed into a path on this machine, "" for
// none: the rest of the one name in its folder that starts so, or of
// the part the names that do share.
func (c *pathCompleter) rest(typed string) string {
	at, err := conf.ExpandHome(typed)
	if err != nil || typed == "" {
		return ""
	}
	dir, leaf := filepath.Split(at)
	// Typed up to a separator: a folder, with no name begun in it.
	if leaf == "" || strings.HasSuffix(typed, string(filepath.Separator)) || strings.HasSuffix(typed, "/") {
		return ""
	}
	if dir != c.dir {
		c.dir, c.names = dir, namesIn(dir)
	}
	return restOf(runtime.GOOS == "windows", c.names, leaf)
}

// restOfPath is a completer's rest, for a path typed once.
func restOfPath(typed string) string {
	var c pathCompleter
	return c.rest(typed)
}

// namesIn are the names in folder dir, a folder's, or a link's to one,
// ending in the separator.
func namesIn(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		isDir := e.IsDir()
		if e.Type()&fs.ModeSymlink != 0 {
			if info, err := os.Stat(filepath.Join(dir, n)); err == nil {
				isDir = info.IsDir()
			}
		}
		if isDir {
			n += string(filepath.Separator)
		}
		names = append(names, n)
	}
	return names
}
