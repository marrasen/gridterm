package shells

import (
	"bytes"
	"context"
	"encoding/binary"
	"os/exec"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// wslListTimeout is how long wsl.exe gets to answer, which a healthy install does in well under a
// second.
const wslListTimeout = 5 * time.Second

// wslWaitDelay is how much longer the pipes get once wsl.exe is killed, since a process it started can
// still hold them open.
const wslWaitDelay = time.Second

// wslBOM is the byte order mark some builds of wsl.exe write before the list.
var wslBOM = []byte{0xff, 0xfe}

// wslDistros returns the installed WSL distributions, or why wsl.exe could not say. A machine without
// WSL fails with exec.ErrNotFound, a broken one with a timeout or whatever wsl.exe reported, and find
// tells the two apart.
func wslDistros() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), wslListTimeout)
	defer cancel()

	// -q lists the installed distributions by name alone, stopped ones included.
	cmd := exec.CommandContext(ctx, "wsl.exe", "-l", "-q")
	cmd.WaitDelay = wslWaitDelay
	out, err := cmd.Output()
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

// decodeUTF16 returns what wsl.exe wrote as UTF-8, reading UTF-16LE, UTF-16LE behind a byte order
// mark, and UTF-8 alike.
func decodeUTF16(b []byte) string {
	if bytes.HasPrefix(b, wslBOM) {
		return fromUTF16LE(b[len(wslBOM):])
	}
	// With no mark: UTF-8 from wsl.exe holds no zero byte and is valid UTF-8, so an even-length
	// buffer that breaks either rule is UTF-16LE.
	if len(b)%2 == 0 && (bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b)) {
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
