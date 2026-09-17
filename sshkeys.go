package main

import (
	"errors"
	"strings"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/serve"
	"github.com/marrasen/gridterm/settings"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/vfs"
)

// keyIndex are the key files the user keeps, offered when a connection
// is made or edited.
type keyIndex struct {
	// remembered is where they are kept between runs. Nothing is saved
	// while it is nil.
	remembered *settings.Settings
}

// newKeyIndex builds an index that remembers nothing until it is given
// the settings.
func newKeyIndex() *keyIndex { return &keyIndex{} }

// remember gives the index the settings it reads and writes.
func (k *keyIndex) remember(set *settings.Settings) { k.remembered = set }

// all is every key file kept, newest first.
func (k *keyIndex) all() []string {
	if k.remembered == nil {
		return nil
	}
	return k.remembered.Keys()
}

// keep puts a key file at the front of the index.
func (k *keyIndex) keep(path string) error {
	if k.remembered == nil {
		return errNoSettingsForKeys
	}
	return k.remembered.KeepKey(path, mostKeptKeys)
}

// mostKeptKeys is how many key files are kept. Keeping one past that
// drops the one kept longest ago.
const mostKeptKeys = 20

// errNoSettingsForKeys is what an index with no settings behind it
// answers.
var errNoSettingsForKeys = errors.New("this window has no settings to keep a key file in")

// makeKeyTitle names the dialog that writes a new key pair.
const makeKeyTitle = "Make an SSH key"

// openMakeKey asks where to write a new key pair and what to call it.
func (a *app) openMakeKey() error {
	f := a.newForm(makeKeyTitle)
	where := f.AddField("File", a.newField("where to write the private half", 0))
	who := f.AddField("Comment", a.newField("what to call it, optional", 0))
	pass := a.newField("optional, asked for when the key is used", 0)
	pass.Mask = '*'
	f.AddField("Passphrase", pass)
	// Twice, because it is masked and the key cannot be written again at
	// the same path: a typo makes a key nobody can open.
	again := a.newField("the same again", 0)
	again.Mask = '*'
	f.AddField("Passphrase again", again)
	f.Lines = []string{
		"A new ed25519 key pair. The public half goes beside it with",
		".pub on the end, and the key is added to the list this window",
		"offers when you make a connection.",
	}
	if at, err := remote.DefaultKeyPath(); err == nil {
		where.SetText(at)
	} else {
		// Said where it can be read rather than only logged: the field
		// is empty and the user is about to wonder why.
		a.logError(err)
	}
	a.completePath(where, vfs.NewLocal())

	f.AddButton(ui.Button{Title: "Make it", Do: func() error {
		if pass.Text() != again.Text() {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return errors.New("the two passphrases are not the same")
		}
		at, err := fromHome(where.Text())
		if err != nil {
			return err
		}
		key, err := remote.MakeKey(at, strings.TrimSpace(who.Text()), pass.Text())
		if err != nil {
			return err
		}
		// The key is on disk, so it is said before anything else: a list
		// that could not be written is not a reason to leave the user
		// without the line to paste.
		kept := a.keyFiles.keep(key.Path)
		a.pump.post(func() {
			a.refreshServers()
			lines, err := installKeyLines(key)
			if err != nil {
				a.reportError("The key was made and this window cannot say where to install it", err)
				return
			}
			a.showNotice(madeKeyTitle(key), lines, false)
			if kept != nil {
				a.reportError("The key was made and not added to the list", kept)
			}
		})
		return nil
	}})
	f.AddButton(ui.Button{Title: "Cancel"})
	a.showForm(f, nil)
	return nil
}

// madeKeyTitle names the notice that says where a new key went.
func madeKeyTitle(key remote.NewKey) string { return "Made " + key.Path }

// installKeyLines says how to put the public half where it is needed.
func installKeyLines(key remote.NewKey) (string, error) {
	var b strings.Builder
	b.WriteString("The public half is at " + key.Pub + ":\n\n")
	b.WriteString(key.Line + "\n\n")
	b.WriteString("Add that line to ~/.ssh/authorized_keys on the machine you\n")
	b.WriteString("want to reach. From a shell that has ssh-copy-id, which runs\n")
	b.WriteString("here rather than there, this does it for you:\n\n")
	b.WriteString("  ssh-copy-id -i " + key.Pub + " user@machine\n\n")
	b.WriteString("For OpenSSH on Windows, add it to\n")
	b.WriteString(`  %USERPROFILE%\.ssh\authorized_keys` + "\n")
	b.WriteString("unless that account is an administrator, and then only to\n")
	b.WriteString(`  %ProgramData%\ssh\administrators_authorized_keys` + "\n")
	b.WriteString("which is the only file sshd reads for one. Either file has to\n")
	b.WriteString("be readable by its owner alone, or sshd ignores it and says\n")
	b.WriteString("nothing about why.\n\n")

	// This machine's own, and said so: the path a window serves from is
	// that machine's, and a Windows one is nothing like a Linux one.
	at, err := serve.AuthorizedKeysPath()
	if err != nil {
		return "", err
	}
	b.WriteString("For a gridterm window serving from this machine, add it to\n")
	b.WriteString("  " + at + "\n")
	b.WriteString("A window serving from another machine keeps that file\n")
	b.WriteString("wherever that machine puts its settings.")
	return b.String(), nil
}
