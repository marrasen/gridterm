package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/marrasen/kakel/install"
)

// This computer's own settings, as a saved server has its: the folder a
// new terminal starts in, the shell it runs, and the folders worth
// opening files at.

// SaveThisComputer keeps this computer's settings. StartFolder is
// where a new terminal starts, empty for the home folder, and a leading
// ~ is the home folder; Shell is the shell a new terminal runs, by the
// ID the shell list gives it, empty for the user's own; Folders are
// folders to open files at.
type SaveThisComputer struct {
	StartFolder string
	Shell       string
	Folders     []string
}

// ThisComputer is what the windows are told of this computer's
// settings. The shell is State's ChosenShell.
type ThisComputer struct {
	StartFolder string
	Folders     []string
}

// showThisComputer tells the windows this computer's settings.
func (a *app) showThisComputer() {
	if a.settings == nil {
		return
	}
	a.st.ThisComputer.StartFolder, a.st.ThisComputer.Folders = a.settings.Local()
}

// saveThisComputer checks and keeps this computer's settings.
func (a *app) saveThisComputer(in SaveThisComputer) error {
	if a.settings == nil {
		return errors.New("the settings could not be read, so nothing can be kept")
	}
	start := strings.TrimSpace(in.StartFolder)
	if start != "" {
		full, err := FolderHere(start)
		if err != nil {
			return err
		}
		start = full
	}
	if err := a.settings.PutLocal(start, in.Folders); err != nil {
		return err
	}
	a.showThisComputer()
	if in.Shell != a.st.ChosenShell {
		return a.pickShell(in.Shell)
	}
	return nil
}

// FolderHere is path as a folder on this computer, a leading ~ the
// home folder, and an error when there is no such folder.
func FolderHere(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	fi, err := os.Stat(path)
	switch {
	case err != nil:
		return "", errors.New("there is no folder " + path)
	case !fi.IsDir():
		return "", errors.New(path + " is a file, not a folder")
	}
	return filepath.Clean(path), nil
}

// startDir is the folder a local shell starts in when nothing names
// another: cwd, where kakel was started, unless that is a folder nobody
// starts kakel in to work there. Those are the home folder, the
// installed program's own folder and Windows's system folder, which a
// shortcut, a desktop's menu or a start with the computer gives it.
// From one of those it starts in the start folder This Computer sets,
// or else at home.
func (a *app) startDir(cwd string) string {
	home, _ := os.UserHomeDir()
	defaults := []string{home}
	if to, err := install.Exe(); err == nil {
		defaults = append(defaults, filepath.Dir(to))
	}
	if root := os.Getenv("SystemRoot"); root != "" {
		defaults = append(defaults, root, filepath.Join(root, "System32"), filepath.Join(root, "SysWOW64"))
	}
	byDefault := cwd == ""
	for _, d := range defaults {
		if d != "" && cwd != "" && install.SamePath(cwd, d) {
			byDefault = true
		}
	}
	if !byDefault {
		return cwd
	}
	if a.settings != nil {
		if start, _ := a.settings.Local(); start != "" {
			if fi, err := os.Stat(start); err == nil && fi.IsDir() {
				return start
			}
		}
	}
	if home != "" {
		return home
	}
	return cwd
}
