package newfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// noHardLinks makes this a filesystem that refuses every hard link, the
// way FAT32 and exFAT do, for as long as the test runs.
func noHardLinks(t *testing.T) {
	t.Helper()
	was := link
	link = func(string, string) error {
		return &os.LinkError{Op: "link", Err: syscall.ENOTSUP}
	}
	t.Cleanup(func() { link = was })
}

// onlyThere says the directory holds the names given and nothing else:
// no file of the write's own is left behind.
func onlyThere(t *testing.T, dir string, names ...string) {
	t.Helper()
	got, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the directory: %v", err)
	}
	var have []string
	for _, e := range got {
		have = append(have, e.Name())
	}
	if len(have) != len(names) {
		t.Fatalf("the directory holds %v, want %v", have, names)
	}
	for i := range names {
		if have[i] != names[i] {
			t.Fatalf("the directory holds %v, want %v", have, names)
		}
	}
}

func eachWay(t *testing.T, run func(t *testing.T)) {
	t.Run("hard links", run)
	t.Run("no hard links", func(t *testing.T) {
		noHardLinks(t)
		run(t)
	})
}

// A file that is not there is written whole, with the mode asked for.
func TestANewFileIsWritten(t *testing.T) {
	eachWay(t, func(t *testing.T) {
		dir := t.TempDir()
		at := filepath.Join(dir, "host_key")

		if err := Write(at, []byte("the key"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := os.ReadFile(at)
		if err != nil || string(got) != "the key" {
			t.Fatalf("it reads %q, %v", got, err)
		}
		onlyThere(t, dir, "host_key")
	})
}

// A file that is there is left as it is, and the answer says so.
func TestAFileThatIsThereIsNotWrittenOver(t *testing.T) {
	eachWay(t, func(t *testing.T) {
		dir := t.TempDir()
		at := filepath.Join(dir, "host_key")
		if err := os.WriteFile(at, []byte("another window's key"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		err := Write(at, []byte("the key"), 0o600)
		if !errors.Is(err, fs.ErrExist) {
			t.Fatalf("writing over it said %v, want ErrExist", err)
		}
		if got, _ := os.ReadFile(at); string(got) != "another window's key" {
			t.Errorf("the file there reads %q, want it untouched", got)
		}
		onlyThere(t, dir, "host_key")
	})
}

// A filesystem that has no hard links gets the file all the same. It used
// to be refused every time: the link failed with an error that was not
// "it is there", and a portable copy on a USB stick could never serve.
func TestAFilesystemWithoutHardLinksGetsTheFile(t *testing.T) {
	noHardLinks(t)
	dir := t.TempDir()
	at := filepath.Join(dir, "host_key")

	if err := Write(at, []byte("the key"), 0o600); err != nil {
		t.Fatalf("write where there are no hard links: %v", err)
	}
	if got, _ := os.ReadFile(at); string(got) != "the key" {
		t.Errorf("it reads %q", got)
	}
}

// A directory that is not there is not made: the caller says where
// files go.
func TestNoDirectoryIsMade(t *testing.T) {
	at := filepath.Join(t.TempDir(), "missing", "host_key")

	if err := Write(at, []byte("the key"), 0o600); err == nil {
		t.Fatal("a file was written into a directory that is not there")
	}
}

// An empty file left at the name long enough ago is a claim whose writer
// never came back, and is written over. One made a moment ago is another
// writer's, part way through, and is not.
func TestAnEmptyFileIsAClaimOnlyOnceItIsStale(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, "host_key")
	if err := os.WriteFile(at, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := Write(at, []byte("the key"), 0o600); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("a fresh claim was written over: %v", err)
	}

	long := time.Now().Add(-time.Minute)
	if err := os.Chtimes(at, long, long); err != nil {
		t.Fatalf("age it: %v", err)
	}
	if err := Write(at, []byte("the key"), 0o600); err != nil {
		t.Fatalf("a stale claim was not written over: %v", err)
	}
	if got, _ := os.ReadFile(at); string(got) != "the key" {
		t.Errorf("it reads %q", got)
	}
}

// A claim far in the future -- FAT keeps local time, so a stick written
// on Windows reads hours ahead on Linux -- is stale too, not one being
// written now.
func TestAClaimFromAnotherClockIsStale(t *testing.T) {
	at := filepath.Join(t.TempDir(), "host_key")
	if err := os.WriteFile(at, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	ahead := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(at, ahead, ahead); err != nil {
		t.Fatalf("age it: %v", err)
	}

	if err := Write(at, []byte("the key"), 0o600); err != nil {
		t.Fatalf("a claim hours ahead was not written over: %v", err)
	}
}
