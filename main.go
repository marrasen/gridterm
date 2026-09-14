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

	"github.com/hajimehoshi/ebiten/v2"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"

	"github.com/marrasen/gridterm/conns"
	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
	"github.com/marrasen/gridterm/jobs"
	"github.com/marrasen/gridterm/meter"
	"github.com/marrasen/gridterm/remote"
	"github.com/marrasen/gridterm/render"
	"github.com/marrasen/gridterm/session"
	"github.com/marrasen/gridterm/ui"
	"github.com/marrasen/gridterm/ui/term"
	"github.com/marrasen/gridterm/vt"
)

// bundledFonts returns the Go Mono faces compiled into the binary.
func bundledFonts() glyph.Fonts {
	return glyph.Fonts{
		Regular:    gomono.TTF,
		Bold:       gomonobold.TTF,
		Italic:     gomonoitalic.TTF,
		BoldItalic: gomonobolditalic.TTF,
	}
}

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
		shotScript = flag.String("shot", "",
			"drive the window through a script and write PNGs, then exit;"+
				" steps are wait:<frames> key:<chord> type:<text> shot:<file>,"+
				` e.g. "wait:60 key:ctrl+k shot:palette.png"`)
	)
	flag.Parse()

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
	m := atlas.Metrics()

	a := &app{atlas: atlas, renderer: render.New(atlas), fontSize: *fontSize}
	// What -font-family chose, so the font menu treats it as the one in
	// use rather than offering to switch to it again.
	a.fontFamily = family
	a.comp = render.NewCompositor(a.renderer)
	// The compositor is called by the game loop and has nowhere to hand
	// a failure back to.
	a.comp.OnError = a.logError

	const initCols, initRows = 100, 32
	pal := vt.DefaultPalette()
	a.g = grid.New(initCols, initRows, pal.FG, pal.BG)
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)
	a.lastSize = [2]int{initCols, initRows}

	// What every pane is started with, so a split can open another.
	command := strings.Fields(*cmdline)
	a.keys = remote.NewRing()
	a.ctx, a.stop = context.WithCancel(context.Background())
	a.book = loadBook()
	a.newSession = func(cols, rows int) (session.Session, error) {
		return startSession(*sshTarget, command, a.keys, cols, rows)
	}
	// Where a pane opened by a split or a tab runs. With -ssh that is
	// the machine on the far end, not this one.
	a.localHost = conns.Local
	if *sshTarget != "" {
		if cfg, err := remote.ParseTarget(*sshTarget); err == nil {
			a.localHost = cfg.Target()
		}
	}
	a.registry = conns.New()
	a.rates = make(map[*conns.Entry]*meter.Rate)
	a.machines = make(map[string]*machine)
	a.opening = make(map[string]context.CancelFunc)
	a.paneOn = make(map[*term.Terminal]*machine)
	a.tunnels = make(map[*conns.Entry]*tunnel)
	a.queue = jobs.New(0)
	a.jobs = make(map[*conns.Entry]*jobs.Job)
	a.asking = make(map[chan jobs.Choice]func())
	a.scrollback = *scroll
	a.colours = pal
	a.panes = make(map[*term.Terminal]*conns.Entry)
	a.ended = make(map[*term.Terminal]bool)
	a.exits = make(chan struct{}, exitQueue)

	first, err := a.newTerminal()
	if err != nil {
		log.Fatal(err)
	}

	a.commands()
	shot, err := parseShotScript(*shotScript)
	if err != nil {
		log.Fatalf("-shot: %v", err)
	}
	a.shot = shot
	// Off the drawing goroutine: reading every font file the system has
	// takes long enough to be seen as the window failing to open.
	a.startFontScan()
	a.showPane(first)

	// The tree: the menu bar over the sidebar and the stage beside it.
	// Everything the window opens goes on the stage, which shows one at
	// a time; the sidebar is what chooses.
	a.panel = a.newPanel()
	a.side = a.newSidebar()
	a.stage = a.newTabs(first)
	a.dock = ui.NewDock(panelWidth, a.side, a.stage)
	a.dock.DividerFG = a.colours.ANSI[8]
	a.dock.DividerBG = a.colours.BG
	// The sidebar is painted onto a grid of its own, over the window's,
	// so that its rows can have room around them while the terminal
	// beside it keeps every line the same height.
	a.sideRegion = newRegion(a.side, grid.New(0, 0, a.colours.FG, a.colours.BG), &a.sideGeo)
	a.dock.PanelElsewhere = true
	a.comp.Add(a.sideRegion.layer)
	// Open to begin with: it is how everything in the window is reached,
	// so a window that hid it would open with no way in.
	a.dock.Collapsed = false
	a.bar = a.newMenubar(a.dock)
	a.refreshServers()
	// Told once there is a window to tell them in: this runs before one
	// exists, so the message waits for the first frame.
	a.reportBookError()
	a.root.SetWidget(a.bar)
	a.root.Layout(ui.Rect{Cols: initCols, Rows: initRows})

	ebiten.SetWindowTitle("gridterm")
	// Room for the padding on top of the cells, or the window opens a
	// column and a row short of the size it was asked for.
	padX, padY := a.padsWanted()
	ebiten.SetWindowSize(
		m.CellW*initCols+padX*m.CellW/grid.PadUnit,
		m.CellH*initRows+padY*m.CellH/grid.PadUnit)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// Damage tracking only pays off if ebiten keeps the previous frame.
	ebiten.SetScreenClearedEveryFrame(false)
	ebiten.SetVsyncEnabled(true)

	// A clean quit is ebiten.Termination rather than nil, so the shells
	// have to be closed into a separate error or every failure to shut
	// one down is dropped on the path the user actually takes.
	err = ebiten.RunGame(a)
	// Every connection still being made, and every dialog waiting for an
	// answer, is let go of here rather than left holding a goroutine.
	a.stop()
	if errors.Is(err, ebiten.Termination) {
		err = nil
	}
	closed := []error{err}
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
	closed = append(closed, a.waitForCloses(jobsGrace)...)
	closed = append(closed, a.closeTunnels(), a.closeMachines())
	if err := errors.Join(closed...); err != nil {
		log.Fatal(err)
	}
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
		fmt.Fprintln(w, "no monospace font families found")
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
		fmt.Fprintf(w, "%-34s %s\n", family.Name, strings.Join(styles, ", "))
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

// startSession opens either a local shell or an SSH connection. The rest
// of the program cannot tell the difference: both are a byte stream and
// a size.
func startSession(target string, command []string, ring *remote.Ring, cols, rows int) (session.Session, error) {
	if target == "" {
		return session.StartLocal(session.LocalConfig{
			Command: command,
			Cols:    cols,
			Rows:    rows,
		})
	}
	cfg, err := remote.ParseTarget(target)
	if err != nil {
		return nil, err
	}
	// -ssh connects before the window opens, so there is nowhere to draw
	// a dialog and the console is the only place left to ask. Connecting
	// from inside the window uses askUser and its dialogs instead.
	cfg.Ask = consoleAsk{}
	cfg.Ring = ring
	sh, err := remote.StartShell(context.Background(), cfg, remote.ShellConfig{
		Command: command,
		Cols:    cols,
		Rows:    rows,
	})
	if err != nil {
		return nil, err
	}
	return sh, nil
}
