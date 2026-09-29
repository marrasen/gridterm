// Package links has the rules for following what a pane shows: which
// links may open, where a machine's own loopback address points, and
// which paths are files here.
package links

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// LocalService is where an address on a machine's own loopback points,
// as the target for a tunnel from another machine, and whether it is
// one.
func LocalService(at string) (string, bool) {
	u, err := url.Parse(at)
	if err != nil || u.Port() == "" || !isLoopbackName(u.Hostname()) {
		return "", false
	}
	return net.JoinHostPort("127.0.0.1", u.Port()), true
}

// isLoopbackName reports whether name is this machine's own, or any.
func isLoopbackName(name string) bool {
	if strings.EqualFold(name, "localhost") {
		return true
	}
	ip := net.ParseIP(name)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// SameTarget reports whether two targets are the same port on the same
// machine, the loopback's names being one.
func SameTarget(a, b string) bool {
	ah, ap, err := net.SplitHostPort(a)
	if err != nil {
		return a == b
	}
	bh, bp, err := net.SplitHostPort(b)
	if err != nil {
		return a == b
	}
	return ap == bp && (strings.EqualFold(ah, bh) || isLoopbackName(ah) && isLoopbackName(bh))
}

// Openable refuses what is no web or mail address.
func Openable(at string) error {
	at = strings.TrimSpace(at)
	switch {
	case at == "":
		return errors.New("there is no address to open")
	case strings.ContainsAny(at, "\r\n\x00"):
		// A line break would end the command line and start another.
		return errors.New("that address has a line break in it, so it is not one")
	}
	u, err := url.Parse(at)
	if err != nil {
		return fmt.Errorf("%s is not an address: %w", at, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto", "ftp", "ftps":
		return nil
	}
	return fmt.Errorf("kakel opens web and mail links, and %s is a %q link", at, u.Scheme)
}

// OnDisk is the file or folder a path in a pane names on this
// machine, read against the pane's folder when it is relative.
func OnDisk(text, dir string) (at string, isDir, ok bool) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, "\r\n\x00") {
		return "", false, false
	}
	try := text
	if !filepath.IsAbs(text) && !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, `\\`) {
		if dir == "" {
			return "", false, false
		}
		try = filepath.Join(dir, text)
	}
	info, err := os.Lstat(try)
	if err != nil {
		return "", false, false
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		if target, err := os.Stat(try); err == nil {
			return try, target.IsDir(), true
		}
	}
	return try, info.IsDir(), true
}

// WindowsAbs reports whether a path starts at the top of a Windows
// drive, "C:\dir" or "C:/dir", the way a shell on a Windows machine
// prints one.
func WindowsAbs(p string) bool {
	if len(p) < 3 || p[1] != ':' || p[2] != '\\' && p[2] != '/' {
		return false
	}
	c := p[0]
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// linkSchemes, linkLeading and linkTrailing are what a link in a
// server's words starts with, and the marks around it that are not
// part of it.
var linkSchemes = []string{"http://", "https://"}

const (
	linkLeading  = `(<"'`
	linkTrailing = `.,)>"'`
)

// Only is the one web link in lines, as a server writes one, and false when there is none or
// more than one: several give nothing to guess between.
func Only(lines []string) (string, bool) {
	var found string
	for _, line := range lines {
		for _, word := range strings.Fields(line) {
			at, ok := linkIn(word)
			if !ok {
				continue
			}
			if found != "" && found != at {
				return "", false
			}
			found = at
		}
	}
	return found, found != ""
}

// linkIn is the link a word is, if it is one.
func linkIn(word string) (string, bool) {
	word = strings.TrimLeft(word, linkLeading)
	lower := strings.ToLower(word)
	is := false
	for _, scheme := range linkSchemes {
		is = is || strings.HasPrefix(lower, scheme)
	}
	if !is {
		return "", false
	}
	at := strings.TrimRight(word, linkTrailing)
	for _, scheme := range linkSchemes {
		if strings.EqualFold(at, scheme) {
			return "", false
		}
	}
	if Openable(at) != nil {
		return "", false
	}
	return at, true
}
