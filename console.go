package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/marrasen/gridterm/remote"
)

// stdin is read through one reader for the life of the process.
//
// A fresh bufio.Reader per prompt reads ahead and then throws the buffer
// away, so an answer typed before it was asked for would be swallowed --
// and the next prompt would wait for input that had already arrived.
var stdin = bufio.NewReader(os.Stdin)

// console is where prompts and notices are written. It is a variable so
// a test can read what a server's wording turned into on the way out.
var console io.Writer = os.Stderr

// consoleAsk answers a connection's questions on the terminal gridterm
// was started from.
//
// It exists for -ssh, which connects before the window opens: there is
// nowhere to draw a dialog yet, so the console is the only place left to
// ask. Connecting from inside the window uses askUser instead.
//
// The context is not watched. Nothing can cancel a read from the console
// on every platform, and there is no window yet to close.
type consoleAsk struct{}

func (consoleAsk) Passphrase(_ context.Context, keyfile string) (string, error) {
	return promptSecret("passphrase for " + keyfile + ": ")
}

func (consoleAsk) Password(_ context.Context, user, host string) (string, error) {
	return promptSecret("password for " + user + "@" + host + ": ")
}

func (consoleAsk) Question(_ context.Context, q remote.Question) ([]string, error) {
	// Named first, in our own words. Everything below is the server's,
	// and it is printed to a real terminal that obeys escape sequences.
	fmt.Fprintf(console, "%s@%s is asking:\n", q.User, q.Host)
	if q.Name != "" {
		fmt.Fprintln(console, plainly(q.Name))
	}
	if q.Instruction != "" {
		fmt.Fprintln(console, plainly(q.Instruction))
	}
	answers := make([]string, len(q.Prompts))
	for i, prompt := range q.Prompts {
		var err error
		if i < len(q.Echo) && q.Echo[i] {
			answers[i], err = promptLine(plainly(prompt))
		} else {
			answers[i], err = promptSecret(plainly(prompt))
		}
		if err != nil {
			return nil, err
		}
	}
	return answers, nil
}

// plainly strips what a terminal would act on rather than print.
//
// A server chooses the wording here, and it goes to the console gridterm
// was started from. Left alone, it could move that terminal's cursor,
// overwrite what was already printed, or set its title.
func plainly(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// TrustHostKey refuses.
//
// Before the window opens there is no dialog to show a fingerprint in,
// and a yes-or-no typed at a console that may not even be attached is
// not a decision worth recording. An unknown host is a hard failure
// here, exactly as it always was.
func (consoleAsk) TrustHostKey(_ context.Context, key remote.HostKey) (bool, error) {
	return false, fmt.Errorf(
		"%s is not in known_hosts (%s %s); connect once with ssh to record it, "+
			"or connect from inside the window, which can ask",
		key.Addr, key.Type(), key.Fingerprint())
}

// promptLine reads one line, for an answer the server said may be shown
// as it is typed.
func promptLine(prompt string) (string, error) {
	fmt.Fprint(console, prompt)
	line, err := stdin.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read the answer from the console "+
			"(start gridterm from a shell): %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
