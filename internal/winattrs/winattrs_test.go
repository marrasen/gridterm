package winattrs_test

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/marrasen/kakel/internal/winattrs"
	"github.com/marrasen/kakel/vfs"
	"github.com/pkg/sftp"
)

// A folder listed and an item stated through the proxy carry the cloud
// attributes of the items that have any, and nothing for the others.
func TestTheCloudAttributesComeWithTheReplies(t *testing.T) {
	dir := t.TempDir()
	// The same names in two folders, kept differently: the attributes
	// are looked up by the whole path.
	for _, sub := range []string{"", "other"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"here.txt", "online.txt", "pinned.txt"} {
			if err := os.WriteFile(filepath.Join(dir, sub, name), []byte(name), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	slashed := filepath.ToSlash(dir)
	var mu sync.Mutex
	followed := map[string]bool{}
	lookup := func(_, p string, follow bool) (uint32, bool) {
		// As the server names it, /C:/x on Windows, as the test does.
		p = winattrs.Resolve("", p)
		mu.Lock()
		followed[p] = follow
		mu.Unlock()
		switch p {
		case slashed + "/online.txt":
			return winattrs.RecallOnDataAccess | 0x20, true
		case slashed + "/pinned.txt", slashed + "/other/online.txt":
			return winattrs.Pinned, true
		}
		return 0x20, true
	}
	here, there := net.Pipe()
	server, err := sftp.NewServer(winattrs.Proxy(there, dir, lookup))
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })
	client, err := sftp.NewClientPipe(here, here)
	if err != nil {
		t.Fatal(err)
	}
	f := vfs.NewSFTP("win", nil, client, client.Close)
	t.Cleanup(func() { _ = f.Close() })

	entries, err := f.ReadDir(slashed)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]uint32{}
	for _, e := range entries {
		got[e.Name] = e.Attrs
	}
	if got["here.txt"] != 0 || got["online.txt"] != winattrs.RecallOnDataAccess || got["pinned.txt"] != winattrs.Pinned {
		t.Fatalf("the listing carries %v", got)
	}
	if !winattrs.OnlineOnly(got["online.txt"]) || winattrs.OnlineOnly(got["pinned.txt"]) {
		t.Fatal("online only is not told from pinned")
	}
	others, err := f.ReadDir(slashed + "/other")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range others {
		if want := map[string]uint32{"online.txt": winattrs.Pinned}[e.Name]; e.Attrs != want {
			t.Fatalf("in the other folder, %s carries %#x", e.Name, e.Attrs)
		}
	}
	e, err := f.Stat(slashed + "/online.txt")
	if err != nil || e.Attrs != winattrs.RecallOnDataAccess || e.Size != int64(len("online.txt")) {
		t.Fatalf("the stat is %+v, %v", e, err)
	}
	info, err := client.Stat(slashed + "/online.txt")
	if err != nil || vfs.WinAttrs(info) != winattrs.RecallOnDataAccess {
		t.Fatalf("a stat that follows links carries %v, %v", info, err)
	}
	mu.Lock()
	follows := followed[winattrs.Resolve("", slashed+"/online.txt")]
	mu.Unlock()
	if !follows {
		t.Fatal("a stat that follows links looked up the link")
	}
	// What is read is read as it is.
	r, err := f.Open(slashed + "/here.txt")
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 20)
	n, _ := r.Read(b)
	_ = r.Close()
	if string(b[:n]) != "here.txt" {
		t.Fatalf("read %q", b[:n])
	}
}

// A path the server is asked about is its own: from home when relative,
// and a Windows drive without the slash before it.
func TestResolve(t *testing.T) {
	for _, c := range [][3]string{
		{"C:/Users/me", "/C:/Users/me/x", "C:/Users/me/x"},
		{"C:/Users/me", "Documents", "C:/Users/me/Documents"},
		{"/home/me", "/etc", "/etc"},
		{"/home/me", "x", "/home/me/x"},
		{"/", "/c:", "c:/"},
	} {
		if got := winattrs.Resolve(c[0], c[1]); got != c[2] {
			t.Errorf("Resolve(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
