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
	var todo []remote.Host
	skipped := 0
	// The config's aliases, to name a route's server by: one saved
	// already under a name of its own, matched by its address, goes by
	// that name.
	alias := map[string]string{}
	for _, c := range found {
		h := c.Host()
		alias[strings.ToLower(h.Name)] = h.Name
		if i := slices.IndexFunc(have, func(o remote.Host) bool {
			return strings.EqualFold(o.Name, h.Name) ||
				strings.EqualFold(o.Address, h.Address) && o.User == h.User && o.Port == h.Port
		}); i >= 0 {
			alias[strings.ToLower(h.Name)] = have[i].Name
			skipped++
			continue
		}
		todo = append(todo, h)
	}
	for i := range todo {
		if todo[i].Via == "" {
			continue
		}
		if name, ok := alias[strings.ToLower(todo[i].Via)]; ok {
			todo[i].Via = name
		} else if !slices.ContainsFunc(have, func(o remote.Host) bool { return strings.EqualFold(o.Name, todo[i].Via) }) {
			// Through a server that is neither saved nor coming: saved
			// without its route.
			todo[i].Via = ""
		}
	}
	// Each machine once its route's server is saved, however long the
	// route: what is left at the end goes round in a circle.
	saved := func(name string) bool {
		_, ok := a.book.Lookup(name)
		return ok
	}
	var added []string
	var errs []error
	for len(todo) > 0 {
		var later []remote.Host
		for _, h := range todo {
			if h.Via != "" && !saved(h.Via) {
				later = append(later, h)
				continue
			}
			if err := a.book.Put(h, ""); err != nil {
				errs = append(errs, errors.New(h.Name+": "+err.Error()))
				continue
			}
			added = append(added, h.Name)
		}
		if len(later) == len(todo) {
			for _, h := range later {
				errs = append(errs, errors.New(h.Name+": its ProxyJump goes round in a circle"))
			}
			break
		}
		todo = later
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
