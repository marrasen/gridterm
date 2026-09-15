package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// bookVersion is written into the file so a later shape can be told from
// this one.
const bookVersion = 1

// bookFile is what the saved servers are kept in, under the directory
// the operating system gives a program for its settings.
const bookDir, bookFile = "gridterm", "servers.json"

// ErrUnsaveable is returned when the book cannot be written because it
// could not be read.
var ErrUnsaveable = errors.New("the server list could not be read, so it will not be written over")

// saved is the shape of the file.
type saved struct {
	Version int    `json:"version"`
	Servers []Host `json:"servers"`
}

// Book is the saved list of machines.
//
// A Book that could not be read refuses to save. A file nobody could
// parse is somebody's list of servers, and replacing it with an empty
// one loses it for good; the failure is reported and the user gets to
// repair the file.
//
// Every change re-reads the file first, so a second window's work is
// never written over by a list built from a stale read.
//
// A Book is safe to use from several goroutines.
type Book struct {
	path string

	mu    sync.Mutex
	hosts []Host

	// loadErr is why the file could not be read, and is what stops it
	// being written over.
	loadErr error
}

// BookPath returns where the saved servers live.
func BookPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("remote: no configuration directory: %w", err)
	}
	return filepath.Join(dir, bookDir, bookFile), nil
}

// LoadBook reads the saved servers.
//
// It always returns a usable Book, and an error when the file could not
// be read. That book holds no servers and will not save, so a window can
// still open and connect to things typed by hand while the user is told
// what is wrong with their file.
//
// A file that is not there is not an error: it is what the first run
// looks like.
func LoadBook(path string) (*Book, error) {
	b := &Book{path: path}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b, b.rereadLocked()
}

// UnusableBook returns a book that holds nothing and will not save,
// because of err.
//
// It exists for a caller that could not work out where the file lives.
// Handing back a plain empty Book instead would give a window that
// silently offers to save servers it has nowhere to put.
func UnusableBook(err error) *Book {
	if err == nil {
		err = errors.New("the server list is unavailable")
	}
	return &Book{loadErr: err}
}

// Err returns why the book cannot be saved, or nil.
func (b *Book) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.loadErr
}

// Path returns the file the book is kept in.
func (b *Book) Path() string { return b.path }

// Reload reads the file again, for a user who has repaired it.
func (b *Book) Reload() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rereadLocked()
}

// Hosts returns the saved machines, by name. The slice is a copy, deep
// enough that a caller cannot change what is saved without saving it.
func (b *Book) Hosts() []Host {
	b.mu.Lock()
	defer b.mu.Unlock()
	return cloneHosts(b.hosts)
}

// Lookup returns the machine with a name, ignoring case so a name typed
// into the palette still finds it.
func (b *Book) Lookup(name string) (Host, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	h, ok := b.lookupLocked(name)
	return h.clone(), ok
}

func (b *Book) lookupLocked(name string) (Host, bool) {
	for _, h := range b.hosts {
		if strings.EqualFold(h.Name, name) {
			return h, true
		}
	}
	return Host{}, false
}

// Put adds a machine or replaces the one with the same name, and saves.
//
// under is the name it is replacing, for a rename. An empty one means it
// is being added, and a name already taken is refused.
func (b *Book) Put(h Host, under string) error {
	h = h.tidy()
	if err := h.Validate(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	// The file first: another window may have changed it since this one
	// read it, and writing a list built from a stale read would throw
	// that work away.
	if err := b.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}

	at := -1
	if under != "" {
		for i, have := range b.hosts {
			if strings.EqualFold(have.Name, under) {
				at = i
				break
			}
		}
		if at < 0 {
			return fmt.Errorf("there is no saved server called %q", under)
		}
	}
	// A name already used by a different entry would give two servers
	// the same one, and Via names a server by its name. Two names that
	// reduce to the same command id are refused for the same reason: one
	// of the two would be unreachable from the menu and the palette.
	for i, have := range b.hosts {
		if i == at {
			continue
		}
		if strings.EqualFold(have.Name, h.Name) {
			return fmt.Errorf("there is already a server called %q", have.Name)
		}
		if CommandName(have.Name) == CommandName(h.Name) {
			return fmt.Errorf("%q is too much like %q to tell apart", h.Name, have.Name)
		}
	}

	before := cloneHosts(b.hosts)
	if at >= 0 {
		// A rename leaves anything that went through the old name
		// pointing at nothing, so those follow it.
		if old := b.hosts[at].Name; !strings.EqualFold(old, h.Name) {
			for i := range b.hosts {
				if strings.EqualFold(b.hosts[i].Via, old) {
					b.hosts[i].Via = h.Name
				}
			}
		}
		b.hosts[at] = h
	} else {
		b.hosts = append(b.hosts, h)
	}
	// Checked after the change, not before: a rename can take away the
	// very name a route was pointing at, and a check run first would
	// have approved it.
	if err := checkRoutes(b.hosts); err != nil {
		b.hosts = before
		return err
	}
	sortHosts(b.hosts)
	return b.applyLocked(before)
}

// Remove takes a machine out of the list and saves.
func (b *Book) Remove(name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}

	at := -1
	for i, h := range b.hosts {
		if strings.EqualFold(h.Name, name) {
			at = i
			break
		}
	}
	if at < 0 {
		return fmt.Errorf("there is no saved server called %q", name)
	}
	for _, h := range b.hosts {
		if strings.EqualFold(h.Via, name) {
			return fmt.Errorf("%q is reached through %q, so %q has to stay",
				h.Name, b.hosts[at].Name, b.hosts[at].Name)
		}
	}

	before := cloneHosts(b.hosts)
	b.hosts = slices.Delete(b.hosts, at, at+1)
	return b.applyLocked(before)
}

// applyLocked writes the changed list out, and puts back what was there
// if it will not save. One of these rather than one per change: a window
// showing servers that are saved nowhere is worse than a change that
// did not happen.
func (b *Book) applyLocked(before []Host) error {
	if err := b.saveLocked(); err != nil {
		b.hosts = before
		return err
	}
	return nil
}

// Route returns the machines to connect to in order to reach one: the
// far end last, and whatever it is reached through before it.
func (b *Book) Route(name string) ([]Host, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var route []Host
	seen := map[string]bool{}
	for at := name; at != ""; {
		key := strings.ToLower(at)
		if seen[key] {
			return nil, fmt.Errorf("the route to %q goes round in a circle", name)
		}
		seen[key] = true

		h, ok := b.lookupLocked(at)
		if !ok {
			return nil, fmt.Errorf("there is no saved server called %q", at)
		}
		route = append(route, h.clone())
		at = h.Via
	}
	slices.Reverse(route)
	return route, nil
}

// rereadLocked reads the file into the book, replacing what it holds.
//
// A file that is not there leaves an empty list and no error: that is
// what the first run looks like, and also what it looks like after the
// user deletes it.
func (b *Book) rereadLocked() error {
	if b.path == "" {
		// Nothing was ever read, so there is nothing to reread. A book
		// built by UnusableBook keeps the reason it is unusable.
		if b.loadErr == nil {
			b.loadErr = errors.New("remote: nowhere to keep the server list")
		}
		return b.loadErr
	}

	hosts, err := readBook(b.path)
	if err != nil {
		b.loadErr = err
		b.hosts = nil
		return err
	}
	b.loadErr = nil
	b.hosts = hosts
	return nil
}

// readBook parses the file, or says why it could not.
func readBook(path string) ([]Host, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("remote: read the server list %s: %w", path, err)
	}

	// A key written twice is not a list to guess at: Go's decoder keeps
	// the last one, so a file holding "servers" twice would quietly
	// become whichever came second -- and the next save would make that
	// permanent.
	if err := checkNoRepeatedKeys(raw); err != nil {
		return nil, fmt.Errorf("remote: the server list %s: %w", path, err)
	}

	var file saved
	dec := json.NewDecoder(bytes.NewReader(raw))
	// Nothing is dropped on the way through. A field this build does not
	// know about belongs to somebody, and writing the file back without
	// it would delete it.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("remote: the server list %s is not readable: %w", path, err)
	}
	if err := endOfFile(dec); err != nil {
		return nil, fmt.Errorf("remote: the server list %s: %w", path, err)
	}

	switch {
	case file.Version > bookVersion:
		return nil, fmt.Errorf(
			"remote: the server list %s was written by a newer gridterm (version %d)",
			path, file.Version)
	case file.Version < 1:
		// Covers a file of "null" or "{}" as well as one written with no
		// version at all: none of them is a list this can safely replace.
		return nil, fmt.Errorf("remote: the server list %s has no version number", path)
	}

	for i, h := range file.Servers {
		if err := h.Validate(); err != nil {
			return nil, fmt.Errorf("remote: the server list %s: server %d: %w", path, i+1, err)
		}
	}
	if name, dup := firstDuplicate(file.Servers); dup {
		return nil, fmt.Errorf("remote: the server list %s names %q twice", path, name)
	}
	// Checked on the way in as well as on the way out, or a file with a
	// broken route would load clean and then refuse every later change
	// for a reason the user never touched.
	if err := checkRoutes(file.Servers); err != nil {
		return nil, fmt.Errorf("remote: the server list %s: %w", path, err)
	}

	sortHosts(file.Servers)
	return file.Servers, nil
}

// endOfFile reports anything after the value that was decoded.
func endOfFile(dec *json.Decoder) error {
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("there is more in the file than one server list")
	}
	return nil
}

// checkNoRepeatedKeys reports an object holding the same key twice,
// which the decoder would otherwise resolve by keeping the last.
func checkNoRepeatedKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	// What is open, innermost last. Inside an object the tokens are a
	// key, then its value, then a key again, and that alternation is
	// the only thing that tells the two apart: both can be strings, and
	// a server whose name is its address has the same string as a key
	// and as a value.
	type scope struct {
		object  bool
		wantKey bool
		keys    map[string]bool
	}
	var scopes []scope
	// filled says the token just read was a value, so the object around
	// it is back to expecting a key.
	filled := func() {
		if n := len(scopes); n > 0 && scopes[n-1].object {
			scopes[n-1].wantKey = true
		}
	}
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			// Not readable as JSON at all, which the decode below says
			// far better than this can.
			return nil
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{':
				filled()
				scopes = append(scopes, scope{object: true, wantKey: true, keys: map[string]bool{}})
			case '[':
				filled()
				scopes = append(scopes, scope{})
			case '}', ']':
				if len(scopes) > 0 {
					scopes = scopes[:len(scopes)-1]
				}
			}
			continue
		}
		n := len(scopes)
		if n == 0 || !scopes[n-1].object || !scopes[n-1].wantKey {
			filled()
			continue
		}
		key, _ := tok.(string)
		if scopes[n-1].keys[key] {
			return fmt.Errorf("%q is in it twice", key)
		}
		scopes[n-1].keys[key] = true
		scopes[n-1].wantKey = false
	}
}

// checkRoutes reports a Via that names nothing, or that goes round in a
// circle. Either would be found only when somebody tried to connect.
func checkRoutes(hosts []Host) error {
	find := func(name string) (Host, bool) {
		for _, h := range hosts {
			if strings.EqualFold(h.Name, name) {
				return h, true
			}
		}
		return Host{}, false
	}
	for _, start := range hosts {
		seen := map[string]bool{}
		for at := start.Name; at != ""; {
			key := strings.ToLower(at)
			if seen[key] {
				return fmt.Errorf("the route to %q goes round in a circle", start.Name)
			}
			seen[key] = true
			h, ok := find(at)
			if !ok {
				return fmt.Errorf("there is no saved server called %q to reach %q through",
					at, start.Name)
			}
			at = h.Via
		}
	}
	return nil
}

// saveLocked writes the list out.
//
// Through a temporary file in the same directory and a rename, so a
// crash or a full disk leaves the old list where it was rather than half
// of the new one.
func (b *Book) saveLocked() error {
	if b.loadErr != nil {
		// The one place the rule lives, so a mutator added later cannot
		// forget it.
		return fmt.Errorf("%w: %w", ErrUnsaveable, b.loadErr)
	}
	if b.path == "" {
		return errors.New("remote: nowhere to save the server list")
	}
	// Nothing is written that cannot be read back. A list this refuses
	// to load is a list the user cannot repair from inside gridterm:
	// every later change rereads the file first and fails on the same
	// thing, so one bad write locks them out of their own servers for
	// good.
	if err := readableBack(b.hosts); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(saved{Version: bookVersion, Servers: b.hosts}, "", "  ")
	if err != nil {
		return fmt.Errorf("remote: write the server list: %w", err)
	}
	raw = append(raw, '\n')

	// The file the path really names. A rename replaces a link rather
	// than what it points at, so a config file linked in from somewhere
	// else would be quietly detached and stop being updated.
	path := b.path
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("remote: write the server list: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("remote: write the server list: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("remote: write the server list: %w", err)
	}
	// Flushed before the rename, or a crash can leave the new name
	// pointing at an empty file.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("remote: write the server list: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("remote: write the server list: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("remote: write the server list: %w", err)
	}
	return nil
}

// readableBack reports why a list could not be loaded again, or nil.
//
// The same checks readBook makes, run before the file is written. They
// are cheap, and the alternative is a file gridterm wrote and gridterm
// will not read.
func readableBack(hosts []Host) error {
	for i, h := range hosts {
		if err := h.Validate(); err != nil {
			return fmt.Errorf("remote: will not write the server list: server %d: %w", i+1, err)
		}
	}
	if name, dup := firstDuplicate(hosts); dup {
		return fmt.Errorf("remote: will not write the server list: it names %q twice", name)
	}
	if err := checkRoutes(hosts); err != nil {
		return fmt.Errorf("remote: will not write the server list: %w", err)
	}
	return nil
}

// firstDuplicate returns a name used twice, ignoring case.
func firstDuplicate(hosts []Host) (string, bool) {
	seen := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		key := strings.ToLower(h.Name)
		if seen[key] {
			return h.Name, true
		}
		seen[key] = true
	}
	return "", false
}

// sortHosts puts the list in the order a user reads it, so the file and
// the menu do not shuffle between runs.
func sortHosts(hosts []Host) {
	slices.SortFunc(hosts, func(a, b Host) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}

// cloneHosts copies a list deeply enough that nothing handed out shares
// a slice with what is saved.
func cloneHosts(hosts []Host) []Host {
	out := make([]Host, len(hosts))
	for i, h := range hosts {
		out[i] = h.clone()
	}
	return out
}

// Names is what the saved machines are called, in the order the book
// holds them.
//
// A caller that only wants the names asks for these rather than for
// Hosts: a Host carries its key files, and cloning those to read a name
// off is work for nothing.
func (b *Book) Names() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, 0, len(b.hosts))
	for _, h := range b.hosts {
		out = append(out, h.Name)
	}
	return out
}

// Kind is what a Book holds under a name, without the key files a Host
// carries.
type Kind struct {
	// Name is the book's own spelling of the name, which can differ in
	// case from the name asked about.
	Name string

	// Window says the machine is another gridterm, and Serve is where
	// that one serves.
	Window bool
	Serve  string
}

// Kind says what the book holds under a name, ignoring case the way
// Lookup does.
//
// A caller that only wants these asks for them rather than for the Host:
// this is asked for every row of every frame, and cloning a machine and
// its key files to read one flag off it is work for nothing.
func (b *Book) Kind(name string) (Kind, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	h, ok := b.lookupLocked(name)
	if !ok {
		return Kind{}, false
	}
	k := Kind{Name: h.Name, Window: h.Window}
	if h.Window {
		k.Serve = h.ServeAddr()
	}
	return k, true
}
