package main

import (
	"testing"

	"github.com/marrasen/kakel/internal/testhome"
)

// The tests run in a home of their own, so a test that writes a
// setting, a key or a skill writes it there. They copy to and paste
// from a clipboard of their own, so the user's stays as it was.
func TestMain(m *testing.M) {
	onAClipboardOfTheirOwn()
	testhome.Main(m)
}
