package files

import "strings"

// SafeName turns a pane's name into a file name every filesystem
// takes, for a save to suggest.
func SafeName(name string) string {
	clean := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < ' ' {
			return '-'
		}
		return r
	}, name)
	clean = strings.Trim(clean, " .-")
	// Windows reads these as devices wherever they sit, so a pane
	// called "con" would write to the console and fail with nothing a
	// user can act on.
	switch strings.ToLower(clean) {
	case "con", "prn", "aux", "nul", "com1", "com2", "com3", "com4",
		"com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		clean += "-pane"
	}
	if clean == "" {
		return "scrollback"
	}
	// A pane's name is whatever the program put in its title, and a
	// long command line makes a path no filesystem will take.
	if len(clean) > mostNameBytes {
		clean = strings.TrimRight(clean[:mostNameBytes], " .-")
	}
	if clean == "" {
		return "scrollback"
	}
	return clean
}

// mostNameBytes is the longest file name suggested, well inside what
// every filesystem here takes.
const mostNameBytes = 80
