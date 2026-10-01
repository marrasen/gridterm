package app

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/marrasen/kakel/install"
	"github.com/marrasen/kakel/internal/update"
	"github.com/marrasen/kakel/settings"
)

// Installing kakel, and keeping it up to date. A copy that is not the
// installed one offers to install itself; the installed one, a release
// build, looks for a newer release a minute after it starts and once a
// day, and says so, or with Updates set to install, fetches it and puts
// it in place for the next start. A restart into the new copy is a
// question away.

// Intents for installing and updating.
type (
	// InstallKakel installs this copy for the user, with a shortcut on
	// the desktop and a start with the computer when asked, and updates
	// done by themselves with AutoUpdate.
	InstallKakel struct{ Desktop, Autostart, AutoUpdate bool }
	// SetUpdates keeps what kakel does with a newer release: one of
	// settings.UpdatesOff, UpdatesNotify and UpdatesInstall.
	SetUpdates struct{ What string }
	// ToggleAutostart starts the installed kakel with the computer, into
	// the tray, or no longer.
	ToggleAutostart struct{}
)

// Update is what the windows are told of installing and updating.
type Update struct {
	// Installed says this is the installed copy, Installable that this
	// system can have one, and Autostart that it starts with the
	// computer. Updates is the setting.
	Installed, Installable, Autostart bool
	Updates                           string
}

// updateEvery is how often the installed kakel looks for a newer
// release, and updateFirst how long after it starts it first looks.
var (
	updateEvery = 24 * time.Hour
	updateFirst = time.Minute
)

// restartInto is the program kakel starts as it ends, for a restart into
// a new copy; empty for none.
var restartInto string

// RestartInto is the program to start once kakel has ended, or "".
func RestartInto() string { return restartInto }

// startDir is the folder a local shell starts in when nothing names
// another: cwd, where kakel was started, unless that is a folder nobody
// starts kakel to work in, which only a shortcut or a start with the
// computer gives it. The installed program's own folder and Windows's
// system folder are such folders, and the shell starts at home instead.
func startDir(cwd string) string {
	if cwd == "" {
		return ""
	}
	var not []string
	if to, err := install.Exe(); err == nil {
		not = append(not, filepath.Dir(to))
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		not = append(not, root, filepath.Join(root, "System32"), filepath.Join(root, "SysWOW64"))
	}
	for _, d := range not {
		if install.SamePath(cwd, d) {
			if home, err := os.UserHomeDir(); err == nil {
				return home
			}
		}
	}
	return cwd
}

// executable is this program, as its path; a test sets it.
var executable = os.Executable

// showUpdate tells the windows where kakel stands.
func (a *app) showUpdate() {
	exe, _ := executable()
	_, err := install.Exe()
	u := Update{Installed: exe != "" && install.Installed(exe), Installable: err == nil, Autostart: install.Autostart(), Updates: settings.UpdatesNotify}
	if a.settings != nil {
		u.Updates = a.settings.Updates()
	}
	a.st.Update = u
}

// startUpdates takes away the copy the last update moved aside, and
// looks for newer releases from now on, while the settings want it.
func (a *app) startUpdates() {
	exe, err := executable()
	if err != nil {
		return
	}
	install.CleanOld(exe)
	a.showUpdate()
	if !isRelease(thisVersion()) || !a.opts.OneOfMany() || !a.st.Update.Installed {
		// A build from a working tree has no order against releases, a
		// kakel of its own leaves it to the one running, and a copy not
		// installed is the user's to keep as it is: Check for Updates
		// still offers it the newest.
		return
	}
	var look func(time.Duration)
	look = func(after time.Duration) {
		time.AfterFunc(after, func() {
			a.events <- func() {
				if a.gone {
					return
				}
				a.lookForUpdate()
				look(updateEvery)
			}
		})
	}
	look(updateFirst)
}

// isRelease reports whether version names a release, not a build from a
// working tree.
func isRelease(version string) bool {
	return strings.HasPrefix(version, "v") && update.Against(version, version) == update.Current
}

// lookForUpdate asks for the newest release, and acts on one newer.
func (a *app) lookForUpdate() {
	what := settings.UpdatesNotify
	if a.settings != nil {
		what = a.settings.Updates()
	}
	if what == settings.UpdatesOff || a.updating {
		return
	}
	a.updating = true
	go func() {
		newest, err := latestRelease(a.ctx)
		a.events <- func() {
			a.updating = false
			if err != nil || update.Against(thisVersion(), newest.Version) != update.Behind || newest.Version == a.staged {
				// Said nothing: the next look may reach GitHub, and an
				// update put in place waits for the next start.
				return
			}
			if what == settings.UpdatesInstall && a.writable() {
				a.fetchUpdate(newest, false)
				return
			}
			a.offerUpdate(newest)
		}
	}()
}

// writable reports whether this program's own copy can be replaced.
func (a *app) writable() bool {
	exe, err := executable()
	if err != nil {
		return false
	}
	f, err := os.OpenFile(exe+".new", os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(exe + ".new")
	return true
}

// offerUpdate says a newer release is out, and fetches it if asked, or
// offers the restart into it when it is in place already.
func (a *app) offerUpdate(newest update.Release) {
	have := thisVersion()
	if newest.Version == a.staged {
		if exe, err := executable(); err == nil {
			a.offerRestart(exe, "kakel "+newest.Version+" is ready", "It starts the next time kakel does.")
		}
		return
	}
	go func() {
		ans, err := a.ask(a.ctx, Ask{Title: "kakel " + newest.Version + " is out",
			Text: "This is " + have + ". Update now, and restart into it when you like.", Yes: "Update", No: "Not Now"})
		a.events <- func() {
			if err == nil && ans.Yes {
				if a.writable() {
					a.fetchUpdate(newest, true)
					return
				}
				// Not writable, as a copy under Program Files: the page,
				// to fetch it by hand.
				a.offerRelease("kakel "+newest.Version+" is out", have, newest)
			}
		}
	}()
}

// fetchUpdate fetches newest and puts it in place of this program, for
// the next start, and offers a restart into it. asked says the user
// asked, and hears a failure; one by itself fails quietly into the log.
func (a *app) fetchUpdate(newest update.Release, asked bool) {
	exe, err := executable()
	if err != nil {
		return
	}
	a.say("update", "Fetching kakel "+newest.Version+"…")
	go func() {
		part, err := update.Fetch(a.ctx, newest, runtime.GOOS, runtime.GOARCH, exe)
		if err == nil {
			err = install.Replace(part, exe)
		}
		a.events <- func() {
			a.say("update", "")
			if err != nil {
				_ = os.Remove(exe + ".new")
				if asked {
					a.failed("Couldn't update kakel", err.Error())
				} else {
					log.Printf("couldn't update kakel to %s: %v", newest.Version, err)
				}
				return
			}
			a.staged = newest.Version
			a.offerRestart(exe, "kakel "+newest.Version+" is ready", "It starts the next time kakel does.")
		}
	}()
}

// offerRestart asks whether to restart into exe now.
func (a *app) offerRestart(exe, title, text string) {
	go func() {
		ans, err := a.ask(a.ctx, Ask{Title: title, Text: text, Yes: "Restart Now", No: "Later"})
		a.events <- func() {
			if err == nil && ans.Yes {
				a.restartAs(exe)
			}
		}
	}()
}

// restartAs ends kakel, asking first as Exit does, and starts exe once
// it has.
func (a *app) restartAs(exe string) {
	restartInto = exe
	a.askToQuit()
}

// installKakel installs this copy, and offers to move to it.
func (a *app) installKakel(in InstallKakel) error {
	exe, err := executable()
	if err != nil {
		return err
	}
	to, err := install.Install(exe, install.Options{Desktop: in.Desktop, Autostart: in.Autostart, Version: thisVersion()})
	if err != nil {
		if errors.Is(err, install.ErrUnsupported) {
			return errors.New("installing is not done on this system yet")
		}
		return err
	}
	if a.settings != nil {
		what := settings.UpdatesNotify
		if in.AutoUpdate {
			what = settings.UpdatesInstall
		}
		if err := a.settings.PutUpdates(what); err != nil {
			a.failed("Couldn't keep the update setting", err.Error())
		}
	}
	a.showUpdate()
	if !install.Installed(exe) {
		a.offerRestart(to, "kakel is installed", "In "+to+". Restart into the installed kakel now?")
	}
	return nil
}
