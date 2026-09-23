// Command gridterm is a GPU-rendered terminal emulator.
//
// It runs a shell either on a local pseudo-terminal — a PTY on Unix, a
// ConPTY on Windows — or on another machine over SSH, and draws it as a
// widget in the text-mode toolkit under ui.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	_ "embed"
	"github.com/hajimehoshi/ebiten/v2"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/mcp"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// dosFontTTF is the IBM VGA 8x16 character set, compiled in so a theme
// can ask for it on a machine that has no such font installed. See
// fonts/README.md for where it came from and what it may be used for.
//
//go:embed fonts/PxPlus_IBM_VGA8.ttf
var dosFontTTF []byte

// bundledFonts returns the Go Mono faces compiled into the binary.
func bundledFonts() glyph.Fonts {
	return glyph.Fonts{
		Regular:    gomono.TTF,
		Bold:       gomonobold.TTF,
		Italic:     gomonoitalic.TTF,
		BoldItalic: gomonobolditalic.TTF,
	}
}

// dosFonts returns the bundled DOS face. It has the one style, so the
// atlas draws bold and italic from it too.
func dosFonts() glyph.Fonts { return glyph.Fonts{Regular: dosFontTTF} }

// loadFonts reads the comma-separated font files given to -font, in the
// order regular, bold, italic, bold italic. A trailing style may be left
// out and an inner one left empty, which makes the atlas borrow a style
// it does have.
func loadFonts(list string) (glyph.Fonts, error) {
	var fonts glyph.Fonts
	into := []*[]byte{&fonts.Regular, &fonts.Bold, &fonts.Italic, &fonts.BoldItalic}
	paths := strings.Split(list, ",")
	if len(paths) > len(into) {
		return glyph.Fonts{}, fmt.Errorf("at most %d files, got %d", len(into), len(paths))
	}
	for i, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return glyph.Fonts{}, err
		}
		*into[i] = b
	}
	if fonts.Regular == nil {
		return glyph.Fonts{}, fmt.Errorf("the first file is the regular font and is required")
	}
	return fonts, nil
}

func main() {
	var (
		fontSize = flag.Float64("font-size", defaultFontSize, "font size in points")
		cmdline  = flag.String("e", "",
			"run this command instead of the login shell; split on spaces, no quoting")
		scroll    = flag.Int("scrollback", vt.DefaultScrollback, "lines of history to keep")
		sshTarget = flag.String("ssh", "",
			"connect to [user@]host[:port] over SSH instead of running a local shell")
		fontFiles = flag.String("font", "",
			"font files to use instead of the bundled Go Mono, comma separated,"+
				" in the order regular,bold,italic,bold-italic")
		fontFamily = flag.String("font-family", "",
			"use this installed monospace family instead of the bundled Go Mono")
		listFonts = flag.Bool("list-fonts", false,
			"print the installed monospace families and exit")
		mcpSkill = flag.Bool("mcp-skill", false,
			"print the skill that tells an agent how to work in a pane handed over in"+
				" gridterm, and exit; the hand-over dialog writes it for you, or save it"+
				" yourself as ~/.claude/skills/gridterm/SKILL.md")
		asMCP = flag.Bool("mcp", false,
			"serve this machine's gridterm panes to an agent over the Model Context"+
				" Protocol, on standard input and output, instead of opening a window;"+
				" it reaches nothing until the user gives it a session code")
		showStats = flag.Bool("stats", false,
			"say how long each frame is taking, once a second, on standard error;"+
				" for working out why a window feels slow")
		shotScript = flag.String("shot", "",
			"drive the window through a script and write PNGs, then exit;"+
				" steps are wait:<ms> until:<text> key:<chord> type:<text>"+
				" shot:<file>, and until: is the one to reach for, e.g."+
				` "until:$ type:make key:Enter until:done shot:built.png"`)
	)
	flag.Parse()

	// Kept as well as written to stderr, from here on: a window started
	// from Explorer has no console for stderr to reach, and "Show what
	// the window has logged" is the only way to read these.
	keepLog()

	if *asMCP {
		// No window, and nothing on standard output but the protocol:
		// whatever started this is reading it.
		panes := mcp.NewWindow()
		err := mcp.Serve(context.Background(), os.Stdin, os.Stdout, panes)
		if err := errors.Join(err, panes.Close()); err != nil {
			log.Fatalf("-mcp: %v", err)
		}
		return
	}

	if *mcpSkill {
		if err := printSkill(os.Stdout); err != nil {
			log.Fatalf("-mcp-skill: %v", err)
		}
		return
	}

	if *listFonts {
		if err := printFonts(os.Stdout); err != nil {
			log.Fatalf("-list-fonts: %v", err)
		}
		return
	}

	fonts, family, err := chooseFonts(*fontFiles, *fontFamily)
	if err != nil {
		log.Fatal(err)
	}
	atlas, err := glyph.NewAtlas(fonts, *fontSize, 96)
	if err != nil {
		log.Fatalf("build glyph atlas: %v", err)
	}

	a, err := openWindow(wanted{
		atlas:      atlas,
		fontSize:   *fontSize,
		sizeFixed:  named("font-size"),
		fontFamily: family,
		fontFixed:  *fontFiles != "" || *fontFamily != "",
		command:    strings.Fields(*cmdline),
		scrollback: *scroll,
		ssh:        *sshTarget,
		stats:      *showStats,
		shot:       *shotScript,
		scan:       true,
	})
	if err != nil {
		log.Fatal(err)
	}
	a.sizeTheWindow(atlas.Metrics())

	// A clean quit is ebiten.Termination rather than nil, so the shells
	// have to be closed into a separate error or every failure to shut
	// one down is dropped on the path the user actually takes.
	err = ebiten.RunGame(a)
	if errors.Is(err, ebiten.Termination) {
		err = nil
	}
	if err := a.shutDown(err); err != nil {
		log.Fatal(err)
	}
}

// shutDown closes everything the window is still holding and returns
// what is left to report.
//
// ran is what the game loop ended with, and comes back with the rest. A
// file session's goodbye that went unanswered is logged rather than
// returned: the window is going either way, a dead link runs that bound
// out on its own, and there is nothing in it for the user to act on.
func (a *app) shutDown(ran error) error {
	// Every connection still being made, and every dialog waiting for an
	// answer, is let go of here rather than left holding a goroutine.
	a.stop()
	closed := []error{ran}
	// The listener first, so a window that has taken this one over is
	// hung up on cleanly rather than finding the socket reset under it
	// as the process goes.
	closed = append(closed, a.stopServing())
	for t := range a.panes {
		closed = append(closed, t.Close())
	}
	// After the panes, so a shell gets its polite hangup before the
	// connection carrying it goes away underneath it.
	// The tunnels before the connections that carry them, so a port
	// that could not be let go of is reported as its own failure.
	// The file work first: a job holding a file open would keep the
	// connection it runs over from closing cleanly.
	a.queue.CancelAll()
	a.queue.WaitFor(jobsGrace)
	// Then the filesystems waiting on those jobs, which are closed on
	// goroutines of their own: one still waiting when the process ends
	// is one never closed.
	closed = append(closed, a.closes.waitFor(jobsGrace)...)
	closed = append(closed, a.closeTunnels(), a.closeMachines())
	// The windows this one took over go last: their panes were closed
	// with the rest, and this hangs up on what carried them. The port
	// agents reach this window on goes with them: there is nothing left
	// to hand over.
	closed = append(closed, a.closeWindows(), a.agents.stop())
	// And the notification area, so no icon is left behind sitting
	// there until something hovers over it.
	if a.toasts != nil {
		closed = append(closed, a.toasts.Close())
	}
	// A screenshot script that gave up is a failed run, so whatever
	// started it is told rather than left reading the pictures from
	// last time as if they were this run's.
	if a.shot != nil {
		closed = append(closed, a.shot.failed)
	}
	return a.graceLogged(errors.Join(closed...))
}

// chooseFonts resolves the typeface flags into what the window starts
// with, and the family name that names it.
//
// What it returns is only the starting typeface. The bundled faces are
// read separately and never come from here, because "back to the bundled
// font" has to mean the bundled font whatever a flag said.
func chooseFonts(files, family string) (glyph.Fonts, string, error) {
	switch {
	case files != "" && family != "":
		return glyph.Fonts{}, "", fmt.Errorf(
			"-font and -font-family both name a typeface; use one")
	case files != "":
		fonts, err := loadFonts(files)
		if err != nil {
			return glyph.Fonts{}, "", fmt.Errorf("-font: %w", err)
		}
		return fonts, "", nil
	case family != "":
		fonts, name, err := loadFamily(family)
		if err != nil {
			return glyph.Fonts{}, "", fmt.Errorf("-font-family: %w", err)
		}
		// The family's own spelling rather than what was typed, so
		// choosing it again from the menu is recognised as no change.
		return fonts, name, nil
	}
	return bundledFonts(), "", nil
}

// printFonts writes the installed monospace families, with the styles
// each has, for -list-fonts.
//
// A failure to read the font directories is reported rather than printed
// as an empty list, which would say the machine has no fonts.
func printFonts(w io.Writer) error {
	families, err := glyph.Monospaced()
	if err != nil {
		return err
	}
	if len(families) == 0 {
		_, _ = fmt.Fprintln(w, "no monospace font families found")
		return nil
	}
	names := map[glyph.Style]string{
		glyph.Regular:    "regular",
		glyph.Bold:       "bold",
		glyph.Italic:     "italic",
		glyph.BoldItalic: "bold-italic",
	}
	for _, family := range families {
		styles := make([]string, 0, 4)
		for _, s := range family.Styles() {
			styles = append(styles, names[s])
		}
		_, _ = fmt.Fprintf(w, "%-34s %s\n", family.Name, strings.Join(styles, ", "))
	}
	return nil
}

// loadFamily reads an installed family by name, ignoring case, and
// returns the family's own spelling of that name alongside it.
func loadFamily(name string) (glyph.Fonts, string, error) {
	families, err := glyph.Monospaced()
	if err != nil {
		return glyph.Fonts{}, "", err
	}
	for _, family := range families {
		if strings.EqualFold(family.Name, name) {
			fonts, err := family.Load()
			return fonts, family.Name, err
		}
	}
	return glyph.Fonts{}, "", fmt.Errorf(
		"no monospace family %q is installed; -list-fonts shows the %d there are",
		name, len(families))
}

// loadBook reads the saved servers.
//
// A list that could not be read is not a reason to refuse to open a
// window: the book comes back empty and refuses to save, and the window
// says why on its first frame. Everything else still works, including
// connecting to a machine typed by hand.
func loadBook() *remote.Book {
	path, err := remote.BookPath()
	if err != nil {
		// A book that knows why it is unusable, so the window says so
		// and the add dialog refuses rather than taking what is typed
		// and having nowhere to put it.
		return remote.UnusableBook(err)
	}
	book, err := remote.LoadBook(path)
	if err != nil {
		// Kept on the book, and shown in the window rather than only
		// here: a window opened from an icon has no console to read.
		log.Print(err)
	}
	return book
}

// startup is what the flags say the window opens with: a local shell,
// or a connection to the machine -ssh named.
type startup struct {
	// target is what -ssh was given, and empty for a local shell.
	target string

	// command is what -e was given: the program the shell runs instead
	// of the login shell, here or on the far end.
	command []string
}

// openFirst opens what the window starts with and returns its pane.
//
// The pane is nil with -ssh, because the connection opens its own on the
// first frame, and nil when what the window would have opened could not
// be opened. Either way the window opens: a failure is posted as a
// notice, because a gridterm started from Explorer has no console for a
// message to reach.
func (a *app) openFirst(s startup) (*term.Terminal, error) {
	if s.target == "" {
		t, err := a.localTerminal()
		if err != nil {
			a.noteFirstPane(noFirstPane, err)
			return nil, nil
		}
		a.showPane(t)
		return t, nil
	}
	cfg, err := remote.ParseTarget(s.target)
	if err != nil {
		a.noteFirstPane("Could not read what -ssh names", err)
		return nil, nil
	}
	// Where a new pane goes from now on, so a new pane or a split opens on the
	// machine -ssh named rather than on this one.
	a.home = cfg.Target()
	// On the first frame rather than from here, because the tree the pane
	// goes in and the dialogs it asks in are built after this returns.
	a.pump.post(func() {
		a.connectFor(cfg.Target(), cfg, opening{command: s.command})
		// A pane here instead: a route that was refused, a window that
		// could not be taken over and a pane that could not be placed all
		// leave nothing at all in it. A local shell that will not start
		// leaves the window empty, and says so above.
		if len(a.panes) == 0 {
			if err := a.openPaneHere(); err != nil {
				a.reportError("Could not open a terminal", err)
			}
		}
	})
	return nil, nil
}

// noFirstPane heads the notice a window shows when it could not open
// the pane it starts with.
const noFirstPane = "Could not open a terminal"

// noteFirstPane says why the window is opening with nothing in it, in a
// notice on the first frame and in the log.
//
// Both, because a notice is gone once it is dismissed and a log is there
// for a machine that has one.
func (a *app) noteFirstPane(title string, err error) {
	a.logError(err)
	a.pump.post(func() { a.reportError(title, err) })
}

// startingPanes is what the stage opens with: the first pane, or nothing
// at all when there is none to open on.
func startingPanes(first *term.Terminal) []ui.Widget {
	if first == nil {
		return nil
	}
	return []ui.Widget{first}
}

// named reports whether a flag was given on the command line.
//
// Asked rather than compared against the default, because the default is
// a real value somebody may ask for: "-font-size 14" means the size is
// the command line's even when fourteen is what it would have been.
func named(flagName string) bool {
	given := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == flagName {
			given = true
		}
	})
	return given
}
