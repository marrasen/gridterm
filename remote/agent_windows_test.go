package remote

import (
	"strings"
	"testing"
)

// The Windows named pipe namespace is \\.\pipe\. One backslash instead
// of two is a drive-rooted path that opens nothing, and it fails the way
// a missing agent fails, so nothing else notices.
func TestAgentPipeIsInThePipeNamespace(t *testing.T) {
	const want = `\\.\pipe\`
	if !strings.HasPrefix(openSSHAgentPipe, want) {
		t.Fatalf("openSSHAgentPipe = %q, want it to start with %q", openSSHAgentPipe, want)
	}
}
