//go:build linux

package install

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"

	"github.com/marrasen/kakel/appicon"
)

// Paths on Linux count letter case, and a running program is replaced
// by moving another over it.
const (
	caseless   = false
	movesAside = false
)

// home is the user's home folder.
func home() (string, error) { return os.UserHomeDir() }

// dataHome is $XDG_DATA_HOME, or ~/.local/share, and configHome
// $XDG_CONFIG_HOME, or ~/.config.
func dataHome() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d, nil
	}
	h, err := home()
	return filepath.Join(h, ".local", "share"), err
}

func configHome() (string, error) {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d, nil
	}
	h, err := home()
	return filepath.Join(h, ".config"), err
}

// Exe is where kakel is installed: ~/.local/bin/kakel.
func Exe() (string, error) {
	h, err := home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".local", "bin", Name), nil
}

// desktopFile is what a desktop file says of kakel, started with args.
func desktopFile(exe, args string) []byte {
	return []byte("[Desktop Entry]\nType=Application\nName=kakel\nComment=Terminals, files and tunnels on your machines\n" +
		"Exec=" + exe + args + "\nIcon=" + Name + "\nTerminal=false\nCategories=System;TerminalEmulator;\n")
}

// paths are where the desktop file, the icon, the autostart file and the
// desktop's shortcut go.
type paths struct{ app, icon, autostart, desk string }

func where() (paths, error) {
	d, err := dataHome()
	if err != nil {
		return paths{}, err
	}
	c, err := configHome()
	if err != nil {
		return paths{}, err
	}
	h, err := home()
	if err != nil {
		return paths{}, err
	}
	return paths{
		app:       filepath.Join(d, "applications", Name+".desktop"),
		icon:      filepath.Join(d, "icons", "hicolor", "256x256", "apps", Name+".png"),
		autostart: filepath.Join(c, "autostart", Name+".desktop"),
		desk:      filepath.Join(h, "Desktop", Name+".desktop"),
	}, nil
}

// write writes data to path, making its folder.
func write(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

// register writes the desktop file and the icon, and the desktop's
// shortcut when asked.
func register(exe string, o Options) error {
	p, err := where()
	if err != nil {
		return err
	}
	var icon bytes.Buffer
	if err := png.Encode(&icon, appicon.Draw(256)); err != nil {
		return err
	}
	if err := write(p.icon, icon.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write the icon: %w", err)
	}
	if err := write(p.app, desktopFile(exe, ""), 0o644); err != nil {
		return fmt.Errorf("write the desktop file: %w", err)
	}
	if o.Desktop {
		if err := write(p.desk, desktopFile(exe, ""), 0o755); err != nil {
			return fmt.Errorf("put kakel on the desktop: %w", err)
		}
	} else {
		_ = os.Remove(p.desk)
	}
	return nil
}

// SetAutostart starts kakel with the user's session, into the tray, or
// no longer.
func SetAutostart(on bool) error {
	p, err := where()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(p.autostart); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := Exe()
	if err != nil {
		return err
	}
	return write(p.autostart, desktopFile(exe, " -tray"), 0o644)
}

// Autostart reports whether kakel starts with the user's session.
func Autostart() bool {
	p, err := where()
	if err != nil {
		return false
	}
	_, err = os.Stat(p.autostart)
	return err == nil
}

// Uninstall takes the program, its desktop files and its icon away.
func Uninstall() error {
	p, err := where()
	if err != nil {
		return err
	}
	for _, f := range []string{p.app, p.icon, p.autostart, p.desk} {
		_ = os.Remove(f)
	}
	exe, err := Exe()
	if err != nil {
		return err
	}
	// A running program on Linux can be taken away under itself.
	if err := os.Remove(exe); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
