package shells

import "strings"

// CommandBase is the program name in the first word of a command line.
//
// filepath.Base is not enough here. It splits on the separator of the
// machine this is running on, so a Linux build reads
// `C:\Windows\System32\cmd.exe` as one long name and recognises no
// shell in it. A pane runs a Windows shell whatever gridterm itself is
// running on -- over SSH to a Windows server, or through WSL -- so a
// command line is taken apart with both separators everywhere, and the
// answer is the same on every machine.
//
// The cost is a program whose own name holds a backslash, which Linux
// allows and nothing anyone runs as a shell does.
func CommandBase(argv0 string) string {
	if i := strings.LastIndexAny(argv0, `/\`); i >= 0 {
		return argv0[i+1:]
	}
	return argv0
}
