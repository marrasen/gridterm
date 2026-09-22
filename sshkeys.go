package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/secrets"
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
const makeKeyTitle = "New SSH Key"

// openMakeKey asks where to write a new key pair and what to call it.
func (a *app) openMakeKey() error {
	f := a.newForm(makeKeyTitle)
	where := f.AddField(fldFile, a.newField("Private key path", 0))
	who := f.AddField(fldComment, a.newField("Optional", 0))
	// Before the two fields it turns off, so it is read before a
	// passphrase has been typed into one of them. Offered only where it
	// can work: the passphrase goes into the vault, so there has to be
	// a vault to put it in.
	var generate *ui.Field
	if a.haveSecrets() {
		generate = f.AddTick(fldGeneratePass, false)
		generate.Hint = "Saved in the secrets and used automatically"
	}
	pass := a.newField("Optional", 0)
	pass.Mask = '*'
	f.AddField(fldPassphrase, pass)
	// Twice, because it is masked and the key cannot be written again at
	// the same path: a typo makes a key nobody can open.
	again := a.newField("", 0)
	again.Mask = '*'
	f.AddField(fldConfirmPass, again)
	if generate != nil {
		// Disabled rather than accepted and dropped on save: a
		// passphrase typed into a field that will not be used is one
		// the user believes they set.
		generate.OnChange = func(string) {
			on := generate.On()
			pass.Disabled, again.Disabled = on, on
			if on {
				pass.SetText("")
				again.SetText("")
			}
			a.markDirty()
		}
	}
	f.Lines = []string{
		"Creates an ed25519 key pair.",
		"The public key is saved as <file>.pub.",
	}
	if at, err := remote.DefaultKeyPath(); err == nil {
		where.SetText(at)
	} else {
		// Said where it can be read rather than only logged: the field
		// is empty and the user is about to wonder why.
		a.logError(err)
	}
	a.completePath(where, vfs.NewLocal())

	f.AddButton(ui.Button{Title: btnCreate, Do: func() error {
		comment := strings.TrimSpace(who.Text())
		if generate != nil && generate.On() {
			at, err := fromHome(where.Text())
			if err != nil {
				return err
			}
			// Not from here: this dialog closes as soon as this returns,
			// and closing one takes anything stacked on top of it. The
			// vault may have a passphrase dialog of its own to put up.
			a.pump.post(func() { a.makeKeyWithSavedPassphrase(at, comment) })
			return nil
		}
		if pass.Text() != again.Text() {
			// Returned rather than shown here, so the dialog stays open
			// with what was typed still there to correct.
			return errors.New("Passphrases do not match")
		}
		at, err := fromHome(where.Text())
		if err != nil {
			return err
		}
		key, err := remote.MakeKey(at, comment, pass.Text())
		if err != nil {
			return err
		}
		a.keyWritten(key, false)
		return nil
	}})
	f.AddButton(ui.Button{Title: btnCancel})
	a.showForm(f, nil)
	return nil
}

// couldNotCreateTheKey heads whatever went wrong on the way to a key.
const couldNotCreateTheKey = "Could not create the key"

// makeKeyWithSavedPassphrase writes a key locked with a passphrase
// nobody is shown, kept in the vault so the window opens the key itself
// from here on.
//
// The passphrase reaches the vault before the key is written with it.
// The other way round leaves, on a save that failed, a key on disk
// locked with a passphrase that exists nowhere: nobody opens that key
// again, and the path cannot be used twice.
func (a *app) makeKeyWithSavedPassphrase(at, comment string) {
	err := a.withOpenSecrets(couldNotCreateTheKey, func(v *secrets.Vault) error {
		// What is at the path decides everything below, so it is asked
		// first. MakeKey refuses a path that has a key on it, and it is
		// called after the passphrase is saved: without this the user
		// would be told about a passphrase when what is in the way is a
		// file.
		//
		// Lstat and only ErrNotExist, both to match the look MakeKey
		// itself takes. Stat follows a symlink, so a key linked to a
		// volume that is not mounted read as a free path here and as a
		// taken one there; and any other trouble reading the path --
		// a directory this user cannot look in, a home that has hung --
		// is not a free path either.
		switch _, err := os.Lstat(at); {
		case err == nil:
			return fmt.Errorf("there is already a key at %s", at)
		case !errors.Is(err, os.ErrNotExist):
			return fmt.Errorf("look at %s: %w", at, err)
		}
		pass, err := secrets.NewPassword(secrets.PassphraseLength)
		if err != nil {
			return err
		}
		// A passphrase already filed under this path, left by an
		// attempt that got as far as saving one and no further, or by a
		// key that has been moved or deleted since.
		//
		// It is taken off the path and kept, not written over. The path
		// is free, but the key that passphrase was made for may be
		// alive somewhere else -- moved, or copied to the second
		// machine this vault has a slot for -- and this item is the
		// only record of it. Nothing on screen ever showed it.
		//
		// Left detached if the key below is never written: the path is
		// empty either way, so an item claiming it was wrong to begin
		// with.
		if old, found := passphraseFor(v, at); found {
			old.File = ""
			if _, err := v.PutDetails(old); err != nil {
				return err
			}
		}
		it, err := v.Put(secrets.Item{
			Name: filepath.Base(at),
			Kind: secrets.Passphrase,
			File: at,
		}, pass)
		if err != nil {
			return err
		}
		key, err := remote.MakeKey(at, comment, pass)
		if err != nil {
			// No key, so what was saved is the passphrase of nothing.
			return errors.Join(err, v.Remove(it.ID))
		}
		a.keyWritten(key, true)
		return nil
	})
	if err != nil {
		a.reportError(couldNotCreateTheKey, err)
	}
}

// passphraseFor is the item holding a key file's passphrase, for a
// caller that needs the item rather than the secret in it.
func passphraseFor(v *secrets.Vault, keyFile string) (secrets.Item, bool) {
	items, err := v.Items()
	if err != nil {
		return secrets.Item{}, false
	}
	for _, it := range items {
		if it.Kind == secrets.Passphrase && it.File == keyFile {
			return it, true
		}
	}
	return secrets.Item{}, false
}

// keyWritten says where a new key went and how to install it, and adds
// it to the list of key files the window offers.
func (a *app) keyWritten(key remote.NewKey, savedPassphrase bool) {
	// The key is on disk, so it is said before anything else: a list
	// that could not be written is not a reason to leave the user
	// without the line to paste.
	kept := a.keyFiles.keep(key.Path)
	a.pump.post(func() {
		a.refreshServers()
		lines, err := installKeyLines(key, savedPassphrase)
		if err != nil {
			a.reportError("Key created, but install steps are unavailable", err)
			return
		}
		n := a.newNotice(madeKeyTitle(key), lines)
		// Paths and a key line, which the dialog would otherwise
		// re-wrap at the spaces.
		n.Preformatted = true
		// The public key on its own: it is the one thing anybody takes
		// away from this dialog, and Copy would hand over the whole
		// page of instructions around it.
		line := key.Line
		n.Action = ui.NoticeAction{Title: btnCopyPublicKey, Do: func() {
			a.clip.set(line)
		}}
		n.FocusOK()
		a.presentNotice(n)
		if kept != nil {
			a.reportError("Key created, but not added to the list", kept)
		}
	})
}

// madeKeyTitle names the notice that says where a new key went.
//
// The constant rather than the words: every title this window draws is
// one, so rewording costs one edit and not a search.
func madeKeyTitle(remote.NewKey) string { return dlgKeyCreated }

// installKeyLines says how to put the public half where it is needed,
// and where the passphrase went when the window kept one.
func installKeyLines(key remote.NewKey, savedPassphrase bool) (string, error) {
	var b strings.Builder
	b.WriteString("Private key:  " + key.Path + "\n")
	b.WriteString("Public key:   " + key.Pub + "\n\n")
	if savedPassphrase {
		// Said because it cannot be typed. It was never on screen, and
		// the window is what opens this key from now on.
		b.WriteString("The passphrase is saved in the secrets.\n\n")
	}
	b.WriteString(key.Line + "\n\n")
	b.WriteString("To install it on a server:\n\n")
	b.WriteString("  ssh-copy-id -i " + key.Pub + " user@host\n\n")
	b.WriteString("Or add the line above to ~/.ssh/authorized_keys there.\n\n")
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
