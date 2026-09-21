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
	"slices"
	"strings"
	"sync"

	"github.com/marrasen/gridterm/conf"
	"github.com/marrasen/gridterm/internal/jsoncheck"
)

// fileVersion is written into the file so a later shape can be told from
// this one.
const fileVersion = 1

// File is what the settings are kept in, in the directory conf gives
// gridterm.
const File = "settings.json"

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

	// ServeOn says a window was serving when it was last closed, so the
	// next one can offer to serve again. A field left out is a gridterm
	// that has never served. There is one file per user rather than one
	// per window, so this is the last window to say either way.
	ServeOn *bool `json:"serveOn,omitempty"`

	// ServeReach is one of the words above rather than the dialog's
	// wording, so the dialog can be reworded without orphaning it.
	ServeReach *string `json:"serveReach,omitempty"`

	// AgentHost is the agent program the hand-over dialog last wrote a
	// prompt for, by the name that dialog offers.
	AgentHost *string `json:"agentHost,omitempty"`

	// Shell is the shell a new pane runs, by the id the shell list gives
	// it.
	Shell *string `json:"shell,omitempty"`

	// AgentMay is what the hand-over dialog's tick boxes were last set
	// to. A field left out is a box that was not ticked.
	AgentMay *AgentMay `json:"agentMay,omitempty"`

	// Commands are the commands the user asked to keep, newest first.
	Commands []SavedCommand `json:"commands,omitempty"`

	// PaneTitles turns on the line above each pane naming it.
	PaneTitles *bool `json:"paneTitles,omitempty"`

	// ShellSetup turns on teaching a shell on this machine to say where
	// it is and where each command starts. A field left out is on: it
	// costs a cleared pane and it is what makes a path in the output
	// clickable.
	ShellSetup *bool `json:"shellSetup,omitempty"`

	// TermProgram is what the window calls itself in TERM_PROGRAM. A
	// field left out is gridterm's own name, which is the true one.
	TermProgram *string `json:"termProgram,omitempty"`

	// Tunnels are the tunnels the user asked to keep, newest first.
	Tunnels []SavedTunnel `json:"tunnels,omitempty"`

	// Copies are the file copies the user asked to keep, newest first.
	Copies []SavedCopy `json:"copies,omitempty"`

	// Keys are the private key files the user keeps, newest first, to
	// pick from when making a connection.
	Keys []string `json:"keys,omitempty"`

	// SecretsSaves is the highest number of writes a vault file has said
	// it has had, by where that file is.
	//
	// Kept here rather than in the vault, which is the whole point: a
	// vault file is validly sealed however old it is, so the file cannot
	// say whether it is the newest one. A count kept somewhere else can,
	// and putting an old vault back then has to put an old settings file
	// back with it to go unseen.
	SecretsSaves map[string]uint64 `json:"secretsSaves,omitempty"`

	// Theme is the colour theme the window is drawn in, by name.
	Theme *string `json:"theme,omitempty"`

	// FontSize is the size the text is drawn at, in points.
	FontSize *float64 `json:"fontSize,omitempty"`
}

// SavedCommand is a command line the user asked to keep, the directory
// it runs in, and the machine it was saved on.
//
// The line is offered on every machine, because the same command often
// runs on several. The directory is not: a path belongs to the machine
// it was typed on.
type SavedCommand struct {
	Line string `json:"line"`
	Dir  string `json:"dir,omitempty"`
	Host string `json:"host,omitempty"`
}

// SavedTunnel is a tunnel the user asked to keep, so the same one can
// be opened again without typing the ports out.
//
// The machine is named rather than held: one that dropped and came
// back is a different connection under the same name, and the tunnel
// is opened over whatever is connected when it is asked for.
type SavedTunnel struct {
	// Host is the machine it runs over, as the sidebar names it.
	Host string `json:"host"`

	// Kind is which way it goes, written as remote.TunnelKind spells
	// it: "local", "remote" or "socks".
	Kind string `json:"kind"`

	// Listen is the address it listens on, and Target what it reaches.
	// A dynamic one has no target: whoever connects says where it is
	// going.
	Listen string `json:"listen"`
	Target string `json:"target,omitempty"`
}

// Same reports whether two saved tunnels are the same tunnel, which is
// what stops one being kept twice.
func (t SavedTunnel) Same(other SavedTunnel) bool {
	return t.Host == other.Host && t.Kind == other.Kind &&
		t.Listen == other.Listen && t.Target == other.Target
}

// SavedCopy is a file copy the user asked to keep, so the same one can
// be run again without opening a browser to find the files.
//
// The ends are named rather than held: a machine that dropped and came
// back is a different connection under the same name, and a window is
// not there at all until this one connects to it again.
type SavedCopy struct {
	// From and To name the machines the copy is between, as the sidebar
	// names them. An empty one is the machine gridterm runs on.
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`

	// FromWindow and ToWindow name the window an end is reached
	// through, and are empty for a machine this window reaches itself.
	FromWindow string `json:"fromWindow,omitempty"`
	ToWindow   string `json:"toWindow,omitempty"`

	// At is the directory the files are copied from, Into is where they
	// go, and Names is what is copied.
	At    string   `json:"at"`
	Into  string   `json:"into"`
	Names []string `json:"names"`
}

// Same reports whether two saved copies do the same work, which is what
// keeping one twice and forgetting one go by.
func (c SavedCopy) Same(o SavedCopy) bool {
	return c.From == o.From && c.To == o.To &&
		c.FromWindow == o.FromWindow && c.ToWindow == o.ToWindow &&
		c.At == o.At && c.Into == o.Into && slices.Equal(sorted(c.Names), sorted(o.Names))
}

// sorted is the names in order, so which way round they were picked out
// does not make one copy two.
func sorted(names []string) []string {
	out := slices.Clone(names)
	slices.Sort(out)
	return out
}

// AgentMay is what a hand-over allows an agent beyond reading a pane and
// typing into it.
//
// Every one of them is off unless the user ticks it, so the zero value
// is what a hand-over gives when nothing has been remembered.
type AgentMay struct {
	// Restart lets the agent start a closed pane's program again.
	Restart bool `json:"restart,omitempty"`

	// OpenMore lets it open another pane on the machine this one is on.
	OpenMore bool `json:"openMore,omitempty"`

	// ReadOnly refuses its typing, for watching without touching.
	ReadOnly bool `json:"readOnly,omitempty"`

	// ReadBack lets it read above the last clear.
	ReadBack bool `json:"readBack,omitempty"`
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

// Dir returns the directory gridterm keeps its files in.
func Dir() (string, error) { return conf.Dir() }

// Path returns where the settings live.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, File), nil
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

// ServeOn reports whether this window was serving when it was last
// closed.
func (s *Settings) ServeOn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.have.ServeOn != nil && *s.have.ServeOn
}

// SecretsSaves is the highest number of writes the vault at this path
// has been seen to have, and zero for one never opened.
func (s *Settings) SecretsSaves(path string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.have.SecretsSaves[path]
}

// PutSecretsSaves writes down that the vault at this path has been seen
// with this many writes.
//
// It only ever goes up. A vault opened from an older copy on purpose
// still leaves the higher mark behind, so the next older copy is caught
// as well.
func (s *Settings) PutSecretsSaves(path string, saves uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	if s.have.SecretsSaves[path] >= saves {
		return nil
	}
	before := s.have
	// A map of its own, or the copy kept to put back on a failure would
	// be changed along with this one.
	next := make(map[string]uint64, len(s.have.SecretsSaves)+1)
	for at, n := range s.have.SecretsSaves {
		next[at] = n
	}
	next[path] = saves
	s.have.SecretsSaves = next
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// PutServeOn writes down whether the window is serving, for the next run
// to offer.
func (s *Settings) PutServeOn(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The file first: another window may have changed it since this one
	// read it, and writing a copy built from a stale read would throw
	// that work away.
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.ServeOn = &on
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

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

// AgentMay is what the hand-over dialog's tick boxes were last set to,
// and whether anything was saved.
func (s *Settings) AgentMay() (AgentMay, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.have.AgentMay == nil {
		return AgentMay{}, false
	}
	return *s.have.AgentMay, true
}

// PutAgentMay remembers what the hand-over dialog's tick boxes were set
// to, and saves.
func (s *Settings) PutAgentMay(may AgentMay) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The file first, for the same reason PutServe reads it first.
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.AgentMay = &may
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// Commands are the commands the user asked to keep, newest first.
func (s *Settings) Commands() []SavedCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.have.Commands)
}

// KeepCommand puts a command at the front of the list and saves, keeping
// at most most of them. A command already in the list moves to the front
// with whatever it was given here.
//
// The list is edited after the file has been reread, not before, so a
// command another window saved in the meantime is kept.
func (s *Settings) KeepCommand(cmd SavedCommand, most int) error {
	return s.putCommands(func(have []SavedCommand) []SavedCommand {
		want := append([]SavedCommand{cmd}, dropLine(have, cmd.Line)...)
		if most > 0 && len(want) > most {
			want = want[:most]
		}
		return want
	})
}

// DropCommand takes a command out of the list and saves.
func (s *Settings) DropCommand(line string) error {
	return s.putCommands(func(have []SavedCommand) []SavedCommand {
		return dropLine(have, line)
	})
}

// Tunnels are the tunnels the user asked to keep, newest first.
func (s *Settings) Tunnels() []SavedTunnel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.have.Tunnels)
}

// KeepTunnel puts a tunnel at the front of the list and saves, keeping
// at most most of them. One already in the list moves to the front.
func (s *Settings) KeepTunnel(t SavedTunnel, most int) error {
	return s.putTunnels(func(have []SavedTunnel) []SavedTunnel {
		want := append([]SavedTunnel{t}, dropTunnel(have, t)...)
		if most > 0 && len(want) > most {
			want = want[:most]
		}
		return want
	})
}

// DropTunnel takes a tunnel out of the list and saves.
func (s *Settings) DropTunnel(t SavedTunnel) error {
	return s.putTunnels(func(have []SavedTunnel) []SavedTunnel {
		return dropTunnel(have, t)
	})
}

// dropTunnel is the list without one tunnel.
func dropTunnel(have []SavedTunnel, t SavedTunnel) []SavedTunnel {
	return slices.DeleteFunc(have, func(at SavedTunnel) bool { return at.Same(t) })
}

// putTunnels rereads the file, edits the list it holds and saves.
func (s *Settings) putTunnels(edit func([]SavedTunnel) []SavedTunnel) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.Tunnels = edit(slices.Clone(s.have.Tunnels))
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// putCommands rereads the file, edits the list it holds and saves.
func (s *Settings) putCommands(edit func([]SavedCommand) []SavedCommand) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.Commands = edit(slices.Clone(s.have.Commands))
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// Copies are the copies the user keeps, newest first.
func (s *Settings) Copies() []SavedCopy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.have.Copies)
}

// KeepCopy puts a copy at the front of the list and saves, keeping at
// most most of them. One already in the list moves to the front.
func (s *Settings) KeepCopy(saved SavedCopy, most int) error {
	return s.putCopies(func(have []SavedCopy) []SavedCopy {
		want := append([]SavedCopy{saved}, dropCopy(have, saved)...)
		if most > 0 && len(want) > most {
			want = want[:most]
		}
		return want
	})
}

// DropCopy takes a copy out of the list and saves.
func (s *Settings) DropCopy(saved SavedCopy) error {
	return s.putCopies(func(have []SavedCopy) []SavedCopy {
		return dropCopy(have, saved)
	})
}

// putCopies rereads the file, edits the list it holds and saves.
func (s *Settings) putCopies(edit func([]SavedCopy) []SavedCopy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.Copies = edit(slices.Clone(s.have.Copies))
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// dropCopy is the copies with one of them left out.
func dropCopy(have []SavedCopy, want SavedCopy) []SavedCopy {
	return slices.DeleteFunc(have, func(c SavedCopy) bool { return c.Same(want) })
}

// dropLine is the commands with one line left out.
func dropLine(have []SavedCommand, line string) []SavedCommand {
	return slices.DeleteFunc(have, func(cmd SavedCommand) bool { return cmd.Line == line })
}

// Theme is the colour theme the window is drawn in, and whether one
// was picked.
func (s *Settings) Theme() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.have.Theme == nil {
		return "", false
	}
	return *s.have.Theme, true
}

// PutTheme remembers the colour theme, and saves.
func (s *Settings) PutTheme(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The file first, for the same reason PutServe reads it first.
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.Theme = &name
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// FontSize is the size the text is drawn at, in points, and whether one
// was ever written down.
func (s *Settings) FontSize() (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.have.FontSize == nil {
		return 0, false
	}
	return *s.have.FontSize, true
}

// PutFontSize remembers the size the text is drawn at, and saves.
func (s *Settings) PutFontSize(pt float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The file first, for the same reason PutServe reads it first.
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.FontSize = &pt
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// Keys are the key files the user keeps, newest first.
func (s *Settings) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.have.Keys)
}

// KeepKey puts a key file at the front of the list and saves, keeping at
// most most of them. One already in the list moves to the front.
func (s *Settings) KeepKey(path string, most int) error {
	return s.putKeys(func(have []string) []string {
		want := append([]string{path}, dropKey(have, path)...)
		if most > 0 && len(want) > most {
			want = want[:most]
		}
		return want
	})
}

// putKeys rereads the file, edits the list it holds and saves.
func (s *Settings) putKeys(edit func([]string) []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.Keys = edit(slices.Clone(s.have.Keys))
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// dropKey is the key files with one left out.
func dropKey(have []string, path string) []string {
	return slices.DeleteFunc(have, func(at string) bool { return at == path })
}

// PaneTitles reports whether each pane shows a line naming it.
func (s *Settings) PaneTitles() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.have.PaneTitles != nil && *s.have.PaneTitles
}

// PutPaneTitles turns the line above each pane on or off, and saves.
func (s *Settings) PutPaneTitles(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The file first, for the same reason PutServe reads it first.
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.PaneTitles = &on
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// ShellSetup reports whether a shell on this machine is taught to say
// what it is doing. Nothing saved means it is.
func (s *Settings) ShellSetup() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.have.ShellSetup == nil || *s.have.ShellSetup
}

// TermProgram is what the window calls itself in TERM_PROGRAM, and is
// empty for gridterm's own name.
func (s *Settings) TermProgram() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.have.TermProgram == nil {
		return ""
	}
	return *s.have.TermProgram
}

// PutTermProgram writes what the window calls itself, and saves. An
// empty name goes back to gridterm's own.
func (s *Settings) PutTermProgram(called string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The file first, for the same reason PutServe reads it first.
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	if called = strings.TrimSpace(called); called == "" {
		s.have.TermProgram = nil
	} else {
		s.have.TermProgram = &called
	}
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}

// PutShellSetup turns that on or off, and saves.
func (s *Settings) PutShellSetup(on bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The file first, for the same reason PutServe reads it first.
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.ShellSetup = &on
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

// ForgetShell takes the pick away, so a new pane runs whatever the
// machine's own default is, and saves.
func (s *Settings) ForgetShell() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The file first, for the same reason PutServe reads it first.
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.Shell = nil
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
	// Which themes there are is the window's business, so only an empty
	// name is turned away: it names nothing and could not be found.
	if th := file.Theme; th != nil && *th == "" {
		return errors.New("the theme has no name")
	}
	kept := make(map[string]bool, len(file.Keys))
	for i, path := range file.Keys {
		// Which key files there are is the window's business, so only an
		// empty path is turned away: it names nothing and could not be
		// offered back.
		if path == "" {
			return fmt.Errorf("key file %d has no path", i+1)
		}
		if kept[path] {
			return fmt.Errorf("key file %d, %q, is in the list twice", i+1, path)
		}
		kept[path] = true
	}
	seen := make(map[string]bool, len(file.Commands))
	for i, cmd := range file.Commands {
		// What runs is the window's business, so only an empty line is
		// turned away: it names nothing and could not be offered back.
		if cmd.Line == "" {
			return fmt.Errorf("saved command %d has nothing to run", i+1)
		}
		// A line twice over is a dead key in the dialog that offers
		// them: stepping from it lands on itself.
		if seen[cmd.Line] {
			return fmt.Errorf("saved command %d, %q, is in the list twice", i+1, cmd.Line)
		}
		seen[cmd.Line] = true
	}
	for i, saved := range file.Copies {
		// Which files there are is the window's business, so only a copy
		// with nothing to copy is turned away: it names nothing and
		// could not be run.
		if len(saved.Names) == 0 {
			return fmt.Errorf("saved copy %d has nothing to copy", i+1)
		}
		// One twice over is a dead cross in the dialog that offers them:
		// forgetting either takes both out of the file and leaves a row
		// nothing answers.
		if slices.ContainsFunc(file.Copies[:i], saved.Same) {
			return fmt.Errorf("saved copy %d, %q, is in the list twice", i+1, saved.Names[0])
		}
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
