// Command kakel is a terminal emulator, drawn with gunim.
//
// It runs shells here and on other machines through its own sessions,
// VT parser and key encoder, and draws their screens with gunim's
// CellGrid, beside the file panes, tunnels and the rest of the window.
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
	"slices"
	"sync"
	"time"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"

	"github.com/marrasen/kakel/appicon"
	"github.com/marrasen/kakel/mcp"
	"github.com/marrasen/kakel/settings"
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
		// kakel's MCP server, for an agent program to start: it holds
		// nothing and reaches nothing until the agent gives it a code.
		return mcp.Serve(ctx, os.Stdin, os.Stdout, mcp.NewWindow())
	case opts.mcpSkill:
		_, err := io.WriteString(os.Stdout, skillFor(hostNamed(hostClaudeCode), exePath()))
		return err
	case opts.listFonts:
		return printFonts(os.Stdout)
	}
	if path := os.Getenv("KAKEL_PROFILE"); path != "" {
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
		sh := screen.NewShells()
		all, trouble := loadThemesSaying()
		ws := &ownWindows{app: a, sh: sh, all: all}
		w, c, err := ws.open(gunim.WindowOptions{Size: firstSize(keptFontSize(), 220)})
		if err != nil {
			return err
		}
		if opts.stats {
			go logStats(ctx, w)
		}
		prog := newApp(c, sh)
		prog.wins[0].gw = w
		prog.openWindow = ws.openFrom
		prog.opts = opts
		prog.themes = all
		prog.themeTrouble = trouble
		prog.registerThemes = ws.registerThemes
		defer closeToaster()
		return errors.Join(prog.run(ctx), prog.shotErr)
	})
	if errors.Is(err, driver.ErrNoDriver) {
		log.Print("gunim has no driver for this operating system")
		return nil
	}
	return err
}

// ownWindows opens kakel's windows, each with the window's view
// mounted, and names the themes to every one of them.
type ownWindows struct {
	app *gunim.App
	sh  *screen.Shells
	mu  sync.Mutex
	all []themed
	win []*gunim.Window
}

// open opens a window with o's size and place.
func (ws *ownWindows) open(o gunim.WindowOptions) (*gunim.Window, gunim.Client, error) {
	o.Title = programName
	o.Icons = appicon.Images()
	// The close button asks first, as Exit does for the last window.
	o.AskToClose = CloseWindow{}
	w, err := ws.app.NewWindow(o)
	if err != nil {
		return nil, gunim.Client{}, fmt.Errorf("kakel: %w", err)
	}
	c := w.Client()
	ws.mu.Lock()
	all := ws.all
	ws.win = append(ws.win, w)
	ws.mu.Unlock()
	registerThemes(w, all)
	// Each window its own keys, which the shortcuts file changes there.
	keys := shortcuts()
	gunim.RegisterView(w, "window", func(State) *window { return newWindow(ws.sh, keys, all) },
		func(win *window, st State, u *gunim.UI) { win.update(st, u) })
	if err := c.Mount(gunim.Root, "window", "window", State{}, windowTopic); err != nil {
		return nil, gunim.Client{}, err
	}
	return w, c, nil
}

// openFrom opens a window size large, its top left corner at at in
// from's space.
func (ws *ownWindows) openFrom(from *gunim.Window, at geom.Point, size geom.Size) (gunim.Client, *gunim.Window, error) {
	w, c, err := ws.open(gunim.WindowOptions{Size: size, Parent: from, Anchor: at})
	return c, w, err
}

// registerThemes names all to every window, and to those opened later.
func (ws *ownWindows) registerThemes(all []themed) {
	ws.mu.Lock()
	ws.all = all
	list := slices.Clone(ws.win)
	ws.mu.Unlock()
	for _, w := range list {
		registerThemes(w, all)
	}
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
