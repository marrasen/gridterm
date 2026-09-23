package secrets

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Duplicates says what to do about a secret an import names that the
// vault already holds.
type Duplicates int

const (
	// KeepBoth puts the new one in beside the old. The safe answer and
	// the one this takes unless told otherwise: nothing somebody
	// already has should disappear because a file said so.
	KeepBoth Duplicates = iota

	// SkipThem leaves what is here and drops the one from the file.
	SkipThem

	// ReplaceThem puts the one from the file over what is here.
	ReplaceThem
)

// columns are the header names this understands, by what they mean.
//
// There is no standard CSV. Every manager writes its own headers, so
// this reads the ones they write: Chrome and the browsers, Bitwarden,
// LastPass, KeePassXC, 1Password. Matched without case, because they
// do not agree on that either.
var columns = map[string][]string{
	"name":     {"name", "title", "account"},
	"url":      {"url", "uri", "login_uri", "website", "web site"},
	"username": {"username", "user", "login", "login_username", "email", "user name"},
	"password": {"password", "pass", "login_password"},
	"notes":    {"notes", "note", "extra", "comment", "comments"},
	"kind":     {"kind"},
	"file":     {"file"},
}

// ErrNothingToRead says a file has no column an import could use.
var ErrNothingToRead = errors.New(
	"secrets: the file has no password and no notes column, so there is nothing in it to keep")

// ReadCSV reads secrets out of a file another manager wrote.
//
// The header says which column is which. A row with neither a password
// nor a note in it is passed over: it is a bookmark, not a secret.
func ReadCSV(r io.Reader) ([]Export, error) {
	rows := csv.NewReader(r)
	// Managers differ on how many columns they write per row, and a
	// trailing empty one is common. Let the rows be ragged and read
	// what is there.
	rows.FieldsPerRecord = -1
	all, err := rows.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("secrets: read the file: %w", err)
	}
	if len(all) < 2 {
		return nil, ErrNothingToRead
	}
	at := headerOf(all[0])
	if at["password"] < 0 && at["notes"] < 0 {
		return nil, ErrNothingToRead
	}
	var out []Export
	for _, row := range all[1:] {
		if e, ok := rowOf(at, row); ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// headerOf says which column holds what, or -1 where the file has none.
func headerOf(head []string) map[string]int {
	at := map[string]int{}
	for what := range columns {
		at[what] = -1
	}
	for i, name := range head {
		if i == 0 {
			// Excel writes one in front of the first header, and it is
			// not whitespace, so trimming does not reach it. Left
			// there, the first column matches nothing: a file whose
			// first column is the password is refused outright.
			name = strings.TrimPrefix(name, "\ufeff")
		}
		name = strings.ToLower(strings.TrimSpace(name))
		for what, names := range columns {
			if at[what] < 0 && slices.Contains(names, name) {
				at[what] = i
			}
		}
	}
	return at
}

// rowOf turns one line into a secret, and says whether there was one.
func rowOf(at map[string]int, row []string) (Export, bool) {
	get := func(what string) string {
		i := at[what]
		if i < 0 || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	password, notes := get("password"), get("notes")
	e := Export{Item: Item{
		Name: get("name"),
		User: get("username"),
		URL:  get("url"),
		Kind: kindOf(get("kind")),
	}}
	// Only where the file says this is a key's passphrase. A row of
	// somebody else's export naming a key file on this machine would
	// otherwise file itself against that key, and the real passphrase
	// for it could not be saved afterwards.
	if e.Kind == Passphrase {
		e.File = get("file")
	}
	switch {
	case e.Kind == Note:
		e.Value = notes
	case e.Kind == Passphrase, password != "":
		e.Value = password
		if e.Kind == "" {
			e.Kind = Password
		}
	case notes != "":
		// No password, so what is kept is the note. Every manager has
		// a row like this: a licence, a recovery code, an answer to a
		// security question.
		e.Value, e.Kind = notes, Note
	default:
		// Neither. A bookmark, or a row of somebody's folder names.
		return Export{}, false
	}
	if e.Value == "" {
		return Export{}, false
	}
	if e.Name == "" {
		// A secret needs a name, and a file that gave none still gave
		// something worth keeping. What it is for is the best name
		// there is.
		e.Name = firstOf(e.URL, e.User, string(e.Kind))
	}
	return e, true
}

// kindOf is what a file's kind column means here, and nothing for a
// word this window does not use.
//
// A file is somebody else's and says what it likes. An unknown word
// stored as a kind comes back out of the list as itself and is read by
// everything here as "not a password", which is a row nobody can
// explain.
func kindOf(said string) Kind {
	switch k := Kind(strings.ToLower(strings.TrimSpace(said))); k {
	case Password, Note, Passphrase:
		return k
	}
	return ""
}

// firstOf is the first of these that says anything.
func firstOf(of ...string) string {
	for _, one := range of {
		if one != "" {
			return one
		}
	}
	return "imported"
}

// Import puts many secrets in at once, and writes the file once.
//
// One save rather than one per secret: a file of two hundred logins is
// two hundred writes otherwise, and a failure half way through leaves
// half of them in.
func (v *Vault) Import(in []Export, dup Duplicates) (added, skipped int, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data == nil {
		return 0, 0, ErrLocked
	}
	v.refresh()
	was := slices.Clone(v.items)
	now := v.now()
	for _, e := range in {
		if strings.TrimSpace(e.Name) == "" {
			continue
		}
		at := slices.IndexFunc(v.items, func(held entry) bool { return sameSecret(held.Item, e.Item) })
		switch {
		case at < 0:
			id, err := newID()
			if err != nil {
				v.items = was
				return 0, 0, err
			}
			it := e.Item
			it.ID, it.Made, it.Changed = id, now, now
			if it.Kind == "" {
				it.Kind = Password
			}
			if err := v.onlyPassphraseFor(it); err != nil {
				// A passphrase for a key the vault already has one for.
				// Passed over rather than refused: the rest of the file
				// is worth having.
				skipped++
				continue
			}
			v.items = append(v.items, entry{Item: it, Value: e.Value})
			added++
		case dup == SkipThem:
			skipped++
		case dup == ReplaceThem:
			it := v.items[at].Item
			it.User, it.URL, it.Changed = e.User, e.URL, now
			v.items[at] = entry{Item: it, Value: e.Value}
			added++
		default:
			id, err := newID()
			if err != nil {
				v.items = was
				return 0, 0, err
			}
			it := e.Item
			it.ID, it.Made, it.Changed = id, now, now
			if it.Kind == "" {
				it.Kind = Password
			}
			// The same guard the branch above has. Keeping both is
			// right for a password and impossible for a key's
			// passphrase: the vault takes one per key file, and two
			// would leave the user unable to say which locks the key.
			// Reading this window's own export back in with the answer
			// it offers first went straight through this branch.
			if err := v.onlyPassphraseFor(it); err != nil {
				skipped++
				continue
			}
			v.items = append(v.items, entry{Item: it, Value: e.Value})
			added++
		}
	}
	if added == 0 {
		return 0, skipped, nil
	}
	if err := v.save(); err != nil {
		v.items = was
		return 0, 0, err
	}
	return added, skipped, nil
}

// sameSecret reports whether an imported secret is one the vault
// already holds: the same name, for the same thing, of the same kind.
//
// Not the value. A password that has been changed at one end and not
// the other is the same secret with a new value, which is exactly what
// Replace is for.
func sameSecret(held, coming Item) bool {
	kind := coming.Kind
	if kind == "" {
		kind = Password
	}
	return strings.EqualFold(held.Name, coming.Name) &&
		strings.EqualFold(held.User, coming.User) &&
		held.Kind == kind
}
