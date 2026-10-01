package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/marrasen/kakel/secrets"
)

// Signing in with the secrets: a password or a key's passphrase the
// secrets keep for it is used without asking, and the question that
// asks offers the saved secrets to answer with, and to keep what is
// typed. What was typed or picked is kept once the connection has
// gone through, so a password the server refused is never saved.

// signIn is one answer to keep: a password for login, user@host, or a
// key file's passphrase. value is what was typed, or picked the ID of
// the saved secret chosen instead.
type signIn struct {
	login, user, file string
	value, picked     string
}

// signIns are a connection's answers to keep, the last for each login
// or key file: one asked again was refused, and is dropped.
type signIns struct{ list []signIn }

// put keeps in, in place of an earlier answer for the same login or
// key file.
func (k *signIns) put(in signIn) {
	k.list = slices.DeleteFunc(k.list, func(o signIn) bool {
		return o.login != "" && o.login == in.login || o.file != "" && o.file == in.file
	})
	k.list = append(k.list, in)
}

// inHand runs f on the program's goroutine and returns what it gives,
// or "" when ctx ends first.
func (q asker) inHand(ctx context.Context, f func() string) string {
	got := make(chan string, 1)
	select {
	case q.a.events <- func() { got <- f() }:
	case <-ctx.Done():
		return ""
	}
	select {
	case s := <-got:
		return s
	case <-ctx.Done():
		return ""
	}
}

// vaultInHand is the secrets when they open without asking: open
// already, or opened by a key already unlocked. Nil otherwise. It never
// asks, as the key being unlocked may be the one the secrets need.
func (a *app) vaultInHand() *secrets.Vault {
	v, err := a.vault()
	if err != nil || !v.Exists() {
		return nil
	}
	if v.Locked() && v.Unlock(a.ring.Signers()) != nil {
		return nil
	}
	return v
}

// passwordInHand is the password the secrets keep for login, or "".
func (a *app) passwordInHand(login string) string {
	v := a.vaultInHand()
	if v == nil {
		return ""
	}
	pass, err := v.PasswordFor(login)
	if err != nil {
		return ""
	}
	return pass
}

// offered are the saved secrets a question offers, passwords and
// passphrases, by name.
func (a *app) offered() []secrets.Item {
	v := a.vaultInHand()
	if v == nil {
		return nil
	}
	items, err := v.Items()
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(items, func(it secrets.Item) bool { return it.Kind == secrets.Note })
}

// askSecret asks q, a question with one secret field, offering the
// saved secrets to answer with and a box, keep, to keep what is typed.
// The answer is kept as about, once the connection goes through.
func (q asker) askSecret(ctx context.Context, ask Ask, keep string, about signIn) (string, error) {
	var items []secrets.Item
	if q.kept != nil {
		got := make(chan []secrets.Item, 1)
		select {
		case q.a.events <- func() { got <- q.a.offered() }:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		select {
		case items = <-got:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		ask.Also = keep
		for _, it := range items {
			ask.Saved = append(ask.Saved, it.Name)
		}
	}
	for {
		ans, err := q.a.ask(ctx, ask)
		if err != nil {
			return "", err
		}
		typed := ans.Answers[0]
		if q.kept == nil {
			return typed, nil
		}
		rest := ans.Answers[1:]
		ticked := len(rest) > 0 && rest[0] == "yes"
		if len(items) > 0 && len(rest) > 1 {
			if i, err := strconv.Atoi(rest[len(rest)-1]); err == nil && i >= 0 && i < len(items) {
				id := items[i].ID
				value := q.inHand(ctx, func() string { return q.a.secretValue(id) })
				if value == "" {
					// Locked since, or gone: asked again, not answered
					// with what was typed before the pick.
					ask.Text = "That saved secret couldn't be read. Type it, or pick another."
					continue
				}
				about.picked = id
				q.kept.put(about)
				return value, nil
			}
		}
		if ticked && typed != "" {
			about.value = typed
			q.kept.put(about)
		}
		return typed, nil
	}
}

// secretValue is the value of the saved secret id, or "".
func (a *app) secretValue(id string) string {
	v := a.vaultInHand()
	if v == nil {
		return ""
	}
	s, err := v.Secret(id)
	if err != nil {
		return ""
	}
	return s
}

// keepSignIns keeps a connection's answers in the secrets, now that it
// has gone through: a secret picked is linked to the login or key it
// answered, and one typed with its box ticked is saved, in place of the
// one the secrets kept for it before. Secrets that need their own
// passphrase ask for it.
func (a *app) keepSignIns(k *signIns) {
	if k == nil || len(k.list) == 0 {
		return
	}
	list := k.list
	v, err := a.vault()
	if err != nil {
		a.failed("Couldn't keep the sign-in in the secrets", err.Error())
		return
	}
	keep := func() {
		for _, in := range list {
			said, err := keepSignIn(v, in)
			if err != nil {
				a.failed("Couldn't keep the sign-in for "+in.what()+" in the secrets", err.Error())
				continue
			}
			if said != "" {
				a.worked(said, "Used from now on for "+in.what()+".", "")
			}
		}
	}
	if v.Exists() && !v.Locked() {
		keep()
		return
	}
	if v.Exists() && v.Unlock(a.ring.Signers()) == nil {
		keep()
		return
	}
	if !v.Exists() {
		a.failed("Couldn't keep the sign-in in the secrets", "There are no secrets yet. Open Secrets to start them.")
		return
	}
	go a.unlockVault(v, "Couldn't keep the sign-in in the secrets", keep)
}

// what names what in signs in to, for a notice.
func (in signIn) what() string {
	if in.file != "" {
		return filepath.Base(in.file)
	}
	return in.login
}

// keepSignIn keeps one answer in v, and says what it did, or "" for
// nothing to do. A secret answering for this login or key and nothing
// else is changed; one shared with other logins or a key keeps its
// value, and only stops answering here.
func keepSignIn(v *secrets.Vault, in signIn) (string, error) {
	items, err := v.Items()
	if err != nil {
		return "", err
	}
	answers := func(it secrets.Item) bool {
		if in.file != "" {
			return it.File == in.file
		}
		return slices.ContainsFunc(it.Logins, func(l string) bool { return secrets.SameLogin(l, in.login) })
	}
	// unlinked is it no longer answering here.
	unlinked := func(it secrets.Item) secrets.Item {
		if in.file != "" {
			it.File = ""
		} else {
			it.Logins = slices.DeleteFunc(slices.Clone(it.Logins), func(l string) bool { return secrets.SameLogin(l, in.login) })
		}
		return it
	}
	was := slices.IndexFunc(items, answers)
	if in.picked != "" {
		at := slices.IndexFunc(items, func(it secrets.Item) bool { return it.ID == in.picked })
		if at < 0 {
			return "", secrets.ErrNoSuchItem
		}
		it := items[at]
		if answers(it) {
			return "", nil
		}
		// Refused before anything is written, so nothing is left half
		// done.
		if in.file != "" && it.File != "" {
			return "", errors.New(it.Name + " already unlocks " + it.File + ", and a secret unlocks one key")
		}
		if was >= 0 {
			if _, err := v.PutDetails(unlinked(items[was])); err != nil {
				return "", err
			}
		}
		if in.file != "" {
			it.File = in.file
		} else {
			it.Logins = append(slices.Clone(it.Logins), in.login)
		}
		if _, err := v.PutDetails(it); err != nil {
			if was >= 0 {
				_, _ = v.PutDetails(items[was])
			}
			return "", err
		}
		return it.Name + " linked", nil
	}
	if was >= 0 {
		old := items[was]
		alone := old.File == "" && len(old.Logins) == 1
		if in.file != "" {
			alone = len(old.Logins) == 0
		}
		if alone {
			// Typed again, as the one kept was refused: it changes.
			if _, err := v.Put(old, in.value); err != nil {
				return "", err
			}
			return old.Name + " changed", nil
		}
		if _, err := v.PutDetails(unlinked(old)); err != nil {
			return "", err
		}
	}
	it := secrets.Item{Name: in.login, Kind: secrets.Password, User: in.user, Logins: []string{in.login}}
	if in.file != "" {
		it = secrets.Item{Name: filepath.Base(in.file), Kind: secrets.Passphrase, File: in.file}
	}
	if _, err := v.Put(it, in.value); err != nil {
		if was >= 0 {
			_, _ = v.PutDetails(items[was])
		}
		return "", err
	}
	return it.Name + " saved in the secrets", nil
}
