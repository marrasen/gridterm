package vfs

import (
	"errors"
	"io"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// windowsTop answers the way gridterm's SFTP server on Windows does at
// the top: listing "/" means listing the drives, and it fails outright
// when one drive -- here e:, an empty card reader -- cannot be read.
// Asked about one at a time, the drives that are there answer.
type windowsTop struct{}

func (windowsTop) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	switch r.Method {
	case "List":
		if r.Filepath == "/" {
			return nil, errors.New(`CreateFile e:\: The device is not ready.`)
		}
	case "Stat", "Lstat":
		switch r.Filepath {
		case "/c:", "/d:":
			return infos{topInfo{name: `\`}}, nil
		case "/e:":
			return nil, errors.New(`CreateFile e:\: The device is not ready.`)
		}
	}
	return nil, os.ErrNotExist
}

type infos []os.FileInfo

func (l infos) ListAt(dst []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(dst, l[off:])
	if off+int64(n) >= int64(len(l)) {
		return n, io.EOF
	}
	return n, nil
}

// topInfo is the top of a drive, which calls itself by its path.
type topInfo struct{ name string }

func (i topInfo) Name() string       { return i.name }
func (i topInfo) Size() int64        { return 0 }
func (i topInfo) Mode() os.FileMode  { return os.ModeDir | 0o755 }
func (i topInfo) ModTime() time.Time { return time.Time{} }
func (i topInfo) IsDir() bool        { return true }
func (i topInfo) Sys() any           { return nil }

// pipe is one end of a connection made of two pipes.
type pipe struct {
	io.Reader
	io.WriteCloser
}

// aWindowsMachine is an SFTP filesystem on a server answering like
// windowsTop.
func aWindowsMachine(t *testing.T) *SFTP {
	t.Helper()
	toServer, fromClient := io.Pipe()
	toClient, fromServer := io.Pipe()
	h := sftp.InMemHandler()
	h.FileList = windowsTop{}
	server := sftp.NewRequestServer(pipe{toServer, fromServer}, h)
	go func() { _ = server.Serve() }()
	client, err := sftp.NewClientPipe(toClient, fromClient)
	if err != nil {
		t.Fatalf("start SFTP: %v", err)
	}
	// Both pipes are cut before the client is closed: each end waits
	// for the other to hang up, and nothing here would.
	t.Cleanup(func() {
		_ = fromServer.Close()
		_ = fromClient.Close()
		_ = client.Close()
	})
	return NewSFTP("nyli", "nyli", client, func() error { return nil })
}

// Listing the top of a Windows machine with a drive that is not ready
// lists the drives that are, rather than failing for all of them.
// Marcus went up from /C: on a gridterm connection and got "CreateFile
// e:\: The device is not ready." for the whole listing.
func TestADriveThatIsNotReadyLeavesTheOthersListed(t *testing.T) {
	f := aWindowsMachine(t)

	got, err := f.ReadDir("/")
	if err != nil {
		t.Fatalf("the top would not list: %v", err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
		if !e.IsDir() {
			t.Errorf("%s is listed as a file", e.Name)
		}
	}
	if !slices.Equal(names, []string{"c:", "d:"}) {
		t.Errorf("the top lists %v, want the two drives that answer", names)
	}
}

// Anything else that fails to list still fails: only the top has drives
// to ask about one at a time.
func TestAnotherDirectoryThatFailsStillFails(t *testing.T) {
	f := aWindowsMachine(t)

	if _, err := f.ReadDir("/c:/nowhere"); err == nil {
		t.Error("a directory that is not there listed")
	}
}
