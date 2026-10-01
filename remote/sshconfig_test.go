package remote

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSSHConfigTakesTheMachinesItNames(t *testing.T) {
	dir := t.TempDir()
	extra := filepath.Join(dir, "conf.d", "work")
	if err := os.MkdirAll(filepath.Dir(extra), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(extra, []byte("Host db\n  HostName 10.0.0.12\n  Port 2222\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "config")
	if err := os.WriteFile(conf, []byte(`# a comment
Include conf.d/*
Host *
  User everyone
Host web web-alias
  HostName web.example.com
  User deploy
  User second
  IdentityFile "/keys/web key"
Host build
  hostname=build.local
  ProxyJump ci@web:2200,other
Host *.internal !bad
  User nobody
Match host foo
  User matched
Host plain
`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSSHConfig(conf)
	if err != nil {
		t.Fatal(err)
	}
	want := []SSHConfigHost{
		{Alias: "db", HostName: "10.0.0.12", Port: 2222},
		{Alias: "web", HostName: "web.example.com", User: "deploy", Identity: "/keys/web key"},
		{Alias: "web-alias", HostName: "web.example.com", User: "deploy", Identity: "/keys/web key"},
		{Alias: "build", HostName: "build.local", Jump: "web"},
		{Alias: "plain"},
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
