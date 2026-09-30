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
}

// inTray reports whether kakel runs on in the tray once its last window
// closes.
func (a *app) inTray() bool { return a.tray.on && !a.gone }

// trayWanted reports whether the settings want the tray.
func (a *app) trayWanted() bool {
	return a.traySet.Set != nil && (a.settings == nil || a.settings.Tray())
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
	if a.tray.on && sig == a.tray.was {
		return
	}
	items, actions := a.trayMenu()
	pick := func(id int) {
		a.events <- func() {
			if f := a.tray.actions[id]; f != nil {
				f()
			}
		}
	}
	click := func() { a.events <- func() { a.toTray(a.showServers) } }
	err := a.traySet.Set(gunim.Tray{Icon: trayIcons(), Tooltip: ProgramName, Items: items, OnPick: pick, OnClick: click})
	if err != nil {
		if !errors.Is(err, gunim.ErrNoTray) {
			a.failed("Couldn't show kakel in the tray", err.Error())
		}
		a.tray = trayState{}
		a.traySet.StayOpen(false)
		return
	}
	a.tray = trayState{on: true, actions: actions, was: sig}
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
	}
	sig.WriteString("\x01")
	for _, m := range a.machines.Connected() {
		sig.WriteString(string(m) + "\x00")
	}
	return sig.String()
}

// trayMenu is the tray icon's menu, and what each of its lines does.
func (a *app) trayMenu() ([]gunim.TrayItem, map[int]func()) {
	actions := map[int]func(){}
	next := 0
	act := func(f func()) int {
		next++
		actions[next] = f
		return next
	}
	machine := func(m machines.ID, title string) gunim.TrayItem {
		sub := []gunim.TrayItem{
			{Title: "Terminal", ID: act(func() { a.toTray(func() { a.handle(OpenOn{Machine: m}) }) })},
			{Title: "Files", ID: act(func() { a.toTray(func() { a.handle(FilesOn{Machine: m}) }) })},
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
			if len(a.liveWins()) == 0 {
				// Nothing open to ask about.
				a.exitNow()
				return
			}
			a.toTray(a.askToQuit)
		})},
	)
	return items, actions
}

// leaveTray takes the icon out of the tray, as kakel ends.
func (a *app) leaveTray() {
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
		return true
	})
}

// handover opens a window for the command line a kakel started
// meanwhile handed over: a terminal, or what its options ask for, in
// the folder it was started in.
func (a *app) handover(h single.Handover) {
	o, err := ParseOptions(h.Args)
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
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		a.nextDir = dir
	}
	was := a.opts
	a.opts.ssh, a.opts.command = o.ssh, o.command
	a.openFirstOrSay()
	a.opts.ssh, a.opts.command = was.ssh, was.command
	a.nextDir = ""
}
