package secrets

import (
	"encoding/csv"
	"fmt"
	"io"
)

// Export is one secret on its way out, with its value in it.
//
// The one shape in this package that carries a value beside a name.
// Everything else hands over one secret at a time, on purpose; this is
// for the one job that needs them all, which is leaving.
type Export struct {
	Item
	Value string
}

// Everything is every secret in the vault, with its value.
//
// The only call here that brings them all into the open at once, and
// the only one that should. It exists so that a password manager
// nobody can leave is not what this is: whoever keeps passwords here
// can take them somewhere else whenever they like.
//
// The values come back as strings, which Go cannot wipe. They are
// strings inside the vault too, so this is no more exposure than
// having it open at all.
func (v *Vault) Everything() ([]Export, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data == nil {
		return nil, ErrLocked
	}
	v.refresh()
	out := make([]Export, 0, len(v.items))
	for _, e := range v.items {
		out = append(out, Export{Item: e.Item, Value: e.Value})
	}
	return out, nil
}

// csvHeader is the first line of the file, and the order of every line
// under it.
//
// The browser shape first -- name, url, username, password -- because
// there is no standard CSV and that is the nearest thing to one: Chrome
// insists on url, username and password as headers, and the managers
// that read a Chrome export read this. Then notes, which is where a
// note's own text goes.
//
// Then kind and file, which are this window's own. They are what lets
// gridterm read its own export back without losing what it knows, and
// every importer ignores a column it does not recognise.
var csvHeader = []string{"name", "url", "username", "password", "notes", "kind", "file"}

// WriteCSV writes secrets as comma-separated values.
//
// One format because there is one worth writing today. The standard
// that is arriving, FIDO's Credential Exchange Format, is JSON and is
// not yet something other managers will read from a file; when it is,
// it is another function beside this one and not another feature.
func WriteCSV(w io.Writer, out []Export) error {
	rows := csv.NewWriter(w)
	if err := rows.Write(csvHeader); err != nil {
		return fmt.Errorf("secrets: write the header: %w", err)
	}
	for _, e := range out {
		if err := rows.Write(csvRow(e)); err != nil {
			return fmt.Errorf("secrets: write %s: %w", e.Name, err)
		}
	}
	rows.Flush()
	if err := rows.Error(); err != nil {
		return fmt.Errorf("secrets: write the secrets: %w", err)
	}
	return nil
}

// csvRow is one secret as a line.
//
// Where the value goes depends on what it is. A note is text somebody
// keeps -- a licence, a recovery code -- so it goes in the notes
// column, which is what it is and what the manager reading this will
// show it as. A password and a key's passphrase both go in the
// password column, because that is what they are.
func csvRow(e Export) []string {
	password, notes := e.Value, e.Notes
	if e.Kind == Note {
		password, notes = "", e.Value
	}
	kind := e.Kind
	if kind == "" {
		kind = Password
	}
	return []string{e.Name, e.URL, e.User, password, notes, string(kind), e.File}
}
