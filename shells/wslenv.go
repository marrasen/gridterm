package shells

import (
	"path/filepath"
	"strings"
)

// IsWSL reports whether a command line starts a WSL distribution.
func IsWSL(argv []string) bool {
	return len(argv) > 0 && strings.EqualFold(filepath.Base(argv[0]), "wsl.exe")
}

// CarryIntoWSL is a WSLENV value that adds names to the ones already
// carried into a distribution. A variable Windows holds does not reach
// a WSL shell unless WSLENV names it.
//
// was is the WSLENV the window is running with, and a name already in
// it is left alone with whatever flags the user gave it.
func CarryIntoWSL(was string, names ...string) string {
	held := map[string]bool{}
	for _, entry := range strings.Split(was, ":") {
		if name, _, _ := strings.Cut(entry, "/"); name != "" {
			held[name] = true
		}
	}
	out := was
	for _, name := range names {
		if held[name] {
			continue
		}
		if out != "" {
			out += ":"
		}
		// /u carries it into WSL and not back out again, which is what a
		// name Windows set is for.
		out += name + "/u"
		held[name] = true
	}
	return out
}
