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
		scroll = flag.Int("scrollback", vt.DefaultScrollback, "lines of history to keep")
		remote = flag.String("ssh", "",
			"connect to [user@]host[:port] over SSH instead of running a local shell")
		fontFiles = flag.String("font", "",
			"font files to use instead of the bundled Go Mono, comma separated,"+
				" in the order regular,bold,italic,bold-italic")
	)
	flag.Parse()

	fonts := bundledFonts()
	if *fontFiles != "" {
		f, err := loadFonts(*fontFiles)
		if err != nil {
			log.Fatalf("-font: %v", err)
		}
		fonts = f
	}

	atlas, err := glyph.NewAtlas(fonts, *fontSize, 96)
	if err != nil {
		log.Fatalf("build glyph atlas: %v", err)
	}
	m := atlas.Metrics()

	a := &app{atlas: atlas, renderer: render.New(atlas), fontSize: *fontSize}
	a.comp = render.NewCompositor(a.renderer)

	const initCols, initRows = 100, 32
	pal := vt.DefaultPalette()
	a.g = grid.New(initCols, initRows, pal.FG, pal.BG)
	a.layer = &render.Layer{Grid: a.g}
	a.comp.Add(a.layer)
	a.lastSize = [2]int{initCols, initRows}

	// What every pane is started with, so a split can open another.
	command := strings.Fields(*cmdline)
	a.newSession = func(cols, rows int) (session.Session, error) {
		return startSession(*remote, command, cols, rows)
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
	a.root.SetWidget(first)
	a.root.Layout(ui.Rect{Cols: initCols, Rows: initRows})

	ebiten.SetWindowTitle("gridterm")
	ebiten.SetWindowSize(m.CellW*initCols, m.CellH*initRows)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// Damage tracking only pays off if ebiten keeps the previous frame.
	ebiten.SetScreenClearedEveryFrame(false)
	ebiten.SetVsyncEnabled(true)

	err = ebiten.RunGame(a)
	for t := range a.panes {
		if cerr := t.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	if err != nil && !errors.Is(err, ebiten.Termination) {
		log.Fatal(err)
	}
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
	cfg, err := session.ParseSSHTarget(target)
	if err != nil {
		return nil, err
	}
	cfg.Command = command
	cfg.Cols, cfg.Rows = cols, rows
	// Without these, SSH works only with an agent or an unencrypted key
	// on disk: a passphrase-protected key is skipped and password
	// authentication is never even offered.
	cfg.Passphrase = func(keyfile string) (string, error) {
		return promptSecret("passphrase for " + keyfile + ": ")
	}
	cfg.Password = func() (string, error) {
		return promptSecret("password for " + cfg.User + "@" + cfg.Host + ": ")
	}
	return session.StartSSH(cfg)
}
