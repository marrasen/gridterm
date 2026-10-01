package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/words"
)

// ImportSSHConfig saves the machines ~/.ssh/config names that are not
// saved yet, each under its alias.
type ImportSSHConfig struct{}

// sshConfigPath is the OpenSSH client config read; a test sets it.
var sshConfigPath = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "config"), nil
}

// importSSHConfig saves the machines the SSH config names. One saved
// already, by its name or by the same user, address and port, is left
// as it is. A machine reached through another goes after it, so its
// route names a server that is there.
func (a *app) importSSHConfig() error {
	if a.book == nil {
		return errors.New("the saved servers could not be read")
	}
	path, err := sshConfigPath()
	if err != nil {
		return err
	}
	found, err := remote.ReadSSHConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		a.notice(NoticePlain, "No SSH config to import", "There is no "+path+".", "")
		return nil
	}
	if err != nil {
		return err
	}
	have := a.book.Hosts()
	saved := func(h remote.Host) bool {
		return slices.ContainsFunc(have, func(o remote.Host) bool {
			return strings.EqualFold(o.Name, h.Name) ||
				strings.EqualFold(o.Address, h.Address) && o.User == h.User && o.Port == h.Port
		})
	}
	var todo []remote.Host
	skipped := 0
	for _, c := range found {
		h := c.Host()
		if saved(h) {
			skipped++
			continue
		}
		todo = append(todo, h)
	}
	// Each route's server first: one not saved and not coming is no
	// route, and the machine is saved without it.
	named := func(name string) bool {
		return slices.ContainsFunc(have, func(o remote.Host) bool { return strings.EqualFold(o.Name, name) }) ||
			slices.ContainsFunc(todo, func(o remote.Host) bool { return strings.EqualFold(o.Name, name) })
	}
	for i := range todo {
		if todo[i].Via != "" && !named(todo[i].Via) {
			todo[i].Via = ""
		}
	}
	slices.SortStableFunc(todo, func(x, y remote.Host) int {
		switch {
		case x.Via == "" && y.Via != "":
			return -1
		case x.Via != "" && y.Via == "":
			return 1
		}
		return 0
	})
	var added []string
	var errs []error
	for _, h := range todo {
		if err := a.book.Put(h, ""); err != nil {
			errs = append(errs, errors.New(h.Name+": "+err.Error()))
			continue
		}
		added = append(added, h.Name)
	}
	a.st.Saved = a.book.Hosts()
	if len(errs) > 0 {
		a.failed("Couldn't import some of the SSH config", errors.Join(errs...).Error())
	}
	switch {
	case len(added) > 0:
		body := strings.Join(added, ", ") + "."
		if skipped > 0 {
			body += " " + itoa(skipped) + " saved already."
		}
		a.worked("Imported "+words.Count(len(added), "server")+" from the SSH config", body, "")
	case len(errs) == 0:
		a.notice(NoticePlain, "Nothing new in the SSH config", "Every machine "+path+" names is saved already.", "")
	}
	return nil
}
