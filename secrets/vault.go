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
	// Passphrase is what opens a private key file, kept so the window
	// unlocks that key without asking. File says which key it is.
	Passphrase Kind = "passphrase"
)

// Item is one thing in the vault, without the secret in it.
//
// The list is drawn from these, so the window can show what is in the
// vault without holding any of it. Secret fetches the value for the one
// item a user asked for.
type Item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	User string `json:"user,omitempty"`

	// File is the private key file a Passphrase opens, and empty for
	// every other kind. It is what PassphraseFor matches on, so
	// renaming the item leaves the key still unlocking.
	File string `json:"file,omitempty"`

	// URL is where the secret is used, and Notes is whatever was
	// written beside it. Both are empty for most.
	//
	// Nothing in the window asks for either. They are here so that a
	// secret imported from a manager that had them keeps them, and
	// carries them back out again: a way out that quietly drops a
	// column is a way out that loses the user's work.
	//
	// Notes is not what a Note holds. A note's own text is its value,
	// the way a password's is; this is what somebody wrote beside a
	// login, which every manager has a field for and which used to go
	// nowhere. A real export found that: an entry with a password and
	// three recovery codes in its notes came in as the password alone.
	URL   string `json:"url,omitempty"`
	Notes string `json:"notes,omitempty"`

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
	// Saves counts the times this vault has been written, and goes up
	// by one each time.
	//
	// Inside the sealed half, so it cannot be edited without the key.
	// Every copy of this file that was ever written is validly sealed,
	// so opening one proves somebody had the key and not that this is
	// the newest one. The count is the only thing in the file that says
	// which write it is.
	//
	// refresh compares it against the count this vault last wrote, to
	// notice that another window has saved since and take that window's
	// items in before this one's change goes on top. That comparison
	// needs nothing kept outside the file: both numbers are the vault's
	// own.
	//
	// It is not a guard against an older copy of the file being put
	// back, and is not meant as one. Such a copy would restore a key
	// slot revoked since, and that is all it costs: the secrets in it
	// were already readable by the key it restores, so what the
	// rollback buys is the ones added afterwards. Anybody who can write
	// this file is running as the user, and somebody running as the user
	// can read the passphrase as it is typed, have the SSH agent sign
	// for them, or replace gridterm itself. Guarding this file against
	// them while all of that is open is guarding the smallest door in
	// the house.
	//
	// Catching a rollback would need the highest count ever seen kept
	// somewhere else, and whatever kept it would come back along with
	// this file in the one case worth catching: a home directory
	// restored from a backup, where the vault has quietly gone back
	// three weeks. So that check would miss the accident it was most
	// useful for and catch only a narrow attack.
	Saves uint64 `json:"saves,omitempty"`

	Entries []entry `json:"entries"`
}

// ErrLocked says the vault has not been opened yet.
var ErrLocked = errors.New("secrets: locked")

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

	// saves is the count in the file as it was opened, and what the next
	// write puts one above.
	saves uint64

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
//
// Off the disk when this one has never seen a file there, because
// another window may have created it since. A window that had looked
// at the secrets before they existed answered "no" for the rest of its
// life, and offered to create a vault that then refused the name.
func (v *Vault) Exists() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.file == nil {
		if f, err := readFile(v.path); err == nil {
			v.file = f
		}
	}
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
	// The name is taken before anything else, and by the one call that
	// cannot say yes to two callers. Looking first and writing after
	// left a gap: the write goes through a rename, which replaces
	// whatever is there, so a second window creating a vault in that gap
	// wrote over the first one's -- and the file holds the only copy of
	// what is in it.
	//
	// The file left here is empty. It is replaced by the real one below,
	// and taken away again if that never happens: an empty file reads as
	// a vault that cannot be opened, which is worse than no vault.
	took, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("secrets: %s is already there", path)
		}
		return nil, fmt.Errorf("secrets: create %s: %w", path, err)
	}
	if err := took.Close(); err != nil {
		return nil, fmt.Errorf("secrets: create %s: %w",
			path, errors.Join(err, os.Remove(path)))
	}
	give := func(err error) (*Vault, error) {
		return nil, errors.Join(err, os.Remove(path))
	}

	data, err := newDataKey()
	if err != nil {
		return give(err)
	}
	s, err := wrapFor(signer, data, keyFile)
	if err != nil {
		wipe(data)
		return give(err)
	}
	v := &Vault{path: path, now: time.Now, data: data, file: &file{
		Version: fileVersion,
		Slots:   []slot{s},
	}}
	if err := v.save(); err != nil {
		wipe(data)
		return give(err)
	}
	return v, nil
}

// wrapFor builds a slot that signer opens, holding the data key.
func wrapFor(signer ssh.Signer, data []byte, keyFile string) (slot, error) {
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		return slot{}, fmt.Errorf("secrets: create a challenge: %w", err)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return slot{}, fmt.Errorf("secrets: create a salt: %w", err)
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
		if s.Kind != slotKindSSH {
			// A passphrase slot has no key to offer and nothing to
			// match: what it wants is asked for, not held.
			continue
		}
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
	if v.data != nil {
		return nil
	}
	// Off the disk again rather than out of memory. This vault may have
	// been locked for hours, and another window may have added a key or
	// a secret since: the file is the vault, and what was read when it
	// was opened is a copy of how it looked then.
	if err := v.reopen(); err != nil {
		return err
	}
	// A slot that does not open is remembered and the next key is still
	// tried. The whole point of more than one slot is that losing one
	// does not lose the vault, and giving up on the first bad slot took
	// that away: a damaged slot for the key offered first shut out a key
	// offered second whose own slot was untouched.
	var trouble error
	keep := func(err error) {
		if trouble == nil {
			trouble = err
		}
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
				keep(err)
				continue
			}
			data, err := unseal(key, s.Nonce, s.Wrapped, nil)
			wipe(key)
			if err != nil {
				// The slot names this key and the key did not open it,
				// so the file has been tampered with or the key has
				// been replaced under the same name. Worth reporting if
				// nothing else opens it, and not worth stopping for.
				keep(fmt.Errorf("secrets: the key %s is named by a slot it does not open: %w",
					want, err))
				continue
			}
			if err := v.readContents(data); err != nil {
				wipe(data)
				// The contents are one sealed blob shared by every
				// slot, so another key will not read them either. This
				// one is the answer.
				return err
			}
			v.data = data
			return nil
		}
	}
	if trouble != nil {
		return trouble
	}
	return ErrWrongKey
}

// reopen reads the file again, for a way in that is about to try to
// open it.
//
// Every way in does this. The copy in memory is of how the vault
// looked when it was opened, which may be hours ago and may be before
// another window wrote it, and opening the copy and then saving would
// put that copy over the disk.
//
// A file that has gone answers the way a window that never had one is
// answered, rather than with an open() error after a dialog that
// should never have gone up. A file that is there and will not be read
// is refused outright: handing over secrets out of a file nobody can
// say is still the same one is the one thing this must not do.
//
// The caller holds the lock.
func (v *Vault) reopen() error {
	switch f, err := readFile(v.path); {
	case err == nil:
		v.file = f
		return nil
	case errors.Is(err, os.ErrNotExist):
		v.file = nil
		return fmt.Errorf("secrets: there is nothing at %s yet", v.path)
	default:
		return err
	}
}

// readContents unseals the items with the data key.
func (v *Vault) readContents(data []byte) error {
	c, err := v.contentsOf(v.file, data)
	if err != nil {
		return err
	}
	v.items = c.Entries
	v.saves = c.Saves
	return nil
}

// contentsOf unseals one file's items with the data key, without
// putting them on the vault.
//
// Its own step because refresh has to read a file and decide whether to
// adopt it, and deciding after it has already been adopted is too late.
func (v *Vault) contentsOf(f *file, data []byte) (contents, error) {
	if len(f.Sealed) == 0 {
		return contents{}, nil
	}
	plain, err := unseal(data, f.Nonce, f.Sealed, nil)
	if err != nil {
		return contents{}, fmt.Errorf("secrets: open %s: %w", v.path, err)
	}
	var c contents
	if err := json.Unmarshal(plain, &c); err != nil {
		wipe(plain)
		return contents{}, fmt.Errorf("secrets: read what is in %s: %w", v.path, err)
	}
	wipe(plain)
	return c, nil
}

// refresh takes in whatever another window has written, so this
// window's change goes on top of it rather than over it.
//
// Both windows read the file once when they opened it and each save
// writes the whole of it back, so without this the one that saved
// second wrote the other's secrets away and said nothing at all. The
// file holds the only copy of what is in it.
//
// Only a file that says it has been written more times than this vault
// has. Everything else is left alone, because what is in memory is then
// the better answer: a read that failed, a file this key does not open,
// a count no higher than ours.
//
// This narrows the gap rather than closing it. Two windows that both
// read and then both write inside the same moment still lose one of the
// two, and catching that needs a lock on the file or a rename that
// refuses to replace what it did not read. What it does fix is the case
// anybody actually meets: two windows open, minutes apart.
func (v *Vault) refresh() {
	f, err := readFile(v.path)
	if err != nil {
		return
	}
	c, err := v.contentsOf(f, v.data)
	if err != nil || c.Saves <= v.saves {
		return
	}
	v.file = f
	v.items = c.Entries
	v.saves = c.Saves
}

// Saves is how many times this vault has been written, as the file this
// one last read says.
//
// refresh is what the count is for: see the note on contents.Saves.
// This reports it so a test can watch a save land.
func (v *Vault) Saves() uint64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.saves
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
	// The list is what the user picks from, so it is worth a look at
	// the file first: a secret another window added is one this one
	// would otherwise not offer until it wrote something itself.
	v.refresh()
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

// PassphraseFor is the saved passphrase of a private key file.
//
// Matched on the file rather than on the item's name, so a passphrase
// the user has renamed goes on opening the key it was saved for.
// ErrNoSuchItem means the vault holds none for that file, which is the
// ordinary answer for every key the user never saved one for.
func (v *Vault) PassphraseFor(keyFile string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data == nil {
		return "", ErrLocked
	}
	if keyFile == "" {
		return "", ErrNoSuchItem
	}
	// A look at the file first, because the key this asks about may have
	// been made in another window a moment ago: without it that window's
	// passphrase is not here, and this one asks the user for one they
	// were never shown.
	v.refresh()
	for _, e := range v.items {
		if e.Kind == Passphrase && e.File == keyFile {
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
	// On top of what is on the disk now, not over it.
	v.refresh()
	if strings.TrimSpace(it.Name) == "" {
		return Item{}, errors.New("secrets: an item needs a name")
	}
	if it.Kind == "" {
		it.Kind = Password
	}
	if err := v.onlyPassphraseFor(it); err != nil {
		return Item{}, err
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
	// On top of what is on the disk now, not over it.
	v.refresh()
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
	if err := v.onlyPassphraseFor(it); err != nil {
		return Item{}, err
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

// onlyPassphraseFor refuses a second passphrase for a key file the
// vault already has one for. The caller holds the lock.
//
// PassphraseFor answers with the first it finds, so a second one would
// sit in the vault unused and the user would have no way to tell which
// of the two the key is actually locked with.
func (v *Vault) onlyPassphraseFor(it Item) error {
	if it.Kind != Passphrase || it.File == "" {
		return nil
	}
	for _, e := range v.items {
		if e.Kind == Passphrase && e.File == it.File && e.ID != it.ID {
			return fmt.Errorf("secrets: %s is already the passphrase for %s",
				e.Name, it.File)
		}
	}
	return nil
}

// Remove takes an item out and saves.
func (v *Vault) Remove(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.data == nil {
		return ErrLocked
	}
	// On top of what is on the disk now, not over it.
	v.refresh()
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
	// On top of what is on the disk now, not over it.
	v.refresh()
	if err := usable(signer.PublicKey()); err != nil {
		return err
	}
	want := Fingerprint(signer.PublicKey())
	if slices.ContainsFunc(v.file.Slots, func(s slot) bool { return s.Fingerprint == want }) {
		return fmt.Errorf("secrets: %s already opens the secrets", want)
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
	// On top of what is on the disk now, not over it.
	v.refresh()
	at := slices.IndexFunc(v.file.Slots, func(s slot) bool { return s.Fingerprint == fingerprint })
	if at < 0 {
		return fmt.Errorf("secrets: %s does not open the secrets", fingerprint)
	}
	if len(v.file.Slots) == 1 {
		return errors.New("secrets: that is the only key that opens the secrets")
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
		out = append(out, KeySlot{
			Kind: s.Kind, Fingerprint: s.Fingerprint, KeyFile: s.KeyFile,
		})
	}
	return out
}

// KeySlot is one way into the vault, as a dialog shows it.
type KeySlot struct {
	// Kind is what opens it: an SSH key, or a passphrase.
	Kind        string
	Fingerprint string
	KeyFile     string
}

// ByPassphrase reports whether this slot is opened by one.
func (s KeySlot) ByPassphrase() bool { return s.Kind == slotKindPassphrase }

// save seals the items and writes the file. The caller holds the lock.
func (v *Vault) save() error {
	// Up by one before it is sealed, so the file on disk always says a
	// higher number than the one it replaced.
	v.saves++
	raw, err := json.Marshal(contents{Saves: v.saves, Entries: v.items})
	if err != nil {
		return fmt.Errorf("secrets: write %s: %w", v.path, err)
	}
	nonce, box, err := seal(v.data, raw, nil)
	wipe(raw)
	if err != nil {
		v.saves--
		return err
	}
	// Into a copy, so a write that fails leaves the vault holding the
	// file it last wrote rather than the one it tried to.
	//
	// The callers take v.items back themselves, and that was not enough:
	// what Lock and Unlock read is this sealed blob, not v.items. A
	// removal that failed at the rename left the blob without the item,
	// so the next unlock in the same window read it back missing, and
	// the save after that wrote it out that way. The user was told the
	// removal failed and lost the item anyway.
	wrote := *v.file
	wrote.Version = fileVersion
	wrote.Nonce, wrote.Sealed = nonce, box
	if err := writeFile(v.path, &wrote); err != nil {
		// Nothing reached the disk, so the count this vault would write
		// next has not been spent.
		v.saves--
		return err
	}
	*v.file = wrote
	return nil
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
