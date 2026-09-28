package serve

import (
	"testing"

	"github.com/marrasen/kakel/internal/testhome"
)

// The tests run in a home of their own, so a test that writes where the
// user's files go writes there.
func TestMain(m *testing.M) { testhome.Main(m) }
