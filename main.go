// Command gridterm is a GPU-rendered terminal emulator.
//
// It runs a shell either on a local pseudo-terminal — a PTY on Unix, a
// ConPTY on Windows — or on another machine over SSH, and draws it as a
// widget in the text-mode toolkit under ui.
package main

import (
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

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/grid"
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
	a.newSession = func(cols, rows int) (session.Session, error) {
		return startSession(*sshTarget, command, cols, rows)
	}
	a.scrollback = *scroll
	a.colours = pal
	a.panes = make(map[*term.Terminal]struct{})
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
	a.bar = a.newMenubar(first)
	a.root.SetWidget(a.bar)
	a.root.Layout(ui.Rect{Cols: initCols, Rows: initRows})

	ebiten.SetWindowTitle("gridterm")
	ebiten.SetWindowSize(m.CellW*initCols, m.CellH*initRows)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// Damage tracking only pays off if ebiten keeps the previous frame.
	ebiten.SetScreenClearedEveryFrame(false)
	ebiten.SetVsyncEnabled(true)

	// A clean quit is ebiten.Termination rather than nil, so the shells
	// have to be closed into a separate error or every failure to shut
	// one down is dropped on the path the user actually takes.
	err = ebiten.RunGame(a)
	if errors.Is(err, ebiten.Termination) {
		err = nil
	}
	closed := []error{err}
	for t := range a.panes {
		closed = append(closed, t.Close())
	}
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

// startSession opens either a local shell or an SSH connection. The rest
// of the program cannot tell the difference: both are a byte stream and
// a size.
func startSession(target string, command []string, cols, rows int) (session.Session, error) {
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
	// Without these, SSH works only with an agent or an unencrypted key
	// on disk: a passphrase-protected key is skipped and password
	// authentication is never even offered.
	cfg.Passphrase = func(keyfile string) (string, error) {
		return promptSecret("passphrase for " + keyfile + ": ")
	}
	cfg.Password = func() (string, error) {
		return promptSecret("password for " + cfg.User + "@" + cfg.Host + ": ")
	}
	sh, err := remote.StartShell(cfg, remote.ShellConfig{
		Command: command,
		Cols:    cols,
		Rows:    rows,
	})
	if err != nil {
		return nil, err
	}
	return sh, nil
}
