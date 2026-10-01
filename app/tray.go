package app

import (
	"errors"
	"image"
	"os"
	"strings"

	"github.com/marrasen/kakel/appicon"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/single"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
)

// kakel lives in the system tray while the tray will have it: an icon
// whose menu lists the servers, each with what can be opened on it,
// which works whether a window is open or not. Closing the last window
// then leaves kakel running there, and Exit, or Quit kakel on the
// icon's menu, ends it. A kakel started meanwhile hands its command
// line to this one, which opens a window for it.

// Intents for the tray.
type (
	// ToggleTray keeps kakel in the tray, or out of it, and says so for
	// next time.
	ToggleTray struct{}
)

// Tray is what the program needs of gunim's tray: showing an icon, and
// keeping the program running with no window.
type Tray struct {
	Set      func(gunim.Tray) error
	StayOpen func(bool)
}

// trayState is the tray icon as shown: what each line of its menu does,
// by its ID, and the menu it was made from, to make it again only when
// that changes.
type trayState struct {
	on      bool
	actions map[int]func()
	was     string
	// gen numbers the menus made, in the high bits of their lines' IDs,
	// so a pick from a menu since replaced does nothing. failed is the
	// menu the tray last refused, not offered again until it changes.
	gen    int
	failed string
}

// inTray reports whether kakel runs on in the tray once its last window
// closes.
func (a *app) inTray() bool { return a.tray.on && !a.gone }

// trayWanted reports whether the settings want the tray.
func (a *app) trayWanted() bool {
	return a.traySet.Set != nil && a.opts.Trays() && (a.settings == nil || a.settings.Tray())
}

// showTray shows the tray icon, made again when what its menu lists has
// changed, or takes it away when it is not wanted.
func (a *app) showTray() {
	if !a.trayWanted() {
		if a.tray.on {
			_ = a.traySet.Set(gunim.Tray{})
			a.tray = trayState{}
			a.traySet.StayOpen(false)
		}
		return
	}
	sig := a.traySig()
	if a.tray.on && sig == a.tray.was || !a.tray.on && sig == a.tray.failed && a.tray.failed != "" {
		return
	}
	gen := a.tray.gen + 1
	items, actions := a.trayMenu(gen)
	pick := func(id int) {
		a.events <- func() {
			if f := a.tray.actions[id]; f != nil && !a.gone {
				f()
			}
		}
	}
	click := func() {
		a.events <- func() {
			if !a.gone {
				a.toTray(a.showServers)
			}
		}
	}
	err := a.traySet.Set(gunim.Tray{Icon: trayIcons(), Tooltip: ProgramName, Items: items, OnPick: pick, OnClick: click})
	if err != nil {
		// Said once, and not tried again until the menu changes: a
		// tray that refused refuses again, and a window flooded with
		// notices is worse than a missing icon.
		if !errors.Is(err, gunim.ErrNoTray) && a.tray.failed == "" {
			a.failed("Couldn't show kakel in the tray", err.Error())
		}
		lost := a.tray.on
		a.tray = trayState{gen: gen, failed: sig}
		if lost {
			// The icon was the way back to kakel: with no window
			// shown, one opens rather than kakel ending, or going on
			// with nothing to reach it by.
			if len(a.liveWins()) == 0 {
				a.newWindow(a.showServers)
			}
			a.traySet.StayOpen(false)
		}
		return
	}
	a.tray = trayState{on: true, actions: actions, was: sig, gen: gen}
	a.traySet.StayOpen(true)
}

// trayIcons is kakel's icon at the sizes a tray picks from.
func trayIcons() []image.Image {
	var out []image.Image
	for _, n := range []int{16, 20, 24, 32, 48} {
		out = append(out, appicon.Draw(n))
	}
	return out
}

// traySig changes when the tray icon's menu would: what it lists, the
// shells, the saved servers and which are connected.
func (a *app) traySig() string {
	var sig strings.Builder
	for _, sh := range a.st.Shells {
		sig.WriteString(sh.ID + "\x00" + sh.Title + "\x00")
	}
	sig.WriteString("\x01")
	for _, h := range a.st.Saved {
		sig.WriteString(h.ID + "\x00" + h.Name + "\x00")
		if h.Window {
			sig.WriteString("window\x00")
		}
	}
	sig.WriteString("\x01")
	for _, m := range a.machines.Connected() {
		sig.WriteString(string(m) + "\x00")
	}
	return sig.String()
}

// trayMenu is the tray icon's menu, and what each of its lines does.
func (a *app) trayMenu(gen int) ([]gunim.TrayItem, map[int]func()) {
	actions := map[int]func(){}
	next := gen << 16
	act := func(f func()) int {
		next++
		actions[next] = f
		return next
	}
	machine := func(m machines.ID, title string) gunim.TrayItem {
		sub := []gunim.TrayItem{
			{Title: "Terminal", ID: act(func() { a.toTray(func() { a.handle(OpenOn{Machine: m}) }) })},
			{Title: "Files", ID: act(func() { a.toTray(func() { a.handle(OpenFilesOn{Machine: m}) }) })},
		}
		if m == machines.Local {
			for _, sh := range a.st.Shells {
				id := sh.ID
				sub = append(sub, gunim.TrayItem{Title: sh.Title, ID: act(func() { a.toTray(func() { a.handle(OpenShellNamed{ID: id}) }) })})
			}
		} else {
			sub = append(sub, gunim.TrayItem{Title: "Connection Log", ID: act(func() { a.toTray(func() { a.handle(ShowLog{Machine: m}) }) })})
		}
		return gunim.TrayItem{Title: title, Items: sub}
	}
	items := []gunim.TrayItem{
		{Title: "Servers", ID: act(func() { a.toTray(a.showServers) }), Default: true},
		{Title: "Open Launcher", ID: act(a.openLauncher)},
		{Separator: true},
		machine(machines.Local, "This computer"),
	}
	connected := a.machines.Connected()
	for _, h := range a.st.Saved {
		if h.Window {
			continue
		}
		m := machines.ID(h.ID)
		title := h.Name
		for _, c := range connected {
			if c == m {
				title += " (connected)"
			}
		}
		items = append(items, machine(m, title))
	}
	items = append(items,
		gunim.TrayItem{Separator: true},
		gunim.TrayItem{Title: "New Window", ID: act(func() { a.newWindow(func() { a.handle(NewTerminal{}) }) })},
		gunim.TrayItem{Title: "Secrets", ID: act(func() { a.toTray(func() { a.showSecretsPane(func(string) {}) }) })},
		gunim.TrayItem{Separator: true},
		gunim.TrayItem{Title: "Quit kakel", ID: act(func() {
			if len(a.whatIsOpen()) == 0 {
				// Nothing open to ask about.
				a.exitNow()
				return
			}
			a.toTray(a.askToQuit)
		})},
	)
	return items, actions
}

// leaveTray takes the icon out of the tray, as kakel ends, and lets the
// launcher's key and window go.
func (a *app) leaveTray() {
	a.closeLauncher()
	if a.launch.release != nil {
		a.launch.release()
		a.launch.release = nil
	}
	if a.tray.on {
		_ = a.traySet.Set(gunim.Tray{})
		a.traySet.StayOpen(false)
		a.tray = trayState{}
	}
}

// toTray does f, asked for from the tray, in the window last worked in,
// brought to the front, or in a window of its own when none is open.
func (a *app) toTray(f func()) {
	w := a.work
	if w == nil || w.gone {
		w = nil
		for _, o := range a.liveWins() {
			w = o
			break
		}
	}
	if w == nil {
		a.newWindow(f)
		return
	}
	a.front(w)
	w.c.ToFront()
	f()
}

// newWindow opens a window, brings it to the front, and does f in it.
func (a *app) newWindow(f func()) {
	a.openWindowThen(geom.Pt(40, 40), a.opts.WindowSize(), func(w *ownWin) bool {
		a.front(w)
		w.c.ToFront()
		f()
		if len(a.panesIn(w)) == 0 && !a.onTheirWay() {
			// Nothing opened: the window stays, with what f said,
			// until something does.
			a.stayEmpty = true
		}
		return true
	})
}

// onTheirWay reports whether a pane is on its way, as a shell while its
// server answers.
func (a *app) onTheirWay() bool { return len(a.machines.Dialing()) > 0 || a.starting > 0 }

// rehome puts what belongs to no window any more, as a pane that
// arrived after its window closed in the tray, or a question for it,
// into the window in front, opening one when none is open.
func (a *app) rehome() {
	if a.gone {
		return
	}
	var lost []string
	for _, p := range a.st.Panes {
		if w := a.ownerOf(p.ID); (w == nil || w.gone) && !a.closing[p.ID] {
			lost = append(lost, p.ID)
		}
	}
	asks := false
	for _, q := range a.st.Asks {
		if w := a.winByID(q.win); w == nil || w.gone {
			asks = true
		}
	}
	if len(lost) == 0 && !asks {
		return
	}
	live := a.liveWins()
	if len(live) == 0 {
		if a.opening == 0 {
			a.newWindow(func() {})
		}
		return
	}
	w := a.cur
	if w == nil || w.gone {
		w = live[0]
		a.front(w)
	}
	for _, id := range lost {
		a.winOf[id] = w.id
	}
	for i := range a.st.Asks {
		if o := a.winByID(a.st.Asks[i].win); o == nil || o.gone {
			a.st.Asks[i].win = w.id
		}
	}
	if len(lost) > 0 && a.focusIn(w) == "" {
		a.setFocusIn(w, lost[0])
	}
}

// handover opens a window for the command line a kakel started
// meanwhile handed over: a terminal, or what its options ask for, in
// the folder it was started in.
func (a *app) handover(h single.Handover) {
	o, err := ParseOptions(h.Args)
	switch {
	case err == nil && o.launcher:
		a.openLauncher()
		return
	case err == nil && o.quit:
		a.askToQuit()
		return
	case err == nil && o.tray:
		// Started with the computer while one runs: it is there already.
		return
	}
	if err != nil {
		a.newWindow(func() {
			a.failed("Couldn't read the command line", err.Error())
			a.openFirstFor(Options{}, h.Dir)
		})
		return
	}
	a.newWindow(func() { a.openFirstFor(o, h.Dir) })
}

// openFirstFor opens the first pane of a window as o asks, a local
// shell in dir.
func (a *app) openFirstFor(o Options, dir string) {
	dir = a.startDir(dir)
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		a.nextDir = dir
	}
	was := a.opts
	a.opts.ssh, a.opts.command = o.ssh, o.command
	a.openFirstOrSay()
	a.opts.ssh, a.opts.command = was.ssh, was.command
	a.nextDir = ""
}
