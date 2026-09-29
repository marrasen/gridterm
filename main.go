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
	"log"
	"os"
	"os/signal"
	"runtime/pprof"
	"slices"
	"sync"
	"time"

	"github.com/marrasen/kakel/view"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/look"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"

	"github.com/marrasen/kakel/appicon"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	opts, err := app.ParseOptions(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if alone, err := app.RunAlone(ctx, opts); alone {
		return err
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

	app.CaptureLog()
	err = gunim.Main(ctx, func(a *gunim.App) error {
		sh := screen.NewShells()
		all, trouble := look.LoadSaying()
		ws := &ownWindows{app: a, sh: sh, all: all}
		// Where it was as it last closed, or else sized for the font.
		w, c, err := ws.open(gunim.WindowOptions{Size: opts.WindowSize(), Place: opts.WindowPlace()})
		if err != nil {
			return err
		}
		if opts.ShowStats() {
			go logStats(ctx, w)
		}
		return app.Start(ctx, app.Config{
			Client: c, Window: w, Shells: sh, OpenWindow: ws.openFrom, Options: opts,
			Themes: all, ThemeTrouble: trouble, RegisterThemes: ws.registerThemes,
		})
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
	all []look.Themed
	win []*gunim.Window
}

// open opens a window with o's size and place.
func (ws *ownWindows) open(o gunim.WindowOptions) (*gunim.Window, gunim.Client, error) {
	o.Title = app.ProgramName
	o.Icons = appicon.Images()
	// The close button asks first, as Exit does for the last window.
	o.AskToClose = app.CloseWindow{}
	w, err := ws.app.NewWindow(o)
	if err != nil {
		return nil, gunim.Client{}, fmt.Errorf("kakel: %w", err)
	}
	c := w.Client()
	ws.mu.Lock()
	all := ws.all
	ws.win = append(ws.win, w)
	ws.mu.Unlock()
	look.Register(w, all)
	// Each window its own keys, which the shortcuts file changes there.
	keys := view.Shortcuts()
	gunim.RegisterView(w, "window", func(app.State) *view.Window { return view.NewWindow(ws.sh, keys, all) },
		func(win *view.Window, st app.State, u *gunim.UI) { win.Update(st, u) })
	if err := c.Mount(gunim.Root, "window", "window", app.State{}, app.WindowTopic); err != nil {
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
func (ws *ownWindows) registerThemes(all []look.Themed) {
	ws.mu.Lock()
	ws.all = all
	list := slices.Clone(ws.win)
	ws.mu.Unlock()
	for _, w := range list {
		look.Register(w, all)
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
