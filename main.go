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
	"os/exec"
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
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/single"
)

func main() {
	err := run()
	// A restart into a new copy, as after an update: started once this
	// one has stopped listening, so it runs as the one.
	if exe := app.RestartInto(); exe != "" {
		if serr := exec.Command(exe).Start(); serr != nil {
			log.Printf("couldn't start %s again: %v", exe, serr)
		}
	}
	if err != nil {
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

	// One kakel for the user: one already running is handed the
	// command line, and opens a window for it.
	var handovers <-chan single.Handover
	if opts.OneOfMany() {
		if dir, err := settings.Dir(); err == nil {
			cwd, _ := os.Getwd()
			if taken, err := single.Hand(dir, single.Handover{Args: os.Args[1:], Dir: cwd}); taken {
				return nil
			} else if err != nil {
				log.Print(err)
			}
			var stop func()
			if handovers, stop, err = single.Listen(ctx, dir); err != nil {
				log.Printf("kakel runs alone: %v", err)
			} else {
				// Gone before the process is, so the next kakel does not
				// find it.
				defer stop()
			}
		}
	}
	if opts.Quits() {
		// No kakel was running to end.
		return nil
	}

	app.CaptureLog()
	err = gunim.Main(ctx, func(a *gunim.App) error {
		sh := screen.NewShells()
		all, trouble := look.LoadSaying()
		ws := &ownWindows{app: a, sh: sh, all: all, place: opts.WindowPlace}
		// Where it was as it last closed, or else sized for the font.
		w, c, err := ws.open(gunim.WindowOptions{Size: opts.WindowSize(), Place: opts.WindowPlace(), Hidden: opts.StartsInTray()})
		if err != nil {
			return err
		}
		if opts.ShowStats() {
			go logStats(ctx, w)
		}
		return app.Start(ctx, app.Config{
			Client: c, Window: w, Shells: sh, OpenWindow: ws.openFrom, Options: opts,
			Themes: all, ThemeTrouble: trouble, RegisterThemes: ws.registerThemes,
			Tray: app.Tray{Set: a.SetTray, StayOpen: a.StayOpen}, Handovers: handovers,
			OpenLauncher: ws.openLauncher, HotKeys: a.RegisterHotKey,
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
	// place is where the last window was as it closed, for one opened
	// with none open.
	place func() *driver.Placement
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
	o := gunim.WindowOptions{Size: size, Parent: from, Anchor: at}
	if from == nil && ws.place != nil {
		// With none open, as from the tray: where the last one was.
		o.Place = ws.place()
	}
	w, c, err := ws.open(o)
	return c, w, err
}

// openLauncher opens the launcher's window, over the others in the
// middle of the main display, with its view mounted.
func (ws *ownWindows) openLauncher() (gunim.Client, error) {
	o := gunim.WindowOptions{Title: app.ProgramName, Size: view.LauncherSize, Icons: appicon.Images(), Pinned: true, TitleBar: view.NoTitleBar()}
	if mons := ws.app.Monitors(); len(mons) > 0 {
		m := mons[0]
		for _, o := range mons {
			if o.Primary {
				m = o
			}
		}
		area := m.WorkArea
		if area.Empty() {
			area = m.Bounds
		}
		scale := m.CoordsPerLogical
		if scale <= 0 {
			scale = 1
		}
		w, h := view.LauncherSize.W*scale, view.LauncherSize.H*scale
		c := area.Center()
		// A third of the way down, as a search box sits.
		o.Place = &driver.Placement{Bounds: geom.Rc(c.X-w/2, area.Min.Y+(area.Size().H-h)/3, w, h)}
	}
	w, err := ws.app.NewWindow(o)
	if err != nil {
		return gunim.Client{}, fmt.Errorf("kakel: %w", err)
	}
	ws.mu.Lock()
	all := ws.all
	ws.mu.Unlock()
	look.Register(w, all)
	gunim.RegisterView(w, "launcher", func(app.LaunchState) *view.Launcher { return view.NewLauncher() },
		func(l *view.Launcher, st app.LaunchState, u *gunim.UI) { l.Update(st, u) })
	c := w.Client()
	if err := c.Mount(gunim.Root, "launcher", "launcher", app.LaunchState{}, app.LauncherTopic); err != nil {
		c.Close()
		return gunim.Client{}, err
	}
	return c, nil
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
