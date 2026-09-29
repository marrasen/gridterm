// Package app is kakel's program side: what the windows show, as State,
// and what happens when they ask for something, as intents. It owns
// the panes' programs, the connections, the files and everything else
// the windows draw, and runs on one goroutine of its own.
package app

import (
	"context"
	"errors"
	"log"
	"maps"
	"sync/atomic"
	"time"

	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/tunnel"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/words"

	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/marrasen/kakel/glyph"
	"github.com/marrasen/kakel/jobs"
	"github.com/marrasen/kakel/keys"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/secrets"
	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
	"github.com/marrasen/kakel/vfs"
	"github.com/marrasen/kakel/vt"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/theme"
)

// The program side of the window. It owns the panes, how they are
// arranged, and which one has the keyboard, on a goroutine of its own.
// It hears what the window asks for as intents and, after each change,
// publishes the state the window shows.

// State is what the window shows.
type State struct {
	// Window numbers the window, among kakel's own, and Behind says
	// another is in front, which sends the echoes and asks for the
	// user's attention in its place.
	Window int
	Behind bool
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
	Marks look.Marks
	// FileClip is what the file clipboard holds, marked in the lists.
	FileClip FileClip
	// KeyFiles are the key files kept, newest first, offered when a
	// server is saved.
	KeyFiles []string
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
	Accounts []machines.ID
	// Secrets is what the vault holds, by name.
	Secrets Secrets
	// Share is the panes shared with an agent.
	Share Share
	// Serving is this window served to others.
	Serving Serving
	// Windows are the windows this one is connected to.
	Windows []RemoteWindow
	// Machines are what everything else names machines by, their IDs,
	// with the names to show: the saved servers and windows, and the
	// quick connections.
	Machines []machines.Info
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
	Connected []machines.ID
	Dialing   []machines.ID
	// Dropped are the machines whose connection went by itself, kept on
	// the sidebar, greyed, until cleared.
	Dropped     []machines.ID
	Shells      []ShellChoice
	ChosenShell string
	// ShellSetup says new shells here are taught to say what they are
	// doing, and TermProgram what they are told the terminal is called,
	// "" for kakel's own name.
	// Shortcuts are the changes the user's shortcuts file makes to the
	// window's keys, and ShortcutsRead counts its reads; ShortcutsAgain
	// says the last read was asked for, and the window says so once it
	// has taken the file. Contents are
	// the themes' colours for the panes, when the themes were read
	// again.
	// SavedCopies are the copies kept, newest first.
	SavedCopies   []settings.SavedCopy
	Shortcuts     []keys.Change
	ShortcutsRead uint64
	// See Shortcuts.
	ShortcutsAgain bool
	Contents       map[string]theme.Theme
	ShellSetup     bool
	TermProgram    string
	Bells          uint64
	// Pings counts what the window sends an echo out for, past its
	// edges.
	Pings        Pings
	SavedTunnels []settings.SavedTunnel
	Status       string
	// Notices are the latest notices, oldest first, for the window to
	// show each once.
	Notices []Notice
	// Output counts the times shells wrote, so the window copies their
	// screens once a frame however often they write.
	Output uint64
}

// Pings counts the echoes the window sends out past its edges, one
// count for each tone: Problems for failures, such as a connection
// dropped; Dones for work finished, such as a copy; and Calls for bells
// rung in panes out of sight. The window sends one each time a count
// goes up.
//
// FrontProblems and FrontDones count long commands that finished in the
// pane in front, failing or not. The window sends those only while
// another program has the keyboard, since otherwise the user is
// watching.
type Pings struct {
	Problems, Dones, Calls    uint64
	FrontProblems, FrontDones uint64
}

// commandLong is how long a command runs before its finish is worth an
// echo: a build or a copy, rather than an ls.
const commandLong = 3 * time.Second

// commandDone sends an echo for a long command that finished in pane
// id: green for exit 0, red for any other.
func (a *app) commandDone(id string, status int) {
	switch {
	case a.st.Focus == id && status == 0:
		a.st.Pings.FrontDones++
	case a.st.Focus == id:
		a.st.Pings.FrontProblems++
	case status == 0:
		a.done()
	default:
		a.problem()
	}
}

// problem and done have the window send an echo out for a failure, or
// for work finished.
func (a *app) problem() { a.st.Pings.Problems++ }
func (a *app) done()    { a.st.Pings.Dones++ }

// Notice is something to tell the user once, in a toast. Clipboard,
// when set, goes on the clipboard as it shows.
type Notice struct {
	ID          uint64
	Title, Body string
	// Kind says whether it tells of a failure, of work done, or of
	// neither.
	Kind      NoticeKind
	Clipboard string
	// Forget has the window take Clipboard back off the clipboard in
	// half a minute, unless something else was copied since.
	Forget bool
	// win is the window it shows in.
	win int
}

// NoticeKind is what a notice tells of, which picks its icon.
type NoticeKind uint8

// The kinds of notice.
const (
	NoticePlain NoticeKind = iota
	NoticeWorked
	NoticeFailed
)

// Pane is one pane, as the sidebar lists it.
type Pane struct {
	ID    string
	Title string
	// Machine is the server the pane's shell runs on, "" for this
	// computer.
	Machine machines.ID
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
	// half: to the right, or below with Vertical. The shell is on the
	// focused pane's machine, or on Machine with Elsewhere; Shell names
	// one of this computer's shells.
	SplitPane struct {
		Vertical  bool
		Machine   machines.ID
		Elsewhere bool
		Shell     string
	}
	// MovePane moves Pane out of wherever it is and into a split beside
	// Beside: to the right, or below with Vertical.
	MovePane struct {
		Pane, Beside string
		Vertical     bool
	}
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
	OpenOn struct{ Machine machines.ID }
	// FilesOn opens a file pane on Machine, at Path, or at home when
	// Path is empty.
	FilesOn struct {
		Machine machines.ID
		Path    string
	}
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
	// ConnectTo connects to a saved server by its ID, Server, or to one
	// typed as user@host:port, as a quick connection, and opens a shell
	// there. Connected already, it opens another shell. As is the ID of
	// the quick connection a typed one is made again for.
	ConnectTo struct {
		Target     string
		Server, As machines.ID
	}
	// SaveServer saves a server, in place of the one named Under when
	// that is set.
	SaveServer struct {
		Host  remote.Host
		Under string
	}
	// RemoveServer forgets a saved server.
	RemoveServer struct{ ID machines.ID }
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
	// c is the first window's client.
	c gunim.Client
	// wins are kakel's windows, cur the one in front, nextWin numbers
	// them, and winOf is the window each pane is in. intents carries
	// what each window asks for. openWindow opens another, and opening
	// counts those on their way.
	wins       []*ownWin
	cur        *ownWin
	nextWin    int
	winOf      map[string]int
	intents    chan windowIn
	openWindow WindowOpener
	opening    int
	// linksAt is where each pane with links runs, for its paths, which
	// its terminal looks up on a goroutine of its own.
	linksAt map[string]*atomic.Pointer[machines.ID]
	// notRun are the command panes whose connection was not made when
	// they were asked to run again, for their question to say so.
	notRun map[string]bool
	// farLogs are the logs of machines beyond windows on their way here,
	// so a second ask waits for the first rather than opening another.
	farLogs map[machines.ID]bool
	// starting counts the panes on their way, a shell on a server being
	// started, which keep an empty window open for them; stayEmpty keeps
	// it open with none, as when the first pane could not be opened,
	// until one is.
	starting  int
	stayEmpty bool
	shells    *screen.Shells
	st        State
	// groups holds each group's arrangement, and groupOf each pane's
	// group.
	groups  map[int]*Box
	groupOf map[string]int
	// next numbers the panes, and nextGroup the groups.
	next      int
	nextGroup int
	// splits numbers the splits made, for their ids.
	splits int
	// notices counts the notices made.
	notices uint64
	// ctx ends with the window. machines is every machine known and
	// what is kept on its connection, ring holds the keys unlocked so
	// far, and book is the saved servers.
	ctx      context.Context
	machines *machines.Registry
	ring     *remote.Ring
	book     *remote.Book
	// replies waits for the answers to asks, by ID, and askIDs counts
	// them.
	replies map[uint64]chan AskAnswered
	// closing holds the panes folding away.
	closing map[string]bool
	// local is this computer's filesystem, once a file pane needs it.
	local vfs.FS
	// themes are the themes on offer, and palette the terminals' now.
	// settings is kakel's settings file, which keeps the theme
	// picked.
	themes   []look.Themed
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
	tunnels map[string]*tunnel.Held
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
	// leaving is set while the window asks whether to close, and
	// watchingVault while an open secrets pane reads the vault again.
	leaving       bool
	watchingVault bool
	// copied is the last secret put on the clipboard, and copiedAt when.
	copied   string
	copiedAt time.Time
	// opts are what the command line asked for; fixedFont is a family
	// -font-family named, and shotErr why a -shot script gave up.
	opts      Options
	fixedFont string
	shotErr   error
	// gone says the window is on its way out, leaving with what it
	// shows; its panes close once it has gone.
	gone bool
	// paneFiles is each file pane's view of its machine's files.
	paneFiles map[string]wrappedFiles
	// paneAt is the address each pane on a server was opened at, to say
	// so when a pane is reconnected somewhere else.
	paneAt map[string]string
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
	// fontFixed says the command line named the face, which no theme
	// overrules.
	fontFixed bool
	// themeTrouble is what went wrong reading the themes as the window
	// opened, said once it is up.
	themeTrouble error
	// checking says a check for a newer release is on its way.
	checking bool
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
	// restarts and endings count each pane's starts again and its ends,
	// for a window that asked for one to hear how it went.
	restarts map[string]int
	endings  map[string]int
	// reads are what each reader pane reads, to read it again, and
	// following the readers with a follow loop running.
	reads     map[string]readSpec
	following map[string]bool
	// nextShell is the command the next terminal here starts, once.
	nextShell []string
	// found are the shells on this machine.
	found []shellfind.Shell
	// registerThemes names themes to the window, for reading them
	// again.
	registerThemes func([]look.Themed)
	tunnelSeq      int
	ticking        bool
	quiet          bool
	// wake hears that a shell wrote, and events carries changes from
	// the shells' goroutines to this one.
	wake   chan struct{}
	events chan func()
}

func newApp(c gunim.Client, sh *screen.Shells) *app {
	a := &app{
		c:         c,
		shells:    sh,
		st:        State{Sidebar: true, SidebarWidth: 220, FontSize: defaultFontSize, Fonts: []string{bundledFamily, dosFamily}},
		groups:    map[int]*Box{},
		groupOf:   map[string]int{},
		ring:      remote.NewRing(),
		replies:   map[uint64]chan AskAnswered{},
		closing:   map[string]bool{},
		tunnels:   map[string]*tunnel.Held{},
		agents:    agents{by: map[string]*handover{}},
		commands:  map[string]command{},
		noticed:   map[string]uint64{},
		paneFiles: map[string]wrappedFiles{},
		paneAt:    map[string]string{},
		farLogs:   map[machines.ID]bool{},
		notRun:    map[string]bool{},
		linksAt:   map[string]*atomic.Pointer[machines.ID]{},
		argvs:     map[string][]string{},
		farHost:   map[string]string{},
		typed:     map[string]*typedLog{},
		reads:     map[string]readSpec{},
		following: map[string]bool{},
		restarts:  map[string]int{},
		endings:   map[string]int{},
		far:       pathsFar{known: map[string]farPath{}, asking: map[string]bool{}},
		wake:      make(chan struct{}, 1),
		events:    make(chan func(), 64),
		winOf:     map[string]int{},
		intents:   make(chan windowIn, 64),
	}
	a.machines = machines.New(func() *remote.Book { return a.book })
	a.addWindow(c, nil)
	return a
}

// run serves the window until it closes or ctx ends.
func (a *app) run(ctx context.Context) error {
	a.ctx = ctx
	a.palette = vt.DefaultPalette()
	for _, t := range a.themes {
		a.st.Themes = append(a.st.Themes, t.Name)
	}
	a.loadSettings()
	// The theme picked last time, or the first.
	if len(a.themes) > 0 {
		name := a.themes[0].Name
		if a.settings != nil {
			if picked, ok := a.settings.Theme(); ok {
				if slices.ContainsFunc(a.themes, func(t look.Themed) bool { return t.Name == picked }) {
					name = picked
				} else {
					// Said rather than swapped quietly: a window in another
					// theme with no word reads as one that forgot.
					log.Printf("the theme %q is not in the list any more, so this window is %q", picked, name)
				}
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
		a.failed("Couldn't read the shortcuts file", err.Error())
	}
	if a.settings != nil && a.settings.ServeOn() {
		go a.offerToServeAgain()
	}
	a.loadBook()
	if a.themeTrouble != nil {
		a.failed("Couldn't read all the themes", a.themeTrouble.Error())
	}
	if err := a.applyOptions(); err != nil {
		return err
	}
	a.openFirstOrSay()
	a.publish()
	if a.opts.shot != "" {
		list, err := parseShot(a.opts.shot)
		if err != nil {
			return fmt.Errorf("-shot: %w", err)
		}
		go a.runShot(list)
	}
	for _, w := range a.wins {
		a.serveWin(w)
	}
	for {
		select {
		case <-ctx.Done():
			a.takeSecretBack()
			a.hangUp()
			return nil
		case in := <-a.intents:
			if in.closed {
				a.windowClosed(in.w)
				if len(a.wins) == 0 {
					a.closeAll()
					return a.c.Err()
				}
				break
			}
			if a.gone || in.w.gone {
				// On its way out: nothing more is done.
				continue
			}
			a.front(in.w)
			a.handle(in.env.Intent)
		case <-a.wake:
			a.st.Output++
		case f := <-a.events:
			f()
		}
		// Empty, and connecting to nothing that would open a pane: the
		// window leaves, and the last one takes the program with it.
		if a.emptyAndIdle() {
			a.leave()
		}
		a.leaveEmpty()
		if a.gone {
			continue
		}
		if a.quiet {
			a.quiet = false
			continue
		}
		if a.kindOfPane(a.st.Focus) == KindTerminal {
			a.lastTerminal = a.st.Focus
		}
		a.setPane(a.st.Focus, func(p *Pane) { p.Rang = false })
		a.publish()
	}
}

// loadSettings reads kakel's settings, and takes what they keep.
func (a *app) loadSettings() {
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
			a.st.KeyFiles = s.Keys()
			if size, ok := s.FontSize(); ok && !a.opts.sizeSet {
				a.st.FontSize = min(max(float32(size), 8), 40)
			}
		} else {
			a.unreadable("the settings", path+" is repaired or removed, and kakel is started again", err)
		}
	}
}

// loadBook reads the saved servers.
func (a *app) loadBook() {
	if path, err := remote.BookPath(); err == nil {
		if b, err := remote.LoadBook(path); err == nil {
			a.book = b
			a.st.Saved = b.Hosts()
			a.giveSavedIDs()
		} else {
			a.unreadable("the server list", path+" is repaired or removed, and the list read again", err)
		}
	}
}

// unreadable says that what, a file of kakel's, could not be read, and
// that nothing is written to it until what until says: a file kakel
// cannot read is not one to write over.
func (a *app) unreadable(what, until string, err error) {
	a.failed("Couldn't read "+what, err.Error()+"\n\nNothing changed is kept until "+until+".")
}

// keep says when something could not be kept for next time.
func (a *app) keep(what string, err error) {
	if err != nil {
		a.failed("Couldn't keep "+what+" for next time", err.Error())
	}
}

// failedTitle heads what is said when in could not be done, with what
// was being done: "Couldn't change the theme" says more than that
// something did not work.
func failedTitle(in gunim.Intent) string {
	switch in.(type) {
	case NewTerminal, OpenOn, OpenShellNamed, OpenDefaultShell:
		return "Couldn't open a terminal"
	case SplitPane:
		return "Couldn't split the pane"
	case PickTheme:
		return "Couldn't change the theme"
	case PickFont:
		return "Couldn't change the font"
	case ConnectTo:
		return "Couldn't connect"
	case ConnectWindow, AttachWindow:
		return "Couldn't connect to the window"
	case Disconnect, DisconnectWindow:
		return "Couldn't disconnect"
	case OpenFiles, FilesOn:
		return "Couldn't open the files"
	case PasteFiles:
		return "Couldn't paste the files"
	case DropFiles:
		return "Couldn't take the files dropped"
	case PasteImage, PastePicture:
		return "Couldn't paste the picture"
	case SaveServer:
		return "Couldn't save the server"
	case RemoveServer:
		return "Couldn't remove the server"
	case OpenTunnel, OpenSavedTunnel:
		return "Couldn't open the tunnel"
	case CloseTunnel:
		return "Couldn't close the tunnel"
	case RunCommand, RunSavedCommand:
		return "Couldn't run the command"
	case StartServing:
		return "Couldn't serve this window"
	case StopServing:
		return "Couldn't stop serving this window"
	case SharePane, UnsharePane, StopSharing:
		return "Couldn't change what is shared"
	case WriteSkill:
		return "Couldn't write the skill"
	case ShowScrollback:
		return "Couldn't show the scrollback"
	case MakeKey:
		return "Couldn't make the key"
	case RunSavedCopy, RepeatJob:
		return "Couldn't copy"
	}
	return "That didn't work"
}

// stayIfEmpty keeps an empty window open, for what went wrong opening
// its pane to be read: the log that was its one pane closed as the
// connection was made.
func (a *app) stayIfEmpty() {
	if len(a.st.Panes) == 0 {
		a.stayEmpty = true
	}
}

// openFirstOrSay opens the first pane. One that cannot be opened
// leaves the window there, saying why, for a pane to be opened another
// way: started from a desktop icon, a program that closed at once would
// say nothing at all.
func (a *app) openFirstOrSay() {
	if err := a.openFirst(); err != nil {
		a.failed("Couldn't open the first pane", err.Error())
		a.stayEmpty = true
	}
}

// emptyAndIdle reports whether the program has no pane and none on its
// way, and so leaves: nothing connecting, no window or shell opening,
// and not kept open, as after the first pane failed.
func (a *app) emptyAndIdle() bool {
	return len(a.st.Panes) == 0 && len(a.machines.Dialing()) == 0 && a.opening == 0 && a.starting == 0 && !a.stayEmpty
}

func (a *app) publish() {
	a.forgetUnused()
	a.st.Machines = a.machines.Infos()
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
	st.Connected = a.machines.Connected()
	st.Dialing = a.machines.Dialing()
	st.Dropped = a.machines.Dropped()
	a.tellServed()
	a.tellWindowsTunnels()
	st.SavedTunnels = slices.Clone(a.st.SavedTunnels)
	st.Stage = a.groups[a.groupOf[a.st.Focus]].clone()
	st.Secrets.Waiting = a.waitingForSecret()
	for _, w := range a.wins {
		if !w.gone {
			_ = w.c.Publish(WindowTopic, a.stateFor(w, st))
		}
	}
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
		err = a.split(in)
	case MovePane:
		a.movePane(in)
	case ClosePane:
		id := in.Pane
		if id == "" {
			id = a.st.Focus
		}
		a.closePane(id)
	case FocusPane:
		if a.has(in.Pane) {
			a.focus(in.Pane)
		}
	case WindowFocused:
		// In front already, as it asked.
	case CloseWindow:
		a.closeWindow(a.cur)
	case PaneToWindow:
		a.moveToWindow(in.Pane, a.cur)
	case PaneToNewWindow:
		a.paneToNewWindow(in)
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
		// Written down once it is on: a theme not in the list is not
		// one to come back to.
		if !a.pickTheme(in.Name) {
			err = fmt.Errorf("there is no theme called %q", in.Name)
			break
		}
		if a.settings != nil {
			if err := a.settings.PutTheme(in.Name); err != nil {
				a.failed("Couldn't keep the theme for next time", err.Error())
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
				a.failed("Couldn't keep the font size for next time", err.Error())
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
		err = a.removeServer(in.ID)
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
		a.removeSecrets("Couldn't remove the secret", []string{in.ID})
	case RemoveSecrets:
		a.removeSecrets("Couldn't remove the secrets", in.IDs)
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
		a.st.Serving.Tries++
	case StopServing:
		err = a.stopServing()
	case DisconnectClients:
		err = a.disconnectClients()
	case DisconnectClient:
		err = a.disconnectClient(in)
	case ClearMachine:
		a.clearMachine(in.ID)
	case ConnectWindow:
		err = a.connectWindow(in)
	case DisconnectWindow:
		err = a.disconnectWindow(in.ID)
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
	case DropJob:
		a.running = slices.DeleteFunc(a.running, func(r *running) bool { return r.id == in.ID && r.ended })
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
		err = a.open("", Placement{})
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
			err = a.open("", Placement{})
		}
	case OpenOn:
		err = a.open(in.Machine, Placement{})
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
	case NoTextToPaste:
		a.noTextToPaste()
	case MakePortable:
		a.makePortable()
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
		if spec, ok := a.reads[in.Pane]; ok {
			if in.Text {
				spec.text = true
				a.reads[in.Pane] = spec
			}
			a.readOnce(in.Pane)
		} else if r, ok := a.st.Readers[in.Pane]; ok {
			// A scrollback is read off its pane again, as it stands now,
			// while the pane is there to read.
			if t := a.terminal(r.Of); t != nil {
				r.Lines = scrollbackText(t)
			}
			r.Seq++
			a.setReader(in.Pane, r)
		}
	case FollowFile:
		a.followReader(in.Pane, in.On)
	case SaveLines:
		a.saveLines(in)
	case DropFileClip:
		a.clip = nil
	case ListFolders:
		a.listFolders(in)
	case AskAction:
		err = a.askAction(in)
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
				a.failed("Couldn't keep the pane titles for next time", err.Error())
			}
		}
	case DialogClosed:
	}
	if err != nil {
		a.failed(failedTitle(in), err.Error())
	}
}

// notify tells the user something, once, in a toast.
func (a *app) notify(title, body, clip string) { a.notice(NoticePlain, title, body, clip) }

// worked tells the user that something they asked for is done.
func (a *app) worked(title, body, clip string) { a.notice(NoticeWorked, title, body, clip) }

// failed tells the user that something went wrong, and why.
func (a *app) failed(title, why string) { a.notice(NoticeFailed, title, why, "") }

// notice tells the user something, once, in a toast of its kind.
func (a *app) notice(kind NoticeKind, title, body, clip string) {
	// Into the Window Log too, where it stays once the toast has gone:
	// a failure is read again there, or copied.
	if body != "" {
		log.Printf("%s: %s", title, body)
	} else {
		log.Print(title)
	}
	a.notices++
	a.st.Notices = append(a.st.Notices, Notice{ID: a.notices, Title: title, Body: body, Kind: kind, Clipboard: clip, win: a.frontID()})
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

// Placement says where a new pane goes: on a stage of its own, or
// beside a pane, below it with vertical.
type Placement struct {
	Beside   string
	Vertical bool
}

// hooks are what a pane's shell tells the program.
func (a *app) hooks(id string) screen.Hooks {
	return screen.Hooks{
		Output: func() {
			select {
			case a.wake <- struct{}{}:
			default:
			}
		},
		Title: func(t string) { a.events <- func() { a.retitle(id, t) } },
		Exit:  func() { a.events <- func() { a.paneEnded(id) } },
		Bell: func() {
			a.events <- func() {
				a.st.Bells++
				if w := a.ownerOf(id); w == nil || a.focusIn(w) != id {
					a.setPane(id, func(p *Pane) { p.Rang = true })
					a.st.Pings.Calls++
				}
			}
		},
		CommandDone: func(status int, ok bool, took time.Duration) {
			if !ok || took < commandLong {
				return
			}
			a.events <- func() { a.commandDone(id, status) }
		},
		Clipboard: func(s string) {
			a.events <- func() {
				a.worked("Copied to the clipboard", fmt.Sprintf("%d characters, from %s", utf8.RuneCountInString(s), a.titleOf(id)), s)
			}
		},
	}
}

// open opens a shell on machine, "" for this one, as a new pane placed
// at at. A remote shell opens over the machine's connection in the
// background, and its pane arrives once it has.
func (a *app) open(machine machines.ID, at Placement) error { return a.openThen(machine, at, nil) }

// openThen is open, telling then the pane it opened, or why it could
// not, once it has. then runs on the program's goroutine, and may be
// nil.
func (a *app) openThen(machine machines.ID, at Placement, then func(id string, err error)) error {
	if then == nil {
		then = func(string, error) {}
	}
	if window, key, far := machine.Far(); far {
		// Beyond a window: that window opens it, on its connection.
		a.next++
		id := "p" + strconv.Itoa(a.next)
		return a.openThrough(window, key, command{}, id, fmt.Sprintf("Terminal %d", a.next), at, then)
	}
	if machine != "" && a.machines.Get(machine).Conn == nil && a.machines.Get(machine).Window == nil {
		// Not connected: connected to first, as a saved server's plus
		// in the sidebar does.
		return a.dialAgain(machine, func(err error) {
			if err != nil {
				then("", err)
				return
			}
			if a.machines.Get(machine).Conn == nil && a.machines.Get(machine).Window == nil {
				// Connected, but by another name than this one: said,
				// rather than connected to again and again.
				err := errors.New("the connection was made under another name. Open a terminal on it from the sidebar")
				a.failed("Couldn't open a shell on "+a.machines.Name(machine), words.UpperFirst(err.Error())+".")
				then("", err)
				return
			}
			if err := a.openThen(machine, at, then); err != nil {
				a.failed("Couldn't open a shell on "+a.machines.Name(machine), err.Error())
				a.problem()
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
		sess, err := a.startLocalSession(argv, a.dirHere(), screen.Cols, screen.Rows, true)
		if err != nil {
			return fmt.Errorf("kakel: start the shell: %w", err)
		}
		sh := screen.Open(sess, a.palette, a.withLinks(a.hooks(id), id, ""))
		a.argvs[id] = withoutFolder(argv)
		a.addPane(Pane{ID: id, Title: title}, sh, at)
		then(id, nil)
		return nil
	}
	if a.machines.Get(machine).Window != nil {
		return a.openThrough(machine, "", command{}, id, title, at, then)
	}
	conn, ok, err := a.connOf(machine)
	switch {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("kakel: %s is not connected", a.machines.Name(machine))
	}
	a.starting++
	go func() {
		sess, err := conn.Shell(a.ctx, a.shellConfig(machine, screen.Cols, screen.Rows))
		a.events <- func() {
			a.starting--
			if err != nil {
				a.failed("Couldn't open a shell on "+a.machines.Name(machine), err.Error())
				a.problem()
				a.stayIfEmpty()
				then("", err)
				return
			}
			a.teachFar(machine, sess)
			a.paneAt[id] = a.machines.Get(machine).Reached
			a.addPane(Pane{ID: id, Title: title, Machine: machine}, screen.Open(sess, a.palette, a.withLinks(a.hooks(id), id, machine)), at)
			then(id, nil)
		}
	}()
	return nil
}

// addPane shows a new pane, with the keyboard: beside at.beside while
// that pane is still open, and otherwise on a stage of its own.
func (a *app) addPane(p Pane, sh *screen.Shell, at Placement) {
	if sh != nil {
		a.shells.Set(p.ID, sh)
	}
	// Into the window of the pane it goes beside, or the one in front.
	if w := a.ownerOf(at.Beside); w != nil && !w.gone {
		a.front(w)
	}
	a.st.Panes = append(a.st.Panes, p)
	a.stayEmpty = false
	a.winOf[p.ID] = a.frontID()
	a.place(p.ID, at)
	a.st.Focus = p.ID
}

// place puts a pane in no group yet where at says: in a split beside
// another, or in a group of its own.
func (a *app) place(id string, at Placement) {
	a.nextGroup++
	g, ok := a.groupOf[at.Beside]
	if at.Beside == "" || !ok {
		g = a.nextGroup
		a.groups[g] = &Box{Pane: id}
	} else {
		a.splits++
		box := &Box{
			ID: "s" + strconv.Itoa(a.splits), Vertical: at.Vertical, Share: 0.5, Opening: true,
			A: &Box{Pane: at.Beside}, B: &Box{Pane: id},
		}
		a.groups[g] = a.groups[g].replace(at.Beside, box)
	}
	a.groupOf[id] = g
}

// machineOf returns the machine a pane is on, "" for this one.
func (a *app) machineOf(id string) machines.ID {
	for _, p := range a.st.Panes {
		if p.ID == id {
			return p.Machine
		}
	}
	return ""
}

// openTerminal opens a shell where the focused pane is, on a stage of
// its own: on this computer, the one the focused pane runs.
func (a *app) openTerminal() error {
	a.likeHere()
	return a.open(a.filesKey(a.st.Focus), Placement{})
}

// split opens a shell beside the focused pane, on its machine.
func (a *app) split(in SplitPane) error {
	machine := a.filesKey(a.st.Focus)
	if in.Elsewhere {
		machine = in.Machine
	}
	if in.Shell != "" {
		argv := a.shellCommand(in.Shell)
		if argv == nil {
			return fmt.Errorf("this machine has no shell called %q", in.Shell)
		}
		a.nextShell, machine = argv, ""
	} else if machine == a.filesKey(a.st.Focus) {
		a.likeHere()
	}
	return a.open(machine, Placement{Beside: a.st.Focus, Vertical: in.Vertical})
}

// movePane moves a pane that is open into a split beside another, as
// Split Right and Split Down can: the way to two file panes
// side by side.
func (a *app) movePane(in MovePane) {
	if in.Pane == in.Beside || !a.has(in.Pane) || !a.has(in.Beside) {
		return
	}
	from, to := a.ownerOf(in.Pane), a.ownerOf(in.Beside)
	i := slices.IndexFunc(a.st.Panes, func(p Pane) bool { return p.ID == in.Pane })
	next := a.take(in.Pane)
	if from != to {
		a.winOf[in.Pane] = to.id
		a.refocus(from, in.Pane, next, i)
	}
	a.place(in.Pane, Placement{Beside: in.Beside, Vertical: in.Vertical})
	a.focus(in.Pane)
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
	if w := a.ownerOf(id); w != nil && a.focusIn(w) == id {
		if next := a.groups[g].beside(id); next != "" {
			a.setFocusIn(w, next)
		}
	}
	if sh := a.shells.Get(id); sh != nil {
		sh.Close()
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
	if p := a.st.Panes[i]; p.Kind == KindLog && p.Machine != "" && p.On == "" {
		// Closing the log of a connection being made gives it up: it is
		// where the dial is watched from. One beyond a window is only
		// read.
		a.giveUp(p.Machine)
	}
	if sh := a.shells.Get(id); sh != nil {
		sh.Close()
	}
	a.shells.Set(id, nil)
	delete(a.linksAt, id)
	delete(a.notRun, id)
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
	delete(a.restarts, id)
	delete(a.endings, id)
	// A scrollback of it has nothing left to read again, and says so.
	for rid, r := range a.st.Readers {
		if r.Of == id && rid != id {
			r.Gone = "closed"
			a.setReader(rid, r)
		}
	}
	if a.agents.by[id] != nil {
		_ = a.unsharePane(id)
	}
	next := a.take(id)
	a.refocus(a.ownerOf(id), id, next, i)
	a.st.Panes = slices.Delete(a.st.Panes, i, i+1)
	delete(a.winOf, id)
}

func (a *app) nextPane(back bool) {
	panes := a.panesIn(a.cur)
	n := len(panes)
	if n == 0 {
		return
	}
	i := slices.IndexFunc(panes, func(p Pane) bool { return p.ID == a.st.Focus })
	step := 1
	if back {
		step = n - 1
	}
	a.st.Focus = panes[(i+step)%n].ID
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
	title = a.shellTitle(id, title)
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

// WindowTopic is what the program publishes the window's state to.
const WindowTopic = "window"

func itoa(n int) string { return strconv.Itoa(n) }

// pickTheme draws the window in the theme named: gunim fades its
// colours across, and each terminal takes the new palette, what is on
// its screen included.
func (a *app) pickTheme(name string) bool {
	for _, t := range a.themes {
		if t.Name != name {
			continue
		}
		if name != a.st.Theme {
			// Another theme: its wish for a typeface stands again, over
			// one picked by hand for the theme before.
			a.fontPicked = a.fontFixed
		}
		a.st.Theme = name
		a.palette = t.Palette
		a.st.Marks = look.MarksOf(t.Palette)
		a.wantFont = t.Source.Font
		a.useWantedFont()
		for _, sh := range a.shells.All() {
			sh.SetPalette(t.Palette)
		}
		for _, w := range a.wins {
			_ = w.c.SetTheme(name)
		}
		return true
	}
	return false
}

// Config is what the program side starts with: its first window, the
// shells the windows draw, a way to open more windows, the command
// line, and the themes on offer, with what went wrong reading them.
type Config struct {
	Client         gunim.Client
	Window         *gunim.Window
	Shells         *screen.Shells
	OpenWindow     WindowOpener
	Options        Options
	Themes         []look.Themed
	ThemeTrouble   error
	RegisterThemes func([]look.Themed)
}

// Start runs the program side until its last window closes.
func Start(ctx context.Context, cfg Config) error {
	a := newApp(cfg.Client, cfg.Shells)
	a.wins[0].gw = cfg.Window
	a.openWindow = cfg.OpenWindow
	a.opts = cfg.Options
	a.themes = cfg.Themes
	a.themeTrouble = cfg.ThemeTrouble
	a.registerThemes = cfg.RegisterThemes
	defer closeToaster()
	return errors.Join(a.run(ctx), a.shotErr)
}
