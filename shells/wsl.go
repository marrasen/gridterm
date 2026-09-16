package shells

import (
	"bytes"
	"context"
	"encoding/binary"
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"
)

// wslListTimeout is how long wsl.exe gets to answer. A healthy install answers in well under a
// second, and a broken one would otherwise hang the menu for as long as it liked.
const wslListTimeout = 5 * time.Second

// wslBOM is the byte order mark some builds of wsl.exe write before the list.
var wslBOM = []byte{0xff, 0xfe}

// wslDistros returns the installed WSL distributions, or why wsl.exe could not say. A machine without
// WSL fails here, and find reads that as no distributions.
func wslDistros() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), wslListTimeout)
	defer cancel()

	// -q lists the installed distributions by name alone, stopped ones included.
	out, err := exec.CommandContext(ctx, "wsl.exe", "-l", "-q").Output()
	if err != nil {
		return nil, err
	}
	return parseDistros(out), nil
}

// parseDistros reads the distribution names out of what wsl.exe wrote.
func parseDistros(out []byte) []string {
	var names []string
	for _, line := range strings.Split(decodeUTF16(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// decodeUTF16 returns text as UTF-8. wsl.exe writes UTF-16LE, on some builds behind a byte order
// mark, but a test is easier to write in UTF-8 and that is taken as it stands.
func decodeUTF16(b []byte) string {
	if bytes.HasPrefix(b, wslBOM) {
		return fromUTF16LE(b[len(wslBOM):])
	}
	// With no mark, a zero high byte on the first character is what tells the two apart, since a
	// distribution name starts with a letter either way.
	if len(b) >= 2 && len(b)%2 == 0 && b[1] == 0 {
		return fromUTF16LE(b)
	}
	return string(b)
}

// fromUTF16LE decodes UTF-16LE, dropping a trailing odd byte.
func fromUTF16LE(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, binary.LittleEndian.Uint16(b[i:]))
	}
	return string(utf16.Decode(u))
}
