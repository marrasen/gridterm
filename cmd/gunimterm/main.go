// Command gunimterm is a spike: one gridterm terminal in a gunim window.
//
// It runs the user's shell through gridterm's own session, VT parser
// and key encoder, and draws the screen with gunim's CellGrid. It is
// here to measure whether gunim draws a busy terminal fast enough, and
// whether every key reaches the shell as it should, before gridterm's
// interface moves over.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime/pprof"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/driver"

	"github.com/marrasen/gridterm/appicon"
	"github.com/marrasen/gridterm/mcp"
	"github.com/marrasen/gridterm/settings"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	opts, err := parseOptions(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	switch {
	case opts.asMCP:
		// gridterm's MCP server, for an agent program to start: it holds
		// nothing and reaches nothing until the agent gives it a code.
		return mcp.Serve(ctx, os.Stdin, os.Stdout, mcp.NewWindow())
	case opts.mcpSkill:
		_, err := io.WriteString(os.Stdout, skillFor(hostNamed(hostClaudeCode), exePath()))
		return err
	case opts.listFonts:
		return printFonts(os.Stdout)
	}
	if path := os.Getenv("GUNIMTERM_PROFILE"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}

	log.SetOutput(windowLog)
	// keptFontSize is the font size kept from last time, which the
	// window opens to fit.
	keptFontSize := func() float32 {
		if opts.sizeSet {
			return float32(opts.fontSize)
		}
		if path, err := settings.Path(); err == nil {
			if s, err := settings.Load(path); err == nil {
				if size, ok := s.FontSize(); ok {
					return min(max(float32(size), 8), 40)
				}
			}
		}
		return defaultFontSize
	}
	err = gunim.Main(ctx, func(a *gunim.App) error {
		w, err := a.NewWindow(gunim.WindowOptions{
			Title: programName,
			Size:  firstSize(keptFontSize(), 220),
			Icons: appicon.Images(),
			// The close button asks first, as Exit does.
			AskToClose: Exit{},
		})
		if err != nil {
			return fmt.Errorf("gunimterm: %w", err)
		}
		c := w.Client()
		sh := &shells{m: map[string]*shell{}}
		keys := shortcuts()
		all, trouble := loadThemesSaying()
		registerThemes(w, all)
		gunim.RegisterView(w, "window", func(State) *window { return newWindow(sh, keys, all) },
			func(win *window, st State, u *gunim.UI) { win.update(st, u) })
		if err := c.Mount(gunim.Root, "window", "window", State{}, windowTopic); err != nil {
			return err
		}
		if opts.stats {
			go logStats(ctx, w)
		}
		prog := newApp(c, sh)
		prog.opts = opts
		prog.themes = all
		prog.themeTrouble = trouble
		prog.registerThemes = func(all []themed) { registerThemes(w, all) }
		defer closeToaster()
		return errors.Join(prog.run(ctx), prog.shotErr)
	})
	if errors.Is(err, driver.ErrNoDriver) {
		log.Print("gunim has no driver for this operating system")
		return nil
	}
	return err
}

// logStats prints, each second, how many frames the window drew and
// how many screen updates arrived and were merged into them.
func logStats(ctx context.Context, w *gunim.Window) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var last gunim.Stats
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s := w.Stats()
		log.Printf("frames %d, updates %d, merged %d", s.Frames-last.Frames, s.Commands-last.Commands, s.Coalesced-last.Coalesced)
		last = s
	}
}
