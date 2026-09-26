package shells

import (
	"strconv"
	"strings"
	"unicode"
)

// CommandPrefix starts the id of every command that opens a pane on a
// shell, as a menu, the palette and a shortcuts file name it.
const CommandPrefix = "shell.open."

// CommandIDs names the command that opens a pane on each shell in
// list, in the same order. Two ids that flatten to the same name are
// told apart by a number, so neither shell is dropped.
func CommandIDs(list []Shell) []string {
	taken := make(map[string]bool, len(list))
	ids := make([]string, 0, len(list))
	for _, sh := range list {
		base := CommandID(sh.ID)
		id := base
		for n := 2; taken[id]; n++ {
			id = base + "-" + strconv.Itoa(n)
		}
		taken[id] = true
		ids = append(ids, id)
	}
	return ids
}

// CommandID names the command that opens a pane on a shell. A shell's
// id holds a colon, spaces and stops; a command id has none of them, so
// that a menu or a key binding naming one is readable.
func CommandID(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	return CommandPrefix + b.String()
}
