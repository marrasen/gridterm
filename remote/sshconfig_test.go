package remote

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSSHConfigTakesTheMachinesItNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ssh := filepath.Join(home, ".ssh")
	extra := filepath.Join(ssh, "conf.d", "work")
	if err := os.MkdirAll(filepath.Dir(extra), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra, []byte("Host db\n  HostName 10.0.0.12\n  Port 2222\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(ssh, "config")
	// A BOM, as a Windows editor writes, before the first line.
	if err := os.WriteFile(conf, []byte("\ufeff"+`# a comment
Include conf.d/*
Host web web-alias
  HostName web.example.com
  User deploy
  User second
  IdentityFile "/keys/web key"
Host build.corp
  hostname=build.local
Host *.corp
  ProxyJump ci@web:2200,other
  User corpuser
Match host foo
  User matched
Host *
  User everyone
Host plain !nobody
`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSSHConfig(conf)
	if err != nil {
		t.Fatal(err)
	}
	// As ssh takes them: every block that applies, in order, the first
	// value of each setting kept.
	want := []SSHConfigHost{
		{Alias: "db", HostName: "10.0.0.12", Port: 2222, User: "everyone"},
		{Alias: "web", HostName: "web.example.com", User: "deploy", Identity: "/keys/web key"},
		{Alias: "web-alias", HostName: "web.example.com", User: "deploy", Identity: "/keys/web key"},
		{Alias: "build.corp", HostName: "build.local", Jump: "web", User: "corpuser"},
		{Alias: "plain", User: "everyone"},
	}
	if len(got) != len(want) {
		t.Fatalf("read %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %+v, want %+v", i, got[i], want[i])
		}
	}
	if h := got[4].Host(); h.Address != "plain" || h.Name != "plain" {
		t.Errorf("a host with no HostName is saved as %+v", h)
	}
}
