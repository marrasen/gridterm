package main

import (
	"strings"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim"
)

// complete sets Go To's ghost for text: the rest of a folder's name,
// once the folder it is in has been listed, and asks for that listing
// otherwise.
func (b *browser) complete(text string, u *gunim.UI) {
	if b.goTo == nil {
		return
	}
	b.goTo.Ghost = ""
	dir, leaf, ok := splitLeaf(b.st.Sep, text)
	if !ok {
		return
	}
	if dir != b.st.Listed.Dir {
		if dir != b.asked {
			b.asked = dir
			u.Send(b, app.ListFolders{Pane: b.id, Dir: dir})
		}
		return
	}
	b.goTo.Ghost = restOf(b.st.Sep == `\`, b.st.Listed.Folders, leaf)
}

// splitLeaf cuts a typed path into the folder it is in and the name
// being typed, with sep the filesystem's separator. A Windows path may
// be typed with forward slashes.
func splitLeaf(sep, text string) (dir, leaf string, ok bool) {
	if sep == "" {
		sep = "/"
	}
	if sep == `\` {
		text = strings.ReplaceAll(text, "/", sep)
	}
	at := strings.LastIndex(text, sep)
	if at < 0 {
		return "", "", false
	}
	dir, leaf = text[:at+1], text[at+1:]
	if len(dir) > 1 && !strings.HasSuffix(dir, ":"+sep) {
		// A folder is named without its last separator, the top of a
		// filesystem with it.
		dir = strings.TrimSuffix(dir, sep)
	}
	return dir, leaf, true
}

// restOf is what the names starting with leaf share after it, and empty
// when nothing does: whatever the case, where the filesystem pays case
// no mind.
func restOf(anyCase bool, names []string, leaf string) string {
	rest, found := "", false
	for _, name := range names {
		if len(name) <= len(leaf) {
			continue
		}
		starts := strings.HasPrefix(name, leaf)
		if anyCase {
			starts = strings.EqualFold(name[:len(leaf)], leaf)
		}
		if !starts {
			continue
		}
		after := name[len(leaf):]
		if !found {
			rest, found = after, true
			continue
		}
		n := 0
		for n < len(rest) && n < len(after) && rest[n] == after[n] {
			n++
		}
		rest = rest[:n]
		if rest == "" {
			return ""
		}
	}
	return rest
}
