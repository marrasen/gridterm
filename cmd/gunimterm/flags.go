package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/marrasen/gridterm/glyph"
	"github.com/marrasen/gridterm/vt"
)

// options are what the command line asks for, as gridterm's flags ask.
type options struct {
	fontSize   float64
	command    string
	scrollback int
	ssh        string
	fontFiles  string
	fontFamily string
	listFonts  bool
	mcpSkill   bool
	asMCP      bool
	stats      bool
	shot       string
	// sizeSet says -font-size was given, which the size kept from last
	// time does not overrule.
	sizeSet bool
}

// parseOptions reads the command line.
func parseOptions(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet(programName, flag.ContinueOnError)
	fs.Float64Var(&o.fontSize, "font-size", float64(defaultFontSize), "font size in logical pixels")
	fs.StringVar(&o.command, "e", "",
		"run this command instead of the login shell; split on spaces, no quoting")
	fs.IntVar(&o.scrollback, "scrollback", vt.DefaultScrollback, "lines of history to keep")
	fs.StringVar(&o.ssh, "ssh", "",
		"connect to [user@]host[:port] over SSH instead of running a local shell")
	fs.StringVar(&o.fontFiles, "font", "",
		"font files to use instead of the bundled Go Mono, comma separated,"+
			" in the order regular,bold,italic,bold-italic")
	fs.StringVar(&o.fontFamily, "font-family", "",
		"use this installed monospace family instead of the bundled Go Mono")
	fs.BoolVar(&o.listFonts, "list-fonts", false, "print the installed monospace families and exit")
	fs.BoolVar(&o.mcpSkill, "mcp-skill", false,
		"print the skill that tells an agent how to work in a pane handed over,"+
			" and exit; the share dialog writes it for you")
	fs.BoolVar(&o.asMCP, "mcp", false,
		"serve this machine's panes to an agent over the Model Context Protocol,"+
			" on standard input and output, instead of opening a window")
	fs.BoolVar(&o.stats, "stats", os.Getenv("GUNIMTERM_STATS") == "1",
		"say each second how many frames were drawn, on standard error")
	fs.StringVar(&o.shot, "shot", "",
		"drive the window through a script and write PNGs, then exit;"+
			" steps are wait:<ms> until:<text> key:<chord> type:<text>"+
			" shot:<file>, e.g. \"until:$ type:make key:Enter until:done shot:built.png\"")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "font-size" {
			o.sizeSet = true
		}
	})
	o.fontSize = min(max(o.fontSize, 8), 40)
	if o.scrollback < 0 {
		return o, fmt.Errorf("-scrollback %d: a count of lines cannot be below zero", o.scrollback)
	}
	if o.shot != "" {
		if _, err := parseShot(o.shot); err != nil {
			return o, fmt.Errorf("-shot: %w", err)
		}
	}
	return o, nil
}

// printFonts writes the installed monospaced families and their styles,
// as gridterm's -list-fonts does.
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
		glyph.Regular: "regular", glyph.Bold: "bold",
		glyph.Italic: "italic", glyph.BoldItalic: "bold-italic",
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

// loadFontFiles reads the font files -font names, in the order regular,
// bold, italic, bold italic. A trailing style may be left out and an
// inner one left empty, which draws it from the regular face.
func loadFontFiles(list string) (glyph.Fonts, error) {
	var files glyph.Fonts
	into := []*[]byte{&files.Regular, &files.Bold, &files.Italic, &files.BoldItalic}
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
	if files.Regular == nil {
		return glyph.Fonts{}, fmt.Errorf("the first file is the regular font and is required")
	}
	return files, nil
}

// applyOptions sets the program up as the command line asks, before
// the window opens its first pane.
func (a *app) applyOptions() error {
	o := a.opts
	if o.sizeSet {
		a.st.FontSize = float32(o.fontSize)
	}
	scrollbackLines = o.scrollback
	switch {
	case o.fontFiles != "":
		files, err := loadFontFiles(o.fontFiles)
		if err != nil {
			return fmt.Errorf("-font: %w", err)
		}
		faces, err := facesOf(files)
		if err != nil {
			return fmt.Errorf("-font: %w", err)
		}
		a.st.Font = Font{Name: "-font", Faces: faces}
		// A face named on the command line is an instruction, and a
		// theme does not overrule it.
		a.fontPicked = true
	case o.fontFamily != "":
		a.fontPicked = true
		a.fixedFont = o.fontFamily
		if _, compiled := compiledIn(o.fontFamily); compiled {
			if err := a.setFont(o.fontFamily); err != nil {
				return fmt.Errorf("-font-family: %w", err)
			}
		}
	}
	return nil
}

// openFirst opens the window's first pane: the user's shell here, a
// command run instead of it, or a shell on a server over SSH.
func (a *app) openFirst() error {
	switch {
	case a.opts.ssh != "":
		return a.connect(ConnectTo{Target: a.opts.ssh})
	case strings.TrimSpace(a.opts.command) != "":
		a.handle(RunCommand{Line: a.opts.command})
		if len(a.st.Panes) == 0 {
			return fmt.Errorf("-e %q: the command did not start", a.opts.command)
		}
		return nil
	}
	return a.open("", placement{})
}
