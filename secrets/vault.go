package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Kind says what an item is, which is what the list shows and what a
// copy means.
type Kind string

const (
	// Password is a secret to put on the clipboard or type into a pane.
	Password Kind = "password"
	// Note is text the user keeps: a recovery code, a licence, an
	// answer to a security question.
	Note Kind = "note"
)

// Item is one thing in the vault, without the secret in it.
//
// The list is drawn from these, so the window can show what is in the
// vault without holding any of it. Secret fetches the value for the one
// item a user asked for.
type Item struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Kind    Kind      `json:"kind"`
	User    string    `json:"user,omitempty"`
	Made    time.Time `json:"made"`
	Changed time.Time `json:"changed"`
}

// entry is an item with its secret, which is what the sealed half
// holds.
type entry struct {
	Item
	Value string `json:"value"`
}

// contents is everything under the data key.
type contents struct {
	Entries []entry `json:"entries"`
}

// ErrLocked says the vault has not been opened yet.
var ErrLocked = errors.New("secrets: the vault is locked")

// ErrNoSuchItem says nothing in the vault has that id.
var ErrNoSuchItem = errors.New("secrets: there is no such item")

// Vault is a file of secrets and, once unlocked, what is in it.
//
// A Vault is safe to use from several goroutines. It is locked until a
// key opens it, and Lock puts it back: the window locks it when it
// locks the SSH keys, so the two go together.
type Vault struct {
	path string

	mu   sync.Mutex
	file *file
	// data is the key the contents are sealed with, and nil while the
	// vault is locked. Its presence is what "unlocked" means.
	data  []byte
	items []entry

	// now is the clock, for a test that wants times it chose.
	now func() time.Time
}

// Open reads the vault at path. It comes back locked.
//
// A path with no file there is not an error: the answer is a vault that
// says it does not exist yet, which is what the window offers to
// create.
func Open(path string) (*Vault, error) {
	v := &Vault{path: path, now: time.Now}
	f, err := readFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return v, nil
	case err != nil:
		return nil, err
	}
	v.file = f
	return v, nil
}

// Exists reports whether there is a vault to open.
func (v *Vault) Exists() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.file != nil
}

// Locked reports whether the vault still needs a key.
func (v *Vault) Locked() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.data == nil
}

// Path is where the vault is kept.
func (v *Vault) Path() string { return v.path }

// Create makes a vault at path that signer opens, and leaves it
// unlocked.
//
// It refuses to write over one that is already there: the file holds
// the only copy of what is in it.
func Create(path string, signer ssh.Signer, keyFile string) (*Vault, error) {
	if err := usable(signer.PublicKey()); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("secrets: there is already a vault at %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("secrets: look for a vault at %s: %w", path, err)
	}

	data, err := newDataKey()
	if err != nil {
		return nil, err
	}
	s, err := wrapFor(signer, data, keyFile)
	if err != nil {
		wipe(data)
		return nil, err
	}
	v := &Vault{path: path, now: time.Now, data: data, file: &file{
		Version: fileVersion,
		Slots:   []slot{s},
	}}
	if err := v.save(); err != nil {
		wipe(data)
		return nil, err
	}
	return v, nil
}

// wrapFor builds a slot that signer opens, holding the data key.
func wrapFor(signer ssh.Signer, data []byte, keyFile string) (slot, error) {
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return slot{}, fmt.Errorf("secrets: take a challenge: %w", err)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return slot{}, fmt.Errorf("secrets: take a salt: %w", err)
	}
	key, err := slotKeyFrom(signer, challenge, salt)
	if err != nil {
		return slot{}, err
	}
	defer wipe(key)
	nonce, box, err := seal(key, data, nil)
	if err != nil {
		return slot{}, err
	}
	return slot{
		Kind:        slotKindSSH,
		Fingerprint: Fingerprint(signer.PublicKey()),
		KeyFile:     keyFile,
		Challenge:   challenge,
		Salt:        salt,
		Nonce:       nonce,
		Wrapped:     box,
	}, nil
}

// Wants reports the fingerprints of the keys that open this vault, so
// the window can pick one it already has unlocked rather than asking
// for a passphrase it does not need.
func (v *Vault) Wants() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.file == nil {
		return nil
	}
	out := make([]string, 0, len(v.file.Slots))
	for _, s := range v.file.Slots {
		out = append(out, s.Fingerprint)
	}
	return out
}

// Unlock opens the vault with the first of these signers that fits.
//
// Handed every key the window has unlocked, so opening a vault costs
// nothing once a server has been reached. A key that opens no slot is
// passed over rather than reported: the caller offered what it had.
func (v *Vault) Unlock(signers []ssh.Signer) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.file == nil {
		return fmt.Errorf("secrets: there is no vault at %s yet", v.path)
	}
	if v.data != nil {
		return nil
	}
	for _, signer := range signers {
		if usable(signer.PublicKey()) != nil {
			continue
		}
		want := Fingerprint(signer.PublicKey())
		for _, s := range v.file.Slots {
			if s.Kind != slotKindSSH || s.Fingerprint != want {
				continue
			}
			key, err := slotKeyFrom(signer, s.Challenge, s.Salt)
			if err != nil {
				return err
			}
			data, err := unseal(key, s.Nonce, s.Wrapped, nil)
			wipe(key)
			if err != nil {
				// The slot names this key and the key did not open it,
				// so the file has been tampered with or the key has
				// been replaced under the same name.
				return fmt.Errorf("secrets: the key %s is named by a slot it does not open: %w",
					want, err)
			}
			if err := v.readContents(data); err != nil {
				wipe(data)
				return err
			}
			v.data = data
			return nil
		}
	}
	return ErrWrongKey
}

// readContents unseals the items with the data key.
func (v *Vault) readContents(data []byte) error {
	if len(v.file.Sealed) == 0 {
		v.items = nil
		return nil
	}
	plain, err := unseal(data, v.file.Nonce, v.file.Sealed, nil)
	if err != nil {
		return fmt.Errorf("secrets: open the vault %s: %w", v.path, err)
	}
	var c contents
	if err := json.Unmarshal(plain, &c); err != nil {
		wipe(plain)
		return fmt.Errorf("secrets: read what is in the vault %s: %w", v.path, err)
	}
	wipe(plain)
	v.items = c.Entries
	return nil
}

// Lock drops the key and everything it opened. What is on disk stays.
func (v *Vault) Lock() {
	v.mu.Lock()
	defer v.mu.Unlock()
	wipe(v.data)
	v.data = nil
	for i := range v.items {
		v.items[i].Value = ""
	}
	v.items = nil
}

// Items is what is in the vault, by name, without any of the secrets.
func (v *Vault) Items() ([]Item, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data == nil {
		return nil, ErrLocked
	}
	out := make([]Item, 0, len(v.items))
	for _, e := range v.items {
		out = append(out, e.Item)
	}
	slices.SortFunc(out, func(a, b Item) int {
		if n := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// Secret is one item's value, which is the only way to reach one.
//
// Asked for by id and one at a time, so the window holds a secret only
// while it is putting it somewhere.
func (v *Vault) Secret(id string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data == nil {
		return "", ErrLocked
	}
	for _, e := range v.items {
		if e.ID == id {
			return e.Value, nil
		}
	}
	return "", ErrNoSuchItem
}

// Put adds an item or replaces the one with the same id, and saves.
//
// An item with no id is new and is given one.
func (v *Vault) Put(it Item, value string) (Item, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data == nil {
		return Item{}, ErrLocked
	}
	if strings.TrimSpace(it.Name) == "" {
		return Item{}, errors.New("secrets: an item needs a name")
	}
	if it.Kind == "" {
		it.Kind = Password
	}
	now := v.now()
	it.Changed = now
	at := -1
	if it.ID == "" {
		id, err := newID()
		if err != nil {
			return Item{}, err
		}
		it.ID, it.Made = id, now
	} else {
		at = slices.IndexFunc(v.items, func(e entry) bool { return e.ID == it.ID })
		if at < 0 {
			return Item{}, ErrNoSuchItem
		}
		it.Made = v.items[at].Made
	}

	was := slices.Clone(v.items)
	if at < 0 {
		v.items = append(v.items, entry{Item: it, Value: value})
	} else {
		v.items[at] = entry{Item: it, Value: value}
	}
	if err := v.save(); err != nil {
		// The file is what the vault is. A change that did not reach it
		// is not a change, so it is taken back rather than left to look
		// saved until the window closes.
		v.items = was
		return Item{}, err
	}
	return it, nil
}

// PutDetails changes what an item is called and who it is for, and
// leaves the secret alone.
//
// The value is never read, so putting a better name on something does
// not bring it out of the vault at all.
func (v *Vault) PutDetails(it Item) (Item, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data == nil {
		return Item{}, ErrLocked
	}
	if strings.TrimSpace(it.Name) == "" {
		return Item{}, errors.New("secrets: an item needs a name")
	}
	at := slices.IndexFunc(v.items, func(e entry) bool { return e.ID == it.ID })
	if at < 0 {
		return Item{}, ErrNoSuchItem
	}
	if it.Kind == "" {
		it.Kind = v.items[at].Kind
	}
	it.Made = v.items[at].Made
	it.Changed = v.now()

	was := slices.Clone(v.items)
	v.items[at].Item = it
	if err := v.save(); err != nil {
		v.items = was
		return Item{}, err
	}
	return it, nil
}

// Remove takes an item out and saves.
func (v *Vault) Remove(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data == nil {
		return ErrLocked
	}
	at := slices.IndexFunc(v.items, func(e entry) bool { return e.ID == id })
	if at < 0 {
		return ErrNoSuchItem
	}
	was := slices.Clone(v.items)
	v.items = slices.Delete(v.items, at, at+1)
	if err := v.save(); err != nil {
		v.items = was
		return err
	}
	return nil
}

// AddKey lets another key open the vault, for a second machine.
//
// The vault has to be open already: what is added is another wrapping
// of the data key, and the data key comes from having opened it.
func (v *Vault) AddKey(signer ssh.Signer, keyFile string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data == nil {
		return ErrLocked
	}
	if err := usable(signer.PublicKey()); err != nil {
		return err
	}
	want := Fingerprint(signer.PublicKey())
	if slices.ContainsFunc(v.file.Slots, func(s slot) bool { return s.Fingerprint == want }) {
		return fmt.Errorf("secrets: %s already opens this vault", want)
	}
	s, err := wrapFor(signer, v.data, keyFile)
	if err != nil {
		return err
	}
	was := slices.Clone(v.file.Slots)
	v.file.Slots = append(v.file.Slots, s)
	if err := v.save(); err != nil {
		v.file.Slots = was
		return err
	}
	return nil
}

// RemoveKey stops a key opening the vault. The last one cannot go, or
// nothing would open it again.
func (v *Vault) RemoveKey(fingerprint string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data == nil {
		return ErrLocked
	}
	at := slices.IndexFunc(v.file.Slots, func(s slot) bool { return s.Fingerprint == fingerprint })
	if at < 0 {
		return fmt.Errorf("secrets: %s does not open this vault", fingerprint)
	}
	if len(v.file.Slots) == 1 {
		return errors.New("secrets: that is the only key that opens this vault")
	}
	was := slices.Clone(v.file.Slots)
	v.file.Slots = slices.Delete(v.file.Slots, at, at+1)
	if err := v.save(); err != nil {
		v.file.Slots = was
		return err
	}
	return nil
}

// Keys is the keys that open the vault, for a dialog that lists them.
func (v *Vault) Keys() []KeySlot {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.file == nil {
		return nil
	}
	out := make([]KeySlot, 0, len(v.file.Slots))
	for _, s := range v.file.Slots {
		out = append(out, KeySlot{Fingerprint: s.Fingerprint, KeyFile: s.KeyFile})
	}
	return out
}

// KeySlot is one key that opens the vault, as a dialog shows it.
type KeySlot struct {
	Fingerprint string
	KeyFile     string
}

// save seals the items and writes the file. The caller holds the lock.
func (v *Vault) save() error {
	raw, err := json.Marshal(contents{Entries: v.items})
	if err != nil {
		return fmt.Errorf("secrets: write the vault %s: %w", v.path, err)
	}
	nonce, box, err := seal(v.data, raw, nil)
	wipe(raw)
	if err != nil {
		return err
	}
	v.file.Version = fileVersion
	v.file.Nonce, v.file.Sealed = nonce, box
	return writeFile(v.path, v.file)
}

// newID is an item's identifier, which only has to be unlike the
// others.
func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("secrets: name a new item: %w", err)
	}
	return hex.EncodeToString(b), nil
}
