package main

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/term"
)

// promptSecret asks for a password or passphrase on the controlling
// terminal, with echo off.
//
// This happens before the window opens, which is the only reason it can
// use stdin at all. Once gridterm is drawing its own grid there is no
// console to prompt on, so an SSH connection that needs a secret has to
// establish it up front.
func promptSecret(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("no terminal to prompt on: " +
			"use an ssh agent, or start gridterm from a shell")
	}
	fmt.Fprint(os.Stderr, prompt)
	secret, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(secret), nil
}
