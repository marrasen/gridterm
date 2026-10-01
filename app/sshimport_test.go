package app

import (
	"os"
	"path/filepath"
	"testing"
)

// The machines the SSH config names are saved, a route's server before
// the machine reached through it; one saved already is left alone.
func TestTheSSHConfigIsImported(t *testing.T) {
	a, _ := dialApp(t)
	conf := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(conf, []byte(`Host build
  HostName build.local
  ProxyJump jump
Host jump
  HostName jump.example.com
  User ci
Host srv
  HostName elsewhere.example.com
Host lost
  ProxyJump nowhere
`), 0o600); err != nil {
		t.Fatal(err)
	}
	was := sshConfigPath
	sshConfigPath = func() (string, error) { return conf, nil }
	t.Cleanup(func() { sshConfigPath = was })
	before, _ := a.book.Lookup("srv")
	a.handle(ImportSSHConfig{})
	build, ok := a.book.Lookup("build")
	jump, ok2 := a.book.Lookup("jump")
	if !ok || !ok2 || build.Via != jump.ID || jump.User != "ci" || build.Address != "build.local" {
		t.Fatalf("imported build %+v and jump %+v", build, jump)
	}
	if lost, ok := a.book.Lookup("lost"); !ok || lost.Via != "" {
		t.Fatalf("a machine through one not named is %+v, %v", lost, ok)
	}
	if after, _ := a.book.Lookup("srv"); after.Address != before.Address {
		t.Fatalf("a server saved already was changed to %+v", after)
	}
	if len(a.st.Saved) != 4 {
		t.Fatalf("%d servers saved, want 4", len(a.st.Saved))
	}
	if n := a.st.Notices[len(a.st.Notices)-1]; n.Title != "Imported 3 servers from the SSH config" {
		t.Fatalf("said %+v", n)
	}
}
