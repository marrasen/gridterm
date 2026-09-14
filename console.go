package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/marrasen/gridterm/remote"
)

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
	if q.Instruction != "" {
		fmt.Fprintln(os.Stderr, q.Instruction)
	}
	answers := make([]string, len(q.Prompts))
	for i, prompt := range q.Prompts {
		var err error
		if i < len(q.Echo) && q.Echo[i] {
			answers[i], err = promptLine(prompt)
		} else {
			answers[i], err = promptSecret(prompt)
		}
		if err != nil {
			return nil, err
		}
	}
	return answers, nil
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
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", errors.New("no terminal to prompt on: " +
			"start gridterm from a shell")
	}
	return strings.TrimRight(line, "\r\n"), nil
}
