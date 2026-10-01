package app

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/marrasen/kakel/install"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/single"
)

// kakel -install and kakel -uninstall: done with no window, as from a
// shell, an installer script, or the system's list of installed apps.

// installHere installs this copy, and says where.
func installHere() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// Installed again, as by the install script, it keeps the shortcut
	// and the start with the computer it had.
	to, err := install.Install(exe, install.Options{Desktop: install.Desktop(), Autostart: install.Autostart(), Version: thisVersion()})
	if err != nil {
		return sayDone("Couldn't install kakel", err)
	}
	return sayDone("kakel is installed in "+to+".", nil)
}

// uninstallHere ends the kakel running, then takes the installed one
// away. The settings stay, for a kakel installed again.
func uninstallHere() error {
	if !confirm("Remove kakel?", "The program, its shortcuts and its start with the computer go. Your settings and saved servers stay.") {
		return nil
	}
	if dir, err := settings.Dir(); err == nil {
		if taken, _ := single.Hand(dir, single.Handover{Args: []string{"-quit"}}); taken && !waitGone(dir, 60*time.Second) {
			return sayDone("Couldn't remove kakel", errors.New("the kakel running didn't end; close it and try again"))
		}
	}
	if err := install.Uninstall(); err != nil {
		return sayDone("Couldn't remove kakel", err)
	}
	return sayDone("kakel is removed.", nil)
}

// waitGone waits for the kakel running for dir to end, as long as
// patience, while it asks whether to close what is open. It reports
// whether it ended.
func waitGone(dir string, patience time.Duration) bool {
	for end := time.Now().Add(patience); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if !single.Running(dir) {
			return true
		}
	}
	return false
}

// sayDone tells the user how it went: in a message box on Windows, where
// kakel has no console, and on standard output elsewhere.
func sayDone(text string, err error) error {
	if err != nil {
		text = text + ": " + err.Error()
	}
	if !messageBox("kakel", text, err != nil) {
		fmt.Println(text)
	}
	return err
}
