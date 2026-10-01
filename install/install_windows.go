//go:build windows

package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Paths on Windows ignore letter case, and a running program cannot be
// written over, only moved.
const (
	caseless   = true
	movesAside = true
)

// Where Windows keeps a user's installed apps and the programs it starts
// with the user.
const (
	uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\` + Name
	runKey       = `Software\Microsoft\Windows\CurrentVersion\Run`
)

// Exe is where kakel is installed: %LOCALAPPDATA%\Programs\kakel.
func Exe() (string, error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return "", fmt.Errorf("find %%LOCALAPPDATA%%: %w", err)
	}
	return filepath.Join(dir, "Programs", Name, Name+".exe"), nil
}

// startMenu is the Start menu shortcut, and desktop the one on the
// desktop.
func startMenu() (string, error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_Programs, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, Name+".lnk"), nil
}

func desktop() (string, error) {
	dir, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, Name+".lnk"), nil
}

// register makes the shortcuts and the entry under Installed apps.
func register(exe string, o Options) error {
	menu, err := startMenu()
	if err != nil {
		return err
	}
	if err := shortcut(menu, exe); err != nil {
		return fmt.Errorf("make the Start menu shortcut: %w", err)
	}
	if desk, err := desktop(); err == nil {
		if o.Desktop {
			if err := shortcut(desk, exe); err != nil {
				return fmt.Errorf("make the desktop shortcut: %w", err)
			}
		} else {
			_ = os.Remove(desk)
		}
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("list kakel under Installed apps: %w", err)
	}
	defer func() { _ = k.Close() }()
	size := uint32(0)
	if fi, err := os.Stat(exe); err == nil {
		size = uint32(fi.Size() / 1024)
	}
	for _, v := range []struct{ name, value string }{
		{"DisplayName", Name},
		{"DisplayVersion", o.Version},
		{"Publisher", "Marcus Johansson"},
		{"DisplayIcon", exe},
		{"InstallLocation", filepath.Dir(exe)},
		{"UninstallString", `"` + exe + `" -uninstall`},
		{"URLInfoAbout", "https://github.com/marrasen/kakel"},
	} {
		if err := k.SetStringValue(v.name, v.value); err != nil {
			return err
		}
	}
	for _, v := range []struct {
		name  string
		value uint32
	}{{"NoModify", 1}, {"NoRepair", 1}, {"EstimatedSize", size}} {
		if err := k.SetDWordValue(v.name, v.value); err != nil {
			return err
		}
	}
	return nil
}

// shortcut makes a shortcut at lnk to exe, through PowerShell's Windows
// Script Host object: the shell's own way to write one, with no COM
// written here.
func shortcut(lnk, exe string) error {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := "$s = (New-Object -ComObject WScript.Shell).CreateShortcut(" + quote(lnk) + "); " +
		"$s.TargetPath = " + quote(exe) + "; $s.WorkingDirectory = " + quote(filepath.Dir(exe)) + "; " +
		"$s.IconLocation = " + quote(exe+",0") + "; $s.Save()"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SetAutostart starts kakel with the user, into the tray, or no longer.
func SetAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open the programs Windows starts: %w", err)
	}
	defer func() { _ = k.Close() }()
	if !on {
		if err := k.DeleteValue(Name); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := Exe()
	if err != nil {
		return err
	}
	return k.SetStringValue(Name, `"`+exe+`" -tray`)
}

// Autostart reports whether kakel starts with the user.
func Autostart() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = k.Close() }()
	_, _, err = k.GetStringValue(Name)
	return err == nil
}

// Uninstall takes the shortcuts, the entry and the start with the user
// away, and the program's folder once this program, which may be the
// one in it, has ended.
func Uninstall() error {
	exe, err := Exe()
	if err != nil {
		return err
	}
	if menu, err := startMenu(); err == nil {
		_ = os.Remove(menu)
	}
	if desk, err := desktop(); err == nil {
		_ = os.Remove(desk)
	}
	_ = SetAutostart(false)
	if err := registry.DeleteKey(registry.CURRENT_USER, uninstallKey); err != nil && err != registry.ErrNotExist {
		return err
	}
	dir := filepath.Dir(exe)
	// A program cannot take away the folder it runs from: a shell does,
	// a moment after this one has ended.
	cmd := exec.Command("cmd.exe", "/c", "ping -n 3 127.0.0.1 >nul & rmdir /s /q \""+dir+"\"")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW | windows.DETACHED_PROCESS}
	return cmd.Start()
}
