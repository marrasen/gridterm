package main

import (
	"context"
	"maps"
	"sync/atomic"
	"time"

	"fmt"
	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/keys"
	"github.com/marrasen/gridterm/logs"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/secrets"
	"github.com/marrasen/gridterm/settings"
	shellfind "github.com/marrasen/gridterm/shells"
	"github.com/marrasen/gridterm/vfs"
	"github.com/marrasen/gridterm/vt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/theme"
)

// The program side of the window. It owns the panes, how they are
// arranged, and which one has the keyboard, on a goroutine of its own.
// It hears what the window asks for as intents and, after each change,
// publishes the state the window shows.

// State is what the window shows.
type State struct {
	// Panes are the open panes, in the sidebar's order.
	Panes []Pane
	// Stage is the arrangement on screen: the group of panes the
	// focused pane belongs to. Nil when no pane is open.
	Stage *Box
	Focus string
	// Sidebar is whether the sidebar shows, and SidebarWidth how wide.
	Sidebar      bool
	SidebarWidth float32
	// FontSize is the terminals' font size in logical pixels.
	FontSize float32
	// Fonts are the families to draw the terminals in, and Font the one
	// they are drawn in.
	Fonts []string
	Font  Font
	// Marks are the colours of the rings round shared panes.
	Marks Marks
	// FileClip is what the file clipboard holds, marked in the lists.
	FileClip FileClip
	// Theme names the theme the window is drawn in, and Themes those on
	// offer.
	Theme  string
	Themes []string
	// Asks are the questions connections are waiting on the user for,
	// oldest first.
	Asks []Ask
	// Saved are the saved servers.
	Saved []remote.Host
	// Browsers and Readers are what the file panes and the readers
	// show, by pane. Each is replaced whole, never changed in place.
	Browsers map[string]Browser
	Readers  map[string]Reader
	// Tunnels are the tunnels open, and those stopped until cleared,
	// and SavedTunnels those kept for next time, newest first.
	Tunnels []Tunnel
	// Jobs are the file jobs, running and finished, oldest first.
	Jobs []Job
	// Accounts are the machines with a connection log, in the order
	// their first connection began.
	Accounts []string
	// Secrets is what the vault holds, by name.
	Secrets Secrets
	// Share is the panes shared with an agent.
	Share Share
	// Serving is this window served to others.
	Serving Serving
	// Windows are the windows this one is connected to.
	Windows []RemoteWindow
	// PaneTitles says each pane shows a line naming it, and Bells
	// counts the bells rung in panes, for the window to ask for the
	// user's attention.
	PaneTitles bool
	// SavedCommands are the commands kept, newest first.
	SavedCommands []settings.SavedCommand
	// Shells are the shells found on this machine, and ChosenShell the
	// one new terminals start, "" for the user's own.
	// Connected are the servers connected to, by name, and Dialing the
	// ones being connected to.
	Connected   []string
	Dialing     []string
	Shells      []ShellChoice
	ChosenShell string
	// ShellSetup says new shells here are taught to say what they are
	// doing, and TermProgram what they are told the terminal is called,
	// "" for gridterm's own name.
	// Shortcuts are the changes the user's shortcuts file makes to the
	// window's keys, and ShortcutsRead counts its reads. Contents are
	// the themes' colours for the panes, when the themes were read
	// again.
	// SavedCopies are the copies kept, newest first.
	SavedCopies   []settings.SavedCopy
	Shortcuts     []keys.Change
	ShortcutsRead uint64
	Contents      map[string]theme.Theme
	ShellSetup    bool
	TermProgram   string
	Bells         uint64
	SavedTunnels  []settings.SavedTunnel
	Status        string
	// Notices are the latest notices, oldest first, for the window to
	// show each once.
	Notices []Notice
	// Output counts the times shells wrote, so the window copies their
	// screens once a frame however often they write.
	Output uint64
}

// Notice is something to tell the user once, in a toast. Clipboard,
// when set, goes on the clipboard as it shows.
type Notice struct {
	ID          uint64
	Title, Body string
	Clipboard   string
	// Forget has the window take Clipboard back off the clipboard in
	// half a minute, unless something else was copied since.
	Forget bool
}

// Pane is one pane, as the sidebar lists it.
type Pane struct {
	ID    string
	Title string
	// Machine is the server the pane's shell runs on, "" for this
	// computer.
	Machine string
	// On is the machine the pane runs on when that is a server the
	// window in Machine reached, and "" for the window's own.
	On string
	// Kind says what the pane is: a terminal, a file pane or a reader.
	Kind string
	// Named is set once the user has named the pane, and shell is the
	// title its shell last gave it.
	Named bool
	shell string
	// Tunnel is the tunnel a tunnel's pane tells of.
	Tunnel string
	// Ended says the program in a terminal pane has ended; the pane
	// stays, asking whether to start it again.
	Ended bool
	// Rang says its program rang the bell since the user last looked.
	Rang bool
	// Note is what the program in the pane says about itself: how far
	// along it is, and its last message.
	Note string
	// Command says the pane runs one command rather than a shell, and
	// offers to run it again when it finishes.
	Command bool
}

// Box is one part of an arrangement: a pane, or a split of two boxes.
type Box struct {
	// Pane names the pane a leaf shows; a split has none.
	Pane string
	// ID names a split, so the window keeps its divider where it is
	// from one state to the next.
	ID       string
	Vertical bool
	// Share is the first box's share of the split's space.
	Share float32
	// Opening marks a split just made, whose new pane slides in.
	Opening bool
	A, B    *Box
}

func (b *Box) clone() *Box {
	if b == nil {
		return nil
	}
	c := *b
	c.A, c.B = b.A.clone(), b.B.clone()
	return &c
}

// leaves appends the panes under b, first to last.
func (b *Box) leaves(out []string) []string {
	switch {
	case b == nil:
		return out
	case b.Pane != "":
		return append(out, b.Pane)
	}
	return b.B.leaves(b.A.leaves(out))
}

// beside returns the pane nearest pane across the split that holds
// it: the first one on that side when pane is first, and the last one
// when it is second.
func (b *Box) beside(pane string) string {
	if b == nil || b.Pane != "" {
		return ""
	}
	switch {
	case b.A.Pane == pane:
		return b.B.leaves(nil)[0]
	case b.B.Pane == pane:
		l := b.A.leaves(nil)
		return l[len(l)-1]
	}
	if n := b.A.beside(pane); n != "" {
		return n
	}
	return b.B.beside(pane)
}

// replace returns b with the leaf for pane swapped for with, which may
// be nil to take the leaf out: its split then gives way to the other
// side.
func (b *Box) replace(pane string, with *Box) *Box {
	switch {
	case b == nil:
		return nil
	case b.Pane == pane:
		return with
	case b.Pane != "":
		return b
	}
	a, c := b.A.replace(pane, with), b.B.replace(pane, with)
	switch {
	case a == nil:
		return c
	case c == nil:
		return a
	}
	n := *b
	n.A, n.B = a, c
	return &n
}

// Intents: what the window asks the program for.
type (
	// NewTerminal opens a shell in a pane of its own.
	NewTerminal struct{}
	// SplitPane splits the focused pane, and opens a shell in the new
	// half: to the right, or below with Vertical.
	SplitPane struct{ Vertical bool }
	// ClosePane closes a pane, or the focused one when Pane is empty.
	ClosePane struct{ Pane string }
	// FocusPane gives a pane the keyboard, bringing its group on stage.
	FocusPane struct{ Pane string }
	// NextPane moves the keyboard to the next pane in the sidebar, or
	// the one before with Back.
	NextPane struct{ Back bool }
	// PopOut takes the focused pane out of its split, onto a stage of
	// its own.
	PopOut struct{}
	// ToggleSidebar shows or hides the sidebar.
	ToggleSidebar struct{}
	// SplitMoved says where the pointer left a split's divider.
	SplitMoved struct {
		Split string
		Share float32
	}
	// SidebarMoved says how wide the pointer left the sidebar.
	SidebarMoved struct{ Width float32 }
	// Exit closes the window, and every shell in it, after asking while
	// anything is open.
	Exit struct{}
	// RenamePane names a pane; its shell's titles no longer change it.
	// An empty name hands the name back to the shell.
	RenamePane struct{ Pane, Title string }
	// DialogClosed says a dialog closed without a change, so the window
	// gives the keyboard back.
	DialogClosed struct{}
	// TogglePaneTitles shows or hides the line naming each pane.
	TogglePaneTitles struct{}
	// RunSavedCommand runs a command kept from before.
	RunSavedCommand struct{ Saved settings.SavedCommand }
	// OpenOn opens a terminal on Machine, "" for this computer.
	OpenOn struct{ Machine string }
	// FilesOn opens a file pane on Machine, at Path, or at home when
	// Path is empty.
	FilesOn struct{ Machine, Path string }
	// ShowScrollback opens what a terminal pane has kept, scrollback
	// and screen, in a reader beside it, to search and copy from.
	ShowScrollback struct{ Pane string }
	// ReloadServers reads the saved servers again, for a list changed
	// by another window or by hand.
	ReloadServers struct{}
	// ClearFinished closes the panes whose programs have ended and
	// clears the tunnels that stopped.
	ClearFinished struct{}
	// FontSize makes the terminals' text a point larger, or smaller,
	// or, with no Step, the size it started at.
	FontSize struct{ Step int }
	// PickTheme draws the window, terminals and all, in a theme.
	PickTheme struct{ Name string }
	// ConnectTo connects to a server typed as user@host:port, or to a
	// saved one by name, and opens a shell there. Connected already, it
	// opens another shell.
	ConnectTo struct{ Target, Saved string }
	// SaveServer saves a server, in place of the one named Under when
	// that is set.
	SaveServer struct {
		Host  remote.Host
		Under string
	}
	// RemoveServer forgets a saved server.
	RemoveServer struct{ Name string }
	// AskAnswered answers a question: Yes and the answers, or no.
	AskAnswered struct {
		ID      uint64
		Yes     bool
		Answers []string
	}
)

// app is the program side's state. It belongs to the goroutine running
// run.
type app struct {
	c      gunim.Client
	shells *shells
	st     State
	// groups holds each group's arrangement, and groupOf each pane's
	// group.
	groups  map[int]*Box
	groupOf map[string]int
	// next numbers the panes, and nextGroup the groups.
	next      int
	nextGroup int
	// notices counts the notices made.
	notices uint64
	// ctx ends with the window. conns are the connections open, by the
	// machine's name, dialing the ones being made, and ring holds the
	// keys unlocked so far. book is the saved servers.
	ctx     context.Context
	conns   map[string]*remote.Conn
	dialing map[string]bool
	ring    *remote.Ring
	book    *remote.Book
	// replies waits for the answers to asks, by ID, and askIDs counts
	// them.
	replies map[uint64]chan AskAnswered
	// closing holds the panes folding away.
	closing map[string]bool
	// local is this computer's filesystem, once a file pane needs it,
	// and remoteFS the servers' files opened so far, by machine.
	local    vfs.FS
	remoteFS map[string]vfs.FS
	// themes are the themes on offer, and palette the terminals' now.
	// settings is gridterm's settings file, which keeps the theme
	// picked.
	themes   []themed
	palette  vt.Palette
	settings *settings.Settings
	// clip is the file clipboard, jobs the queue of file work, and
	// running the jobs followed.
	clip    *fileClip
	jobs    *jobs.Queue
	running []*running
	// jobSeq counts the jobs, and watching is set while a goroutine
	// looks at them.
	jobSeq   int
	watching bool
	askIDs   atomic.Uint64
	// tunnels are the tunnels by ID, tunnelSeq counts them, ticking is
	// set while their notes are kept up to date, and quiet says a tick
	// changed nothing, so nothing is published.
	tunnels map[string]*tunnel
	// accounts are the connection logs, by machine.
	accounts map[string]*logs.Lines
	// secrets is the vault, once asked for, and secretsAt where it is
	// kept, when a test says. lastTerminal is the terminal pane that
	// last had the keyboard, for typing a secret into.
	secrets      *secrets.Vault
	secretsAt    string
	lastTerminal string
	// agents is the share of panes with an agent.
	agents agents
	// serving serves this window to others.
	serving serving
	// windows are the windows connected to, by name.
	windows map[string]*remoteWin
	// leaving is set while the window asks whether to close.
	leaving bool
	// copied is the last secret put on the clipboard, and copiedAt when.
	copied   string
	copiedAt time.Time
	// opts are what the command line asked for; fixedFont is a family
	// -font-family named, and shotErr why a -shot script gave up.
	opts      options
	fixedFont string
	shotErr   error
	// paneFiles is each file pane's view of its machine's files.
	paneFiles map[string]wrappedFiles
	// dialCancel gives up each connection being made, and dialWaiters
	// are what is to happen once each has come back.
	dialCancel  map[string]context.CancelFunc
	dialWaiters map[string][]func(error)
	// reached is the address each server was reached at, and paneAt
	// the address each pane on one was opened at, to say so when a
	// pane is reconnected somewhere else.
	reached map[string]string
	paneAt  map[string]string
	// noticed is the number of the last message each pane's program
	// sent, and lastToast when the last pop-up went up.
	noticed   map[string]uint64
	lastToast time.Time
	// families are the monospaced families found here; wantFont is the
	// one the theme names, taken unless fontPicked says the user chose
	// from the Font menu.
	families   []glyph.Family
	wantFont   string
	fontPicked bool
	// commands are what each command pane runs, to run it again.
	commands map[string]command
	// argvs are what each local pane runs, to start it again and to
	// know how to hand it a picture.
	argvs map[string][]string
	// farHost is the machine a pane attached from another window runs
	// on, when that is a machine the window reached rather than its own.
	farHost map[string]string
	// far remembers which paths on servers are there, for links.
	far pathsFar
	// typed is what agents typed, by pane.
	typed map[string]*typedLog
	// reads are what each reader pane reads, to read it again.
	reads map[string]readSpec
	// nextShell is the command the next terminal here starts, once.
	nextShell []string
	// found are the shells on this machine.
	found []shellfind.Shell
	// registerThemes names themes to the window, for reading them
	// again.
	registerThemes func([]themed)
	tunnelSeq      int
	ticking        bool
	quiet          bool
	// wake hears that a shell wrote, and events carries changes from
	// the shells' goroutines to this one.
	wake   chan struct{}
	events chan func()
}

// shells holds the running shells by pane, for the window to draw.
type shells struct {
	mu sync.Mutex
	m  map[string]*shell
}

func (s *shells) get(id string) *shell {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[id]
}

// all returns the running shells.
func (s *shells) all() []*shell {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*shell, 0, len(s.m))
	for _, sh := range s.m {
		out = append(out, sh)
	}
	return out
}

func (s *shells) set(id string, sh *shell) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sh == nil {
		delete(s.m, id)
		return
	}
	s.m[id] = sh
}

func newApp(c gunim.Client, sh *shells) *app {
	return &app{
		c:           c,
		shells:      sh,
		st:          State{Sidebar: true, SidebarWidth: 220, FontSize: defaultFontSize, Fonts: []string{bundledFamily, dosFamily}},
		groups:      map[int]*Box{},
		groupOf:     map[string]int{},
		conns:       map[string]*remote.Conn{},
		dialing:     map[string]bool{},
		ring:        remote.NewRing(),
		replies:     map[uint64]chan AskAnswered{},
		closing:     map[string]bool{},
		remoteFS:    map[string]vfs.FS{},
		tunnels:     map[string]*tunnel{},
		agents:      agents{by: map[string]*handover{}},
		windows:     map[string]*remoteWin{},
		commands:    map[string]command{},
		noticed:     map[string]uint64{},
		reached:     map[string]string{},
		paneFiles:   map[string]wrappedFiles{},
		dialCancel:  map[string]context.CancelFunc{},
		dialWaiters: map[string][]func(error){},
		paneAt:      map[string]string{},
		argvs:       map[string][]string{},
		farHost:     map[string]string{},
		typed:       map[string]*typedLog{},
		reads:       map[string]readSpec{},
		far:         pathsFar{known: map[string]farPath{}, asking: map[string]bool{}},
		accounts:    map[string]*logs.Lines{},
		wake:        make(chan struct{}, 1),
		events:      make(chan func(), 64),
	}
}

// run serves the window until it closes or ctx ends.
func (a *app) run(ctx context.Context) error {
	a.ctx = ctx
	a.palette = vt.DefaultPalette()
	for _, t := range a.themes {
		a.st.Themes = append(a.st.Themes, t.name)
	}
	if path, err := settings.Path(); err == nil {
		if s, err := settings.Load(path); err == nil {
			a.settings = s
			a.st.SavedTunnels = s.Tunnels()
			a.st.PaneTitles = s.PaneTitles()
			a.st.SavedCommands = s.Commands()
			a.st.ChosenShell, _ = s.Shell()
			a.st.ShellSetup = s.ShellSetup()
			a.st.TermProgram = s.TermProgram()
			a.st.SavedCopies = s.Copies()
			if size, ok := s.FontSize(); ok && !a.opts.sizeSet {
				a.st.FontSize = min(max(float32(size), 8), 40)
			}
		}
	}
	// The theme picked last time, as gridterm keeps it, or the first.
	if len(a.themes) > 0 {
		name := a.themes[0].name
		if a.settings != nil {
			if picked, ok := a.settings.Theme(); ok && slices.ContainsFunc(a.themes, func(t themed) bool { return t.name == picked }) {
				name = picked
			}
		}
		a.pickTheme(name)
	}
	a.showShare()
	a.showServing()
	a.scanShells()
	a.scanFonts()
	// Whether there are secrets, read off the disk and left locked.
	if _, err := a.vault(); err == nil {
		a.showVault()
	}
	if err := a.loadShortcuts(false); err != nil {
		a.notify("Couldn't read the shortcuts file", err.Error(), "")
	}
	if a.settings != nil && a.settings.ServeOn() {
		go a.offerToServeAgain()
	}
	if path, err := remote.BookPath(); err == nil {
		if b, err := remote.LoadBook(path); err == nil {
			a.book = b
			a.st.Saved = b.Hosts()
			a.giveSavedIDs()
		}
	}
	if err := a.applyOptions(); err != nil {
		return err
	}
	if err := a.openFirst(); err != nil {
		return err
	}
	a.publish()
	if a.opts.shot != "" {
		list, err := parseShot(a.opts.shot)
		if err != nil {
			return fmt.Errorf("-shot: %w", err)
		}
		go a.runShot(list)
	}
	intents := a.c.Intents()
	for {
		select {
		case <-ctx.Done():
			a.takeSecretBack()
			return nil
		case env, ok := <-intents:
			if !ok {
				return a.c.Err()
			}
			a.handle(env.Intent)
		case <-a.wake:
			a.st.Output++
		case f := <-a.events:
			f()
		}
		// Empty, and connecting to nothing that would open a pane: the
		// window closes.
		if len(a.st.Panes) == 0 && len(a.dialing) == 0 {
			a.c.Close()
			intents = a.c.Intents()
			for range intents {
			}
			return a.c.Err()
		}
		if a.quiet {
			a.quiet = false
			continue
		}
		if a.kindOfPane(a.st.Focus) == kindTerminal {
			a.lastTerminal = a.st.Focus
		}
		a.setPane(a.st.Focus, func(p *Pane) { p.Rang = false })
		a.publish()
	}
}

func (a *app) publish() {
	a.notePanes()
	a.st.FileClip = FileClip{}
	if c := a.clip; c != nil {
		a.st.FileClip = FileClip{Key: c.machine, At: c.at, Names: slices.Clone(c.names), Cut: c.kind == jobs.Move}
	}
	st := a.st
	st.Panes = slices.Clone(a.st.Panes)
	st.Notices = slices.Clone(a.st.Notices)
	st.Asks = slices.Clone(a.st.Asks)
	st.Saved = slices.Clone(a.st.Saved)
	st.Themes = slices.Clone(a.st.Themes)
	st.Tunnels = slices.Clone(a.st.Tunnels)
	st.Jobs = slices.Clone(a.st.Jobs)
	st.Accounts = slices.Clone(a.st.Accounts)
	st.Share.Panes = slices.Clone(a.st.Share.Panes)
	st.Serving.Clients = slices.Clone(a.st.Serving.Clients)
	st.Serving.Allowed = slices.Clone(a.st.Serving.Allowed)
	st.Windows = slices.Clone(a.st.Windows)
	st.SavedCommands = slices.Clone(a.st.SavedCommands)
	st.Shells = slices.Clone(a.st.Shells)
	st.SavedCopies = slices.Clone(a.st.SavedCopies)
	st.Connected = slices.Sorted(maps.Keys(a.conns))
	st.Dialing = slices.Sorted(maps.Keys(a.dialing))
	a.tellServed()
	st.SavedTunnels = slices.Clone(a.st.SavedTunnels)
	st.Stage = a.groups[a.groupOf[a.st.Focus]].clone()
	st.Secrets.Waiting = a.waitingForSecret()
	_ = a.c.Publish(windowTopic, st)
	// A split opens once; after that it is only a split.
	for _, g := range a.groups {
		clearOpening(g)
	}
}

func clearOpening(b *Box) {
	if b == nil {
		return
	}
	b.Opening = false
	clearOpening(b.A)
	clearOpening(b.B)
}

func (a *app) handle(in gunim.Intent) {
	if a.needsFiles(in) {
		return
	}
	var err error
	switch in := in.(type) {
	case NewTerminal:
		err = a.openTerminal()
	case SplitPane:
		err = a.split(in.Vertical)
	case ClosePane:
		id := in.Pane
		if id == "" {
			id = a.st.Focus
		}
		a.closePane(id)
	case FocusPane:
		if a.has(in.Pane) {
			a.st.Focus = in.Pane
		}
	case NextPane:
		a.nextPane(in.Back)
	case PopOut:
		a.popOut()
	case ToggleSidebar:
		a.st.Sidebar = !a.st.Sidebar
	case SplitMoved:
		setShare(a.groups[a.groupOf[a.st.Focus]], in.Split, in.Share)
	case SidebarMoved:
		a.st.SidebarWidth = in.Width
	case Exit:
		a.askToQuit()
	case RenamePane:
		for i := range a.st.Panes {
			if p := &a.st.Panes[i]; p.ID == in.Pane {
				p.Named = in.Title != ""
				p.Title = in.Title
				if !p.Named {
					p.Title = p.shell
				}
			}
		}
	case PickTheme:
		a.pickTheme(in.Name)
		if a.settings != nil {
			if err := a.settings.PutTheme(in.Name); err != nil {
				a.notify("Couldn't keep the theme for next time", err.Error(), "")
			}
		}
	case FontSize:
		size := defaultFontSize
		if in.Step != 0 {
			size = min(max(a.st.FontSize+float32(in.Step), 8), 40)
		}
		a.st.FontSize = size
		if a.settings != nil {
			if err := a.settings.PutFontSize(float64(size)); err != nil {
				a.notify("Couldn't keep the font size for next time", err.Error(), "")
			}
		}
	case PickFont:
		err = a.pickFont(in.Name)
	case ConnectTo:
		err = a.connect(in)
	case OpenFiles:
		err = a.openFiles()
	case Browse:
		a.browse(in)
	case ReadFile:
		a.readFile(in)
	case EnterEntry:
		a.enter(in)
	case ClipFiles:
		a.clipFiles(in)
	case PasteFiles:
		err = a.pasteFiles(in)
	case DeleteFiles:
		a.deleteFiles(in)
	case RenameFile:
		a.renameFile(in)
	case MakeFolder:
		a.makeFolder(in)
	case GoUp:
		a.goUp(in)
	case GoTo:
		a.goTo(in)
	case ViewFile:
		a.viewFile(in)
	case SaveServer:
		err = a.saveServer(in)
	case RemoveServer:
		err = a.removeServer(in.Name)
	case AskAnswered:
		if reply, ok := a.replies[in.ID]; ok {
			a.dropAsk(in.ID)
			reply <- in
		}
	case OpenTunnel:
		err = a.openTunnel(in)
	case OpenSavedTunnel:
		err = a.openSavedTunnel(in.Saved)
	case CloseTunnel:
		err = a.closeTunnel(in.ID)
	case WatchTunnel:
		a.watchTunnel(in)
	case ShowTunnel:
		a.showTunnel(in.ID)
	case ShowSecrets:
		a.showSecretsPane()
	case UnlockSecrets:
		a.withSecrets("Couldn't open the secrets", func(*secrets.Vault) error { return nil })
	case LockSecrets:
		a.lockSecrets()
	case PutSecret:
		a.putSecret(in)
	case RemoveSecret:
		a.withSecrets("Couldn't remove the secret", func(v *secrets.Vault) error { return v.Remove(in.ID) })
	case CopySecret:
		a.copySecret(in.ID)
	case TypeSecret:
		a.typeSecret(in.ID)
	case RevealSecret:
		a.revealSecret(in.ID)
	case AddSecretsKey:
		a.addSecretsKey()
	case RemoveSecretsKey:
		a.removeSecretsKey(in.Fingerprint)
	case AddSecretsPassphrase:
		a.addSecretsPassphrase(in.Passphrase)
	case ExportSecrets:
		a.exportSecrets(in)
	case ImportSecrets:
		a.importSecrets(in)
	case SharePane:
		err = a.sharePane(in.Pane)
	case UnsharePane:
		err = a.unsharePane(in.Pane)
	case StopSharing:
		err = a.stopSharing()
	case SetAgentMay:
		a.setAgentMay(in)
	case CopyAgentPrompt:
		a.copyAgentPrompt(in.Host)
	case WriteSkill:
		err = a.writeSkill(in)
	case CopyAgentSetup:
		a.copyAgentSetup(in.Host)
	case StartServing:
		err = a.startServing(in)
	case StopServing:
		err = a.stopServing()
	case DisconnectClients:
		err = a.disconnectClients()
	case ConnectWindow:
		err = a.connectWindow(in)
	case DisconnectWindow:
		err = a.disconnectWindow(in.Name)
	case AttachWindow:
		err = a.attachWindow(in)
	case Disconnect:
		err = a.disconnect(in.Machine)
	case ShowLog:
		a.showLog(in.Machine)
	case ShowJobs:
		a.showJobsPane()
	case CancelJob:
		a.cancelJob(in.ID)
	case ClearJobs:
		a.clearJobs(true)
		a.showJobs()
	case RunCommand:
		err = a.runCommand(in)
	case RunSavedCommand:
		err = a.runSavedCommand(in.Saved)
	case OpenShellNamed:
		argv := a.shellCommand(in.ID)
		if argv == nil {
			err = fmt.Errorf("this machine has no shell called %q", in.ID)
			break
		}
		a.nextShell = argv
		err = a.open("", placement{})
	case ToggleShellSetup:
		a.st.ShellSetup = !a.st.ShellSetup
		if a.settings != nil {
			err = a.settings.PutShellSetup(a.st.ShellSetup)
		}
	case SetTermProgram:
		a.st.TermProgram = strings.TrimSpace(in.Called)
		if a.settings != nil {
			err = a.settings.PutTermProgram(a.st.TermProgram)
		}
	case PickShell:
		err = a.pickShell(in.ID)
	case OpenDefaultShell:
		if err = a.pickShell(""); err == nil {
			err = a.open("", placement{})
		}
	case OpenOn:
		err = a.open(in.Machine, placement{})
	case FilesOn:
		err = a.filesOn(in.Machine, in.Path)
	case ReloadShortcuts:
		err = a.loadShortcuts(true)
	case WriteShortcuts:
		err = a.writeShortcuts(in.Bindings)
	case ReloadThemes:
		a.reloadThemes()
	case WriteThemeFile:
		err = a.writeThemeFile()
	case CheckUpdates:
		a.checkUpdates()
	case ShowHelp:
		a.showHelp()
	case MakeKey:
		err = a.makeKey(in)
	case LockKeys:
		a.lockKeys()
	case ShowTyped:
		err = a.showTyped(in.Pane)
	case RepeatJob:
		err = a.repeatJob(in.ID)
	case SaveCopy:
		err = a.saveCopy(in)
		a.showJobs()
	case RunSavedCopy:
		err = a.runSavedCopy(in.Saved)
	case ForgetCopy:
		err = a.forgetCopy(in.Saved)
	case ShowCopies:
		a.showCopies()
	case ReadAgain:
		if _, ok := a.reads[in.Pane]; ok {
			a.readOnce(in.Pane)
		} else if r, ok := a.st.Readers[in.Pane]; ok {
			// Lines with no file behind them, as the scrollback's, are
			// what they were.
			r.Seq++
			a.setReader(in.Pane, r)
		}
	case SaveLines:
		a.saveLines(in)
	case DropFileClip:
		a.clip = nil
	case ListFolders:
		a.listFolders(in)
	case DropFiles:
		err = a.dropFiles(in)
	case PasteImage:
		err = a.pastePicture(in.Pane, true)
	case PastePicture:
		err = a.pastePicture(in.Pane, false)
	case ShowScrollback:
		err = a.showScrollback(in.Pane)
	case ReloadServers:
		err = a.reloadServers()
	case ClearFinished:
		a.clearFinished()
	case TogglePaneTitles:
		a.st.PaneTitles = !a.st.PaneTitles
		if a.settings != nil {
			if err := a.settings.PutPaneTitles(a.st.PaneTitles); err != nil {
				a.notify("Couldn't keep the pane titles for next time", err.Error(), "")
			}
		}
	case DialogClosed:
	}
	if err != nil {
		a.notify("That didn't work", err.Error(), "")
	}
}

// notify tells the user something, once, in a toast.
func (a *app) notify(title, body, clip string) {
	a.notices++
	a.st.Notices = append(a.st.Notices, Notice{ID: a.notices, Title: title, Body: body, Clipboard: clip})
	// The window has shown all but the newest few by now.
	if n := len(a.st.Notices); n > 8 {
		a.st.Notices = slices.Delete(a.st.Notices, 0, n-8)
	}
}

// titleOf returns a pane's title, or empty.
func (a *app) titleOf(id string) string {
	for _, p := range a.st.Panes {
		if p.ID == id {
			return p.Title
		}
	}
	return ""
}

func setShare(b *Box, id string, share float32) {
	if b == nil {
		return
	}
	if b.ID == id {
		b.Share = share
	}
	setShare(b.A, id, share)
	setShare(b.B, id, share)
}

func (a *app) has(id string) bool {
	return slices.ContainsFunc(a.st.Panes, func(p Pane) bool { return p.ID == id })
}

// placement says where a new pane goes: on a stage of its own, or
// beside a pane, below it with vertical.
type placement struct {
	beside   string
	vertical bool
}

// hooks are what a pane's shell tells the program.
func (a *app) hooks(id string) shellHooks {
	return shellHooks{
		output: func() {
			select {
			case a.wake <- struct{}{}:
			default:
			}
		},
		title: func(t string) { a.events <- func() { a.retitle(id, t) } },
		exit:  func() { a.events <- func() { a.paneEnded(id) } },
		bell: func() {
			a.events <- func() {
				a.st.Bells++
				if a.st.Focus != id {
					a.setPane(id, func(p *Pane) { p.Rang = true })
				}
			}
		},
		clipboard: func(s string) {
			a.events <- func() {
				a.notify("Copied to the clipboard", fmt.Sprintf("%d characters, from %s", utf8.RuneCountInString(s), a.titleOf(id)), s)
			}
		},
	}
}

// open opens a shell on machine, "" for this one, as a new pane placed
// at at. A remote shell opens over the machine's connection in the
// background, and its pane arrives once it has.
func (a *app) open(machine string, at placement) error { return a.openThen(machine, at, nil) }

// openThen is open, telling then the pane it opened, or why it could
// not, once it has. then runs on the program's goroutine, and may be
// nil.
func (a *app) openThen(machine string, at placement, then func(id string, err error)) error {
	if then == nil {
		then = func(string, error) {}
	}
	if machine != "" && a.conns[machine] == nil && a.windows[machine] == nil {
		// Not connected: connected to first, as a saved server's plus
		// in the sidebar does in gridterm.
		return a.dialAgain(machine, func(err error) {
			if err != nil {
				then("", err)
				return
			}
			if err := a.openThen(machine, at, then); err != nil {
				a.notify("Couldn't open a shell on "+machine, err.Error(), "")
			}
		})
	}
	a.next++
	id := "p" + strconv.Itoa(a.next)
	title := fmt.Sprintf("Terminal %d", a.next)
	if machine == "" {
		argv := a.localShell()
		if a.nextShell != nil {
			argv, a.nextShell = a.nextShell, nil
		}
		sess, err := a.startLocalSession(argv, a.dirHere(), shellCols, shellRows, true)
		if err != nil {
			return fmt.Errorf("gunimterm: start the shell: %w", err)
		}
		sh := openShell(sess, a.palette, a.withLinks(a.hooks(id), ""))
		a.argvs[id] = argv
		a.addPane(Pane{ID: id, Title: title}, sh, at)
		then(id, nil)
		return nil
	}
	if _, ok := a.windows[machine]; ok {
		return a.openOnWindow(machine, id, title, at, then)
	}
	conn, ok := a.conns[machine]
	if !ok {
		return fmt.Errorf("gunimterm: %s is not connected", machine)
	}
	go func() {
		sess, err := conn.Shell(a.ctx, remote.ShellConfig{Cols: shellCols, Rows: shellRows})
		a.events <- func() {
			if err != nil {
				a.notify("Couldn't open a shell on "+machine, err.Error(), "")
				then("", err)
				return
			}
			a.teachFar(machine, sess)
			a.paneAt[id] = a.reached[machine]
			a.addPane(Pane{ID: id, Title: title, Machine: machine}, openShell(sess, a.palette, a.withLinks(a.hooks(id), machine)), at)
			then(id, nil)
		}
	}()
	return nil
}

// addPane shows a new pane, with the keyboard: beside at.beside while
// that pane is still open, and otherwise on a stage of its own.
func (a *app) addPane(p Pane, sh *shell, at placement) {
	if sh != nil {
		a.shells.set(p.ID, sh)
	}
	a.st.Panes = append(a.st.Panes, p)
	a.nextGroup++
	g, ok := a.groupOf[at.beside]
	if at.beside == "" || !ok {
		g = a.nextGroup
		a.groups[g] = &Box{Pane: p.ID}
	} else {
		box := &Box{
			ID: "s" + strconv.Itoa(a.next), Vertical: at.vertical, Share: 0.5, Opening: true,
			A: &Box{Pane: at.beside}, B: &Box{Pane: p.ID},
		}
		a.groups[g] = a.groups[g].replace(at.beside, box)
	}
	a.groupOf[p.ID] = g
	a.st.Focus = p.ID
}

// machineOf returns the machine a pane is on, "" for this one.
func (a *app) machineOf(id string) string {
	for _, p := range a.st.Panes {
		if p.ID == id {
			return p.Machine
		}
	}
	return ""
}

// openTerminal opens a shell where the focused pane is, on a stage of
// its own.
func (a *app) openTerminal() error { return a.open(a.machineOf(a.st.Focus), placement{}) }

// split opens a shell beside the focused pane, on its machine.
func (a *app) split(vertical bool) error {
	return a.open(a.machineOf(a.st.Focus), placement{beside: a.st.Focus, vertical: vertical})
}

// take takes a pane out of its group's arrangement, and reports the
// pane that should have the keyboard in its place: the nearest pane on
// the other side of the split it leaves.
func (a *app) take(id string) string {
	g, ok := a.groupOf[id]
	if !ok {
		return ""
	}
	delete(a.groupOf, id)
	next := a.groups[g].beside(id)
	rest := a.groups[g].replace(id, nil)
	if rest == nil {
		delete(a.groups, g)
		return ""
	}
	a.groups[g] = rest
	return next
}

// foldTime is how long a closing pane takes to fold away before it
// goes.
const foldTime = 350 * time.Millisecond

// closePane closes a pane. One in a split folds away first: the split
// gives its space to the other side, and the pane goes once it has.
// The keyboard moves to the pane beside it at once.
func (a *app) closePane(id string) {
	if a.closing[id] || !a.has(id) {
		return
	}
	g, ok := a.groupOf[id]
	if !ok || !fold(a.groups[g], id) {
		a.remove(id)
		return
	}
	a.closing[id] = true
	if a.st.Focus == id {
		if next := a.groups[g].beside(id); next != "" {
			a.st.Focus = next
		}
	}
	if sh := a.shells.get(id); sh != nil {
		sh.close()
	}
	time.AfterFunc(foldTime, func() {
		a.events <- func() {
			delete(a.closing, id)
			a.remove(id)
		}
	})
}

// fold aims the split holding pane at the other side, and reports
// whether pane was in a split.
func fold(b *Box, pane string) bool {
	switch {
	case b == nil || b.Pane != "":
		return false
	case b.A.Pane == pane:
		b.Share = 0
		return true
	case b.B.Pane == pane:
		b.Share = 1
		return true
	}
	return fold(b.A, pane) || fold(b.B, pane)
}

// remove takes a pane away at once.
func (a *app) remove(id string) {
	i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == id })
	if i < 0 {
		return
	}
	if p := a.st.Panes[i]; p.Kind == kindLog && p.Machine != "" {
		// Closing the log of a connection being made gives it up, as
		// in gridterm: it is where the dial is watched from.
		a.giveUp(p.Machine)
	}
	if sh := a.shells.get(id); sh != nil {
		sh.close()
	}
	a.shells.set(id, nil)
	if _, ok := a.st.Browsers[id]; ok {
		m := maps.Clone(a.st.Browsers)
		delete(m, id)
		a.st.Browsers = m
	}
	if _, ok := a.st.Readers[id]; ok {
		m := maps.Clone(a.st.Readers)
		delete(m, id)
		a.st.Readers = m
	}
	a.tunnelPaneGone(id)
	delete(a.commands, id)
	delete(a.argvs, id)
	delete(a.noticed, id)
	delete(a.paneAt, id)
	delete(a.paneFiles, id)
	delete(a.farHost, id)
	delete(a.typed, id)
	delete(a.reads, id)
	if a.agents.by[id] != nil {
		_ = a.unsharePane(id)
	}
	next := a.take(id)
	a.st.Panes = slices.Delete(a.st.Panes, i, i+1)
	if a.st.Focus == id {
		switch {
		case next != "":
			a.st.Focus = next
		case len(a.st.Panes) > 0:
			a.st.Focus = a.st.Panes[max(0, i-1)].ID
		default:
			a.st.Focus = ""
		}
	}
}

func (a *app) nextPane(back bool) {
	n := len(a.st.Panes)
	if n == 0 {
		return
	}
	i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == a.st.Focus })
	step := 1
	if back {
		step = n - 1
	}
	a.st.Focus = a.st.Panes[(i+step)%n].ID
}

// popOut moves the focused pane onto a stage of its own.
func (a *app) popOut() {
	id := a.st.Focus
	if b := a.groups[a.groupOf[id]]; b == nil || b.Pane == id {
		return
	}
	a.take(id)
	a.nextGroup++
	a.groups[a.nextGroup] = &Box{Pane: id}
	a.groupOf[id] = a.nextGroup
}

func (a *app) retitle(id, title string) {
	for i := range a.st.Panes {
		if p := &a.st.Panes[i]; p.ID == id && title != "" {
			p.shell = title
			if !p.Named {
				p.Title = title
			}
		}
	}
}

// defaultFontSize is the terminals' font size to begin with.
const defaultFontSize float32 = 15

// windowTopic is what the program publishes the window's state to.
const windowTopic = "window"

func itoa(n int) string { return strconv.Itoa(n) }

// pickTheme draws the window in the theme named: gunim fades its
// colours across, and each terminal takes the new palette, what is on
// its screen included.
func (a *app) pickTheme(name string) {
	for _, t := range a.themes {
		if t.name != name {
			continue
		}
		a.st.Theme = name
		a.palette = t.palette
		a.st.Marks = marksOf(t.palette)
		a.wantFont = t.source.Font
		a.useWantedFont()
		for _, sh := range a.shells.all() {
			sh.setPalette(t.palette)
		}
		_ = a.c.SetTheme(name)
	}
}
