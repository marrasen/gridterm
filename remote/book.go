package remote

import (
	"encoding/json"
	"errors"
	"fmt"
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
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return b, nil
	}
	if err != nil {
		b.loadErr = fmt.Errorf("remote: read the server list %s: %w", path, err)
		return b, b.loadErr
	}

	var file saved
	if err := json.Unmarshal(raw, &file); err != nil {
		b.loadErr = fmt.Errorf("remote: the server list %s is not readable: %w", path, err)
		return b, b.loadErr
	}
	if file.Version > bookVersion {
		b.loadErr = fmt.Errorf(
			"remote: the server list %s was written by a newer gridterm (version %d)",
			path, file.Version)
		return b, b.loadErr
	}
	for i, h := range file.Servers {
		if err := h.Validate(); err != nil {
			b.loadErr = fmt.Errorf("remote: the server list %s: server %d: %w", path, i+1, err)
			return b, b.loadErr
		}
	}
	if name, dup := firstDuplicate(file.Servers); dup {
		b.loadErr = fmt.Errorf("remote: the server list %s names %q twice", path, name)
		return b, b.loadErr
	}

	b.hosts = file.Servers
	sortHosts(b.hosts)
	return b, nil
}

// Err returns why the book cannot be saved, or nil.
func (b *Book) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.loadErr
}

// Path returns the file the book is kept in.
func (b *Book) Path() string { return b.path }

// Hosts returns the saved machines, by name. The slice is a copy.
func (b *Book) Hosts() []Host {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.hosts)
}

// Lookup returns the machine with a name, ignoring case so a name typed
// into the palette still finds it.
func (b *Book) Lookup(name string) (Host, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lookupLocked(name)
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
	if err := h.Validate(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.loadErr != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, b.loadErr)
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
	// the same one, and Via names a server by its name.
	for i, have := range b.hosts {
		if i != at && strings.EqualFold(have.Name, h.Name) {
			return fmt.Errorf("there is already a server called %q", have.Name)
		}
	}
	if h.Via != "" {
		if _, ok := b.lookupLocked(h.Via); !ok {
			return fmt.Errorf("there is no saved server called %q to reach it through", h.Via)
		}
	}

	before := slices.Clone(b.hosts)
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
	if err := b.checkRoutes(); err != nil {
		b.hosts = before
		return err
	}
	sortHosts(b.hosts)
	if err := b.saveLocked(); err != nil {
		b.hosts = before
		return err
	}
	return nil
}

// Remove takes a machine out of the list and saves.
func (b *Book) Remove(name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.loadErr != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, b.loadErr)
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

	before := slices.Clone(b.hosts)
	b.hosts = slices.Delete(b.hosts, at, at+1)
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
		route = append(route, h)
		at = h.Via
	}
	slices.Reverse(route)
	return route, nil
}

// checkRoutes reports a Via that goes round in a circle, which would
// otherwise be found only when somebody tried to connect.
func (b *Book) checkRoutes() error {
	for _, start := range b.hosts {
		seen := map[string]bool{}
		for at := start.Name; at != ""; {
			key := strings.ToLower(at)
			if seen[key] {
				return fmt.Errorf("the route to %q goes round in a circle", start.Name)
			}
			seen[key] = true
			h, ok := b.lookupLocked(at)
			if !ok {
				break
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
	if b.path == "" {
		return errors.New("remote: nowhere to save the server list")
	}
	raw, err := json.MarshalIndent(saved{Version: bookVersion, Servers: b.hosts}, "", "  ")
	if err != nil {
		return fmt.Errorf("remote: write the server list: %w", err)
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(b.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("remote: write the server list: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(b.path)+".*")
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
	if err := os.Chmod(name, 0o600); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("remote: write the server list: %w", err)
	}
	if err := os.Rename(name, b.path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("remote: write the server list: %w", err)
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
