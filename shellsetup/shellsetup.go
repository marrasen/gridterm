// Package shellsetup holds the lines that teach a shell to say what it
// is doing.
//
// A shell is a separate program, and gridterm only sees the bytes it
// prints. So it cannot know which directory the shell is in, or where
// one command's output ends and the next begins, unless the shell says
// so. The shell says so by printing escape sequences nobody sees: OSC 7
// for the directory, OSC 133 around each command.
//
// The lines here are typed into the shell as soon as it starts, the way
// a user would type them. That is the one way that works everywhere: on
// a server the login shell is started by sshd and gridterm never sees a
// command line to add to.
package shellsetup

import (
	"strings"

	"github.com/marrasen/gridterm/shells"
)

// Route is the kind of shell the lines are written for.
type Route int

const (
	// NoRoute is a shell nothing is typed into.
	NoRoute Route = iota

	// Posix is bash, zsh and anything close enough to them, which is
	// what a server's login shell is taken to be.
	Posix

	// PowerShell is Windows PowerShell and PowerShell.
	PowerShell

	// Cmd is the Command Prompt.
	Cmd
)

// RouteFor is the kind of shell an argv runs.
//
// An empty argv means a login shell on a machine at the far end, which
// is taken to be POSIX: sshd starts whatever the account is set to, and
// nothing in the protocol says what that is.
func RouteFor(argv []string) Route {
	if len(argv) == 0 {
		return Posix
	}
	switch strings.ToLower(shells.CommandBase(argv[0])) {
	case "cmd", "cmd.exe":
		return Cmd
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return PowerShell
	}
	return Posix
}

// Lines are what to type into a shell of that kind, one line each,
// ending with the one that clears the pane.
//
// The pane is cleared because the shell echoes what is typed into it,
// and a pane that opens on a screenful of setup is worse than one that
// opens empty. A shell's own banner goes with it.
func Lines(r Route) []string {
	switch r {
	case Posix:
		// The leading space keeps it out of the history of a shell set
		// to ignore lines that start with one.
		return []string{" " + posixLine, "clear"}
	case PowerShell:
		return []string{powerShellLine}
	case Cmd:
		// prompt takes the rest of its line literally, so the clear
		// cannot be joined onto it.
		return []string{cmdPrompt, "cls"}
	}
	return nil
}

// posixLine tells bash and zsh to mark the directory and each command.
//
// __gt_d runs before each prompt: it reports how the last command went,
// says where the shell is, and marks the prompt. __gt_c runs before a
// command and marks where its output starts. bash has no hook for that,
// so the debug trap stands in, skipping the calls this makes itself and
// firing once per command line.
const posixLine = `__gt_d(){ local c=$?; __gt_r=; printf '\033]133;D;%s\a\033]7;file://%s%s\a\033]133;A\a' "$c" "${HOSTNAME:-${HOST:-localhost}}" "$PWD"; }; ` +
	`__gt_c(){ case "$BASH_COMMAND" in __gt_*) return;; esac; [ -n "$__gt_r" ] || { __gt_r=1; printf '\033]133;C\a'; }; }; ` +
	`if [ -n "$ZSH_VERSION" ]; then autoload -Uz add-zsh-hook; add-zsh-hook precmd __gt_d; add-zsh-hook preexec __gt_c; ` +
	`else PROMPT_COMMAND="__gt_d${PROMPT_COMMAND:+;$PROMPT_COMMAND}"; trap __gt_c DEBUG; fi`

// powerShellLine wraps the prompt that is already there rather than
// replacing it, so a profile that sets one keeps it.
//
// The command mark goes on the function PSReadLine calls to read a
// line. Without PSReadLine the host reads the line itself and never
// calls it, so there is no mark and the rest still works.
const powerShellLine = `$global:GtPrompt=$function:prompt; ` +
	`function global:prompt { $ok=$?; $e=[char]27; $b=[char]7; $c=0; ` +
	`if(-not $ok){ if($global:LASTEXITCODE){$c=$global:LASTEXITCODE}else{$c=1} }; ` +
	`$h=(Get-Location).Path; ` +
	`Write-Host -NoNewline "$e]133;D;$c$b$e]7;file://localhost/$([uri]::EscapeUriString(($h -replace '\\','/')))$b$e]133;A$b"; ` +
	`$t=if($global:GtPrompt){& $global:GtPrompt}else{"PS $h> "}; "$t$e]133;B$b" }; ` +
	`if($function:PSConsoleHostReadLine){ $global:GtRead=$function:PSConsoleHostReadLine; ` +
	`function global:PSConsoleHostReadLine { $l=& $global:GtRead; Write-Host -NoNewline "$([char]27)]133;C$([char]7)"; $l } }; ` +
	`Clear-Host`

// cmdPrompt is the Command Prompt saying where it is.
//
// OSC 9;9 rather than OSC 7, because the prompt is built from what
// cmd.exe substitutes and it has no way to make a URL out of a path.
// $e is an escape, $p the directory and $g a greater-than sign.
const cmdPrompt = `prompt $e]9;9;$p$e\$p$g`

// Typed is the bytes to write to a shell's input, with a return after
// each line.
//
// A return rather than a newline: this goes in where the keyboard goes
// in, and the key sends a return.
func Typed(r Route) []byte {
	lines := Lines(r)
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\r") + "\r")
}
