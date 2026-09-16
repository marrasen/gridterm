// Package settings keeps the choices gridterm remembers between runs.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/marrasen/gridterm/internal/jsoncheck"
)

// fileVersion is written into the file so a later shape can be told from
// this one.
const fileVersion = 1

// settingsDir and settingsFile are what the settings are kept in, under
// the directory the operating system gives a program for its settings.
const settingsDir, settingsFile = "gridterm", "settings.json"

// How far a serving window may be reached from, as it is written down.
const (
	ReachHere     = "here"
	ReachAnywhere = "anywhere"
)

// ErrUnsaveable is returned when the settings cannot be written because
// they could not be read.
var ErrUnsaveable = errors.New("the settings could not be read, so they will not be written over")

// rename puts a finished temporary file in place of the real one. A
// variable so a test can fail it and see that the old file survives.
var rename = os.Rename

// stored is the shape of the file.
type stored struct {
	Version int `json:"version"`

	// ServePort is a pointer because a field left out is a choice nobody
	// has made, which is not the same as port 0, meaning whichever port
	// is free.
	ServePort *int `json:"servePort,omitempty"`

	// ServeReach is one of the words above rather than the dialog's
	// wording, so the dialog can be reworded without orphaning it.
	ServeReach *string `json:"serveReach,omitempty"`

	// AgentHost is the agent program the hand-over dialog last wrote a
	// prompt for, by the name that dialog offers.
	AgentHost *string `json:"agentHost,omitempty"`

	// Shell is the shell a new pane runs, by the id the shell list gives
	// it.
	Shell *string `json:"shell,omitempty"`
}

// Settings are the choices gridterm remembers between runs.
//
// Settings that could not be read refuse to save. A file nobody could
// parse is somebody's settings, and replacing it with an empty one loses
// them for good; the failure is reported and the user gets to repair the
// file.
//
// Every change re-reads the file first, so a second window's work is
// never written over by a copy built from a stale read.
//
// Settings are safe to use from several goroutines.
type Settings struct {
	path string

	mu   sync.Mutex
	have stored

	// loadErr is why the file could not be read, and is what stops it
	// being written over.
	loadErr error
}

// Path returns where the settings live.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("settings: no configuration directory: %w", err)
	}
	return filepath.Join(dir, settingsDir, settingsFile), nil
}

// Load reads the settings.
//
// It always returns usable Settings, and an error when the file could not
// be read. Those settings remember nothing and will not save, so a window
// can still open while the user is told what is wrong with their file.
//
// A file that is not there is not an error: it is what the first run
// looks like.
func Load(path string) (*Settings, error) {
	s := &Settings{path: path}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s, s.rereadLocked()
}

// Unusable returns settings that remember nothing and will not save,
// because of err.
//
// It exists for a caller that could not work out where the file lives.
func Unusable(err error) *Settings {
	if err == nil {
		err = errors.New("the settings are unavailable")
	}
	return &Settings{loadErr: err}
}

// Err returns why the settings cannot be saved, or nil.
func (s *Settings) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadErr
}

// Path returns the file the settings are kept in.
func (s *Settings) Path() string { return s.path }

// ServePort is the port the serve dialog was last set to, and whether one
// was saved.
func (s *Settings) ServePort() (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.have.ServePort == nil {
		return 0, false
	}
	return *s.have.ServePort, true
}

// ServeReach is how far the serve dialog was last set to be reachable
// from, and whether one was saved.
func (s *Settings) ServeReach() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.have.ServeReach == nil {
		return "", false
	}
	return *s.have.ServeReach, true
}

// PutServe remembers what the serve dialog was set to, and saves.
//
// The port is remembered as it was typed, so 0 stays 0 and goes on
// meaning whichever port is free.
func (s *Settings) PutServe(port int, reach string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The file first: another window may have changed it since this one
	// read it, and writing a copy built from a stale read would throw
	// that work away.
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.ServePort = &port
	s.have.ServeReach = &reach
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// AgentHost is the agent the hand-over dialog was last set to, and
// whether one was saved.
func (s *Settings) AgentHost() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.have.AgentHost == nil {
		return "", false
	}
	return *s.have.AgentHost, true
}

// PutAgentHost remembers which agent the hand-over dialog was set to,
// and saves.
func (s *Settings) PutAgentHost(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The file first, for the same reason PutServe reads it first.
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.AgentHost = &name
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// Shell is the shell a new pane was last opened on, and whether one was
// saved.
func (s *Settings) Shell() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.have.Shell == nil {
		return "", false
	}
	return *s.have.Shell, true
}

// PutShell remembers which shell a new pane runs, and saves.
func (s *Settings) PutShell(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The file first, for the same reason PutServe reads it first.
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.Shell = &id
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// rereadLocked reads the file into the settings, replacing what they
// hold.
//
// A file that is not there leaves nothing remembered and no error: that
// is what the first run looks like, and also what it looks like after the
// user deletes it.
func (s *Settings) rereadLocked() error {
	if s.path == "" {
		// Nothing was ever read, so there is nothing to reread. Settings
		// built by Unusable keep the reason they are unusable.
		if s.loadErr == nil {
			s.loadErr = errors.New("settings: nowhere to keep the settings")
		}
		return s.loadErr
	}

	have, err := read(s.path)
	if err != nil {
		s.loadErr = err
		s.have = stored{}
		return err
	}
	s.loadErr = nil
	s.have = have
	return nil
}

// read parses the file, or says why it could not.
func read(path string) (stored, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		// No version either: the version written is the one this build
		// writes, stamped on the way out.
		return stored{}, nil
	}
	if err != nil {
		return stored{}, fmt.Errorf("settings: read %s: %w", path, err)
	}

	// A key written twice is not a file to guess at: Go's decoder keeps
	// the last one, so the next save would make that permanent.
	if err := jsoncheck.NoRepeatedKeys(raw); err != nil {
		return stored{}, fmt.Errorf("settings: %s: %w", path, err)
	}

	// The version before the rest, or settings written by a newer
	// gridterm that also added a field are turned away for the field
	// instead, in the decoder's words rather than in words the user can
	// act on.
	var version struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &version); err != nil {
		return stored{}, fmt.Errorf("settings: %s is not readable: %w", path, err)
	}
	switch {
	case version.Version > fileVersion:
		return stored{}, fmt.Errorf(
			"settings: %s was written by a newer gridterm (version %d)", path, version.Version)
	case version.Version < 1:
		// Covers a file of "null" or "{}" as well as one written with no
		// version at all.
		return stored{}, fmt.Errorf("settings: %s has no version number", path)
	}

	var file stored
	dec := json.NewDecoder(bytes.NewReader(raw))
	// Nothing is dropped on the way through. A field this build does not
	// know about belongs to somebody, and writing the file back without
	// it would delete it.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return stored{}, fmt.Errorf("settings: %s is not readable: %w", path, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return stored{}, fmt.Errorf("settings: there is more in %s than one set of settings", path)
	}
	if err := check(file); err != nil {
		return stored{}, fmt.Errorf("settings: %s: %w", path, err)
	}
	return file, nil
}

// check reports a value that cannot be read back, or nil.
func check(file stored) error {
	if p := file.ServePort; p != nil && (*p < 0 || *p > 65535) {
		return fmt.Errorf("%d is not a port number", *p)
	}
	if r := file.ServeReach; r != nil && *r != ReachHere && *r != ReachAnywhere {
		return fmt.Errorf("%q is not somewhere a window can be reached from", *r)
	}
	// Which agents there are is the window's business, not this package's, so only an empty name is
	// turned away: it names nothing and could not be offered back to the dialog.
	if h := file.AgentHost; h != nil && *h == "" {
		return errors.New("the agent host has no name")
	}
	// Which shells there are is the window's business, not this package's, so only an empty id is
	// turned away: it names nothing and could not be opened.
	if sh := file.Shell; sh != nil && *sh == "" {
		return errors.New("the shell has no id")
	}
	return nil
}

// saveLocked writes the settings out.
//
// Through a temporary file in the same directory and a rename, so a crash
// or a full disk leaves the old settings where they were rather than half
// of the new ones.
func (s *Settings) saveLocked() error {
	if s.loadErr != nil {
		// The one place the rule lives, so a mutator added later cannot
		// forget it.
		return fmt.Errorf("%w: %w", ErrUnsaveable, s.loadErr)
	}
	if s.path == "" {
		return errors.New("settings: nowhere to save the settings")
	}
	// Nothing is written that cannot be read back, or a bad write locks
	// the user out of their own settings: every later change rereads the
	// file first and fails on the same thing.
	if err := check(s.have); err != nil {
		return fmt.Errorf("settings: will not write the settings %s: %w", s.path, err)
	}
	s.have.Version = fileVersion
	raw, err := json.MarshalIndent(s.have, "", "  ")
	if err != nil {
		return fmt.Errorf("settings: write the settings %s: %w", s.path, err)
	}
	raw = append(raw, '\n')

	// The file the path really names. A rename replaces a link rather
	// than what it points at, so a settings file linked in from
	// somewhere else would be quietly detached and stop being updated.
	// Only "it is not there" means there is no link to follow, which is
	// what the first save meets.
	path, err := filepath.EvalSymlinks(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		path = s.path
	case err != nil:
		return fmt.Errorf("settings: write the settings %s: %w", s.path, err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("settings: write the settings %s: %w", s.path, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("settings: write the settings %s: %w", s.path, err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("settings: write the settings %s: %w", s.path, err)
	}
	// Flushed before the rename, or a crash can leave the new name
	// pointing at an empty file.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("settings: write the settings %s: %w", s.path, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("settings: write the settings %s: %w", s.path, err)
	}
	if err := rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("settings: write the settings %s: %w", s.path, err)
	}
	return nil
}
