package app

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/marrasen/kakel/screen"
)

// The tests' terminals run this machine's own shell: sh on Unix, and
// cmd.exe on Windows, where kakel starts COMSPEC and ignores SHELL. The
// helpers here give the lines and commands each of them runs, so one
// test works in both.

// testAnswerVar is set to testAnswer in the tests' environment,
// which every shell they start inherits.
const (
	testAnswerVar = "KAKEL_TEST_ANSWER"
	testAnswer    = "42"
)

// saysAnswer is a line the local shell runs to print word, a dash and
// the answer, and what it prints. The answer is absent from the line,
// so finding it on the screen shows the shell ran the line.
func saysAnswer(word string) (line, want string) {
	return echoVar(word+"-", testAnswerVar), word + "-" + testAnswer
}

// echoVar is a line the local shell runs to print prefix and then the
// variable called name.
func echoVar(prefix, name string) string {
	if runtime.GOOS == "windows" {
		return "echo " + prefix + "%" + name + "%"
	}
	return "echo " + prefix + "$" + name
}

// promptEnd is the character the local shell's prompt ends with.
func promptEnd() string {
	if runtime.GOOS == "windows" {
		return ">"
	}
	return "$"
}

// clearAndEcho is a line that clears the screen and prints text, so
// the output is the only line that starts with text.
func clearAndEcho(text string) string {
	if runtime.GOOS == "windows" {
		return "cls & echo " + text
	}
	return "clear; echo " + text
}

// countTo is a line that prints the numbers from 1 to n, one a line.
func countTo(n string) string {
	if runtime.GOOS == "windows" {
		return "for /l %i in (1,1," + n + ") do @echo %i"
	}
	return "seq 1 " + n
}

// changeDir is a line that moves the local shell into dir. cmd.exe
// needs /d to move to another drive as well.
func changeDir(dir string) string {
	if runtime.GOOS == "windows" {
		return "cd /d " + dir
	}
	return "cd " + dir
}

// printDir is a line that prints the local shell's folder.
func printDir() string {
	if runtime.GOOS == "windows" {
		return "cd"
	}
	return "pwd"
}

// plainShell is the command that starts the local shell.
func plainShell() []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd.exe"}
	}
	return []string{"/bin/sh"}
}

// shellSaying is a command that starts the local shell, has it print
// word, and leaves it open.
func shellSaying(word string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd.exe", "/k", "echo " + word}
	}
	return []string{"/bin/sh", "-c", "echo " + word + "; exec /bin/sh"}
}

// echoCommand is a command line, as the Run Command box takes it, that
// prints word and exits. On Windows echo is part of cmd.exe, so the
// line starts cmd.exe to run it.
func echoCommand(word string) string {
	if runtime.GOOS == "windows" {
		return "cmd /c echo " + word
	}
	return "echo " + word
}

// The commands below are ones a plain machine has. sleep, true and
// false are no programs on Windows: a machine with Git for Windows on
// PATH has them, as a GitHub runner does, and one without has not.

// waitCommand is a command line that runs for about seconds and exits.
func waitCommand(seconds int) string {
	if runtime.GOOS == "windows" {
		// ping waits a second between its tries.
		return "ping -n " + strconv.Itoa(seconds+1) + " 127.0.0.1"
	}
	return "sleep " + strconv.Itoa(seconds)
}

// trueCommand is a command line that exits at once with 0, and
// falseCommand one that exits at once with 1.
func trueCommand() string {
	if runtime.GOOS == "windows" {
		return "cmd /c exit 0"
	}
	return "true"
}

func falseCommand() string {
	if runtime.GOOS == "windows" {
		return "cmd /c exit 1"
	}
	return "false"
}

// saysFolder is what the local shell prints to say it is in dir. A
// shell on Unix sends OSC 7 with a file URL. cmd.exe sends OSC 9;9
// with the path as it is, since its prompt has no way to make a URL.
func saysFolder(dir string) string {
	if runtime.GOOS == "windows" {
		return "\x1b]9;9;" + dir + "\x1b\\"
	}
	return "\x1b]7;" + (&url.URL{Scheme: "file", Host: "localhost", Path: filepath.ToSlash(dir)}).String() + "\x07"
}

// widen gives pane id a screen wide enough for a long path on one row.
// A Windows temporary folder is long enough to wrap at 80 columns, and
// each row of the screen reads as a line of its own.
func widen(a *app, id string) { a.shells.Get(id).Resize(240, screen.Rows) }
