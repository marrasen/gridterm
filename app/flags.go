package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/marrasen/kakel/internal/quiet"
	"io"
	"log"
	"os"
	"strings"

	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/kakel/agenthost"
	"github.com/marrasen/kakel/mcp"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/settings"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"

	"github.com/marrasen/kakel/glyph"
	"github.com/marrasen/kakel/vt"
)

// Options are what the command line asks for.
type Options struct {
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
	// launcher opens the launcher, in the kakel running when there is
	// one, for a key the desktop binds where kakel can take none.
	launcher bool
	// tray starts kakel in the tray, with no window, as it does with
	// the computer; quit ends the kakel running; install and uninstall
	// install this copy for the user, or take the installed one away.
	tray, quit, install, uninstall bool
	// sizeSet says -font-size was given, which the size kept from last
	// time does not overrule.
	sizeSet bool
}

// ParseOptions reads the command line.
func ParseOptions(args []string) (Options, error) {
	var o Options
	fs := flag.NewFlagSet(ProgramName, flag.ContinueOnError)
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
	fs.BoolVar(&o.stats, "stats", os.Getenv("KAKEL_STATS") == "1",
		"say each second how many frames were drawn, on standard error")
	fs.BoolVar(&o.tray, "tray", false, "start in the tray, with no window, as kakel does with the computer")
	fs.BoolVar(&o.quit, "quit", false, "end the kakel running, asking first as Exit does while anything is open")
	fs.BoolVar(&o.install, "install", false, "install this copy of kakel for you, with a Start menu or desktop file entry, and exit")
	fs.BoolVar(&o.uninstall, "uninstall", false, "end the kakel running, take the installed kakel away, and exit; the settings stay")
	fs.BoolVar(&o.launcher, "launcher", false,
		"open the launcher, in the kakel already running if there is one;"+
			" bind this to a key where kakel cannot take one itself, as under Wayland")
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
	o.fontSize = float64(fontSizeIn(float32(o.fontSize)))
	if o.fontFiles != "" && o.fontFamily != "" {
		return o, errors.New("-font and -font-family both name a typeface; use one")
	}
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
// for -list-fonts.
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
	screen.ScrollbackLines = o.scrollback
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
		// A face named on the command line is for this run: the one
		// kept from last time waits for the next.
		a.keptFont = ""
	case o.fontFamily != "":
		a.keptFont = ""
		a.fixedFont = o.fontFamily
		if _, compiled := compiledIn(o.fontFamily); compiled {
			if err := a.setFont(o.fontFamily); err != nil {
				return fmt.Errorf("-font-family: %w", err)
			}
		}
	default:
		a.useKeptFont(false)
	}
	return nil
}

// openFirst opens the window's first pane: the user's shell here, a
// command run instead of it, or a shell on a server over SSH.
func (a *app) openFirst() error {
	switch {
	case a.opts.ssh != "":
		in := ConnectTo{Target: a.opts.ssh}
		// A saved server's name is that server, as Quick Connect takes
		// it.
		if a.book != nil {
			if h, ok := a.book.Lookup(strings.TrimSpace(a.opts.ssh)); ok && !h.Window {
				in = ConnectTo{Server: machines.ID(h.ID)}
			}
		}
		line := strings.TrimSpace(a.opts.command)
		if line == "" {
			return a.connect(in)
		}
		// -e with -ssh runs the command there, once connected.
		machine := in.Server
		if machine == "" {
			cfg, err := remote.ParseTarget(strings.TrimSpace(a.opts.ssh))
			if err != nil {
				return err
			}
			machine = a.machines.NewQuick(cfg.Target(), false)
			in.As = machine
		}
		return a.connectThen(in, func(err error) {
			if err != nil {
				return
			}
			if err := a.runCommand(RunCommand{Machine: machine, Line: line}); err != nil {
				a.failed("Couldn't run "+line+" on "+a.machines.Name(machine), err.Error())
			}
		})
	case strings.TrimSpace(a.opts.command) != "":
		// In the folder a command line handed over was started in.
		if err := a.runCommand(RunCommand{Line: a.opts.command, Dir: a.nextDir}); err != nil {
			return fmt.Errorf("-e %q: %w", a.opts.command, err)
		}
		return nil
	}
	return a.open("", Placement{})
}

// OneOfMany reports whether this kakel is one to hand its command line
// to a kakel already running: not one driving itself for screenshots,
// nor one told to run alone with KAKEL_ALONE=1.
func (o Options) OneOfMany() bool { return o.shot == "" && os.Getenv("KAKEL_ALONE") != "1" }

// StartsInTray reports whether this kakel starts in the tray, with its
// first window never shown: as it does with the computer, and when it
// is started with nothing to do, as from its shortcut. A window opens
// from the tray, the launcher's key, or kakel started again. Where
// there is no tray to start in, a window shows after all.
func (o Options) StartsInTray() bool { return o.tray || o.bare() }

// bare reports whether this kakel was started with nothing to do: no
// command, no server, no launcher, no screenshots.
func (o Options) bare() bool {
	return o.command == "" && o.ssh == "" && !o.launcher && o.shot == "" &&
		!o.quit && !o.install && !o.uninstall && !o.asMCP && !o.listFonts && !o.mcpSkill
}

// Trays reports whether this kakel shows itself in the tray: one of
// many does, and one driving itself for screenshots does not.
func (o Options) Trays() bool { return o.shot == "" }

// ProgramName is what the window is called, before the focused
// terminal's title.
const ProgramName = "kakel"

// RunAlone does what opts asks for that opens no window: serving an
// agent program over MCP, or printing the skill or the fonts. It
// reports whether there was such a thing to do.
func RunAlone(ctx context.Context, opts Options) (bool, error) {
	switch {
	case opts.asMCP:
		// kakel's MCP server, for an agent program to start: it holds
		// nothing and reaches nothing until the agent gives it a code.
		return true, mcp.Serve(ctx, os.Stdin, os.Stdout, mcp.NewWindow())
	case opts.mcpSkill:
		quiet.ToParentConsole()
		// Refused rather than printed with the bare name, as Write Skill
		// refuses: a skill naming no program to start goes on failing
		// long after this is forgotten.
		if _, ok := exeKnown(); !ok {
			return true, errors.New("-mcp-skill: the path to kakel could not be found, so the skill would name no program to start")
		}
		_, err := io.WriteString(os.Stdout, agenthost.Named(agenthost.ClaudeCode).Skill(exePath()))
		return true, err
	case opts.listFonts:
		quiet.ToParentConsole()
		return true, printFonts(os.Stdout)
	case opts.install:
		return true, installHere()
	case opts.uninstall:
		return true, uninstallHere()
	}
	return false, nil
}

// Quits says the command line only ends the kakel running, and opens
// nothing when none is.
func (o Options) Quits() bool { return o.quit }

// ShowStats says to log how the window draws, each second.
func (o Options) ShowStats() bool { return o.stats }

// WindowPlace is where the window was as it last closed, for it to open
// there again, or nil the first time. gunim moves it onto a screen when
// the one it was on is gone.
func (o Options) WindowPlace() *driver.Placement {
	path, err := settings.Path()
	if err != nil {
		return nil
	}
	s, err := settings.Load(path)
	if err != nil {
		return nil
	}
	w, ok := s.Window()
	if !ok || w.W <= 0 || w.H <= 0 {
		return nil
	}
	return &driver.Placement{Bounds: geom.Rc(w.X, w.Y, w.W, w.H), Maximized: w.Maximized}
}

// WindowSize is the first window's size: room for a terminal of the
// usual size, at the font size given or kept from last time.
func (o Options) WindowSize() geom.Size {
	size := defaultFontSize
	if o.sizeSet {
		size = float32(o.fontSize)
	} else if path, err := settings.Path(); err == nil {
		if s, err := settings.Load(path); err == nil {
			if kept, ok := s.FontSize(); ok {
				size = fontSizeIn(float32(kept))
			}
		}
	}
	return firstSize(size)
}

// CaptureLog keeps what kakel logs for the window log to show, and
// still writes it to stderr.
func CaptureLog() { log.SetOutput(plainLog{windowLog}) }

// plainLog cleans each line on its way into the window log of anything
// a terminal would act on: the log is shown in a terminal pane, and is
// served to other windows, and a line may carry what a far end said.
type plainLog struct{ to io.Writer }

func (p plainLog) Write(b []byte) (int, error) {
	if _, err := p.to.Write([]byte(serve.Plain(string(b)))); err != nil {
		return 0, err
	}
	return len(b), nil
}
