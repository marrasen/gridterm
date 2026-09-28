package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/marrasen/kakel/internal/testhome"
)

// The tests run in a home of their own, so a test that writes a
// setting, a key or a skill writes it there. They copy to and paste
// from a clipboard of their own, so the user's stays as it was. The
// shells they start find testAnswerVar set; see saysAnswer.
func TestMain(m *testing.M) {
	onAClipboardOfTheirOwn()
	if err := os.Setenv(testAnswerVar, testAnswer); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testhome.Main(m)
}
