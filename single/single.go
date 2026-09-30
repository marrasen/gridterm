// Package single keeps one kakel running for a user: a kakel started
// while another runs hands its command line to that one, which opens a
// window for it, and ends.
//
// The one running listens on the loopback address, on a port it writes
// in a file only the user can read, with a random token that a
// handover has to carry. Nothing else on the machine can open windows
// through it without reading that file.
package single

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// File is where the one running says where it listens, in kakel's
// directory.
const File = "running.json"

// Handover is a command line handed to the one running: the arguments,
// and the folder it was started in.
type Handover struct {
	Args []string `json:"args"`
	Dir  string   `json:"dir"`
}

// wire is what a handover sends: the handover, and the token.
type wire struct {
	Token string `json:"token"`
	Handover
}

// running is what the file says.
type running struct {
	Addr  string `json:"addr"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

// handTime is how long a handover waits for the one running to answer.
const handTime = 3 * time.Second

// Hand hands h to the kakel running for dir, and reports whether one
// took it. With none running, or one that does not answer, it reports
// false, and the caller runs as the one.
func Hand(dir string, h Handover) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		return false, nil
	}
	var r running
	if json.Unmarshal(raw, &r) != nil || r.Addr == "" {
		return false, nil
	}
	conn, err := net.DialTimeout("tcp", r.Addr, handTime)
	if err != nil {
		// Gone without taking the file with it.
		return false, nil
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(handTime))
	msg, err := json.Marshal(wire{Token: r.Token, Handover: h})
	if err != nil {
		return false, err
	}
	if _, err := conn.Write(append(msg, '\n')); err != nil {
		return false, nil
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "ok\n" {
		return false, fmt.Errorf("the kakel already running did not take the command line: %q", line)
	}
	return true, nil
}

// Listen makes this the kakel running for dir, and sends what later
// ones hand it on the channel, until ctx ends. The file goes when it
// does, unless another has written it since.
func Listen(ctx context.Context, dir string) (<-chan Handover, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	r := running{Addr: l.Addr().String(), Token: hex.EncodeToString(token), PID: os.Getpid()}
	raw, err := json.Marshal(r)
	if err != nil {
		_ = l.Close()
		return nil, err
	}
	path := filepath.Join(dir, File)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		_ = l.Close()
		return nil, err
	}
	// Written whole, then moved into place, so a kakel starting reads
	// all of it or none.
	part := path + ".part"
	if err := os.WriteFile(part, raw, 0o600); err != nil {
		_ = l.Close()
		return nil, err
	}
	if err := os.Rename(part, path); err != nil {
		_ = l.Close()
		return nil, err
	}
	out := make(chan Handover, 4)
	go func() {
		<-ctx.Done()
		_ = l.Close()
		if now, err := os.ReadFile(path); err == nil && string(now) == string(raw) {
			_ = os.Remove(path)
		}
	}()
	go func() {
		defer close(out)
		for {
			conn, err := l.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			go serve(ctx, conn, r.Token, out)
		}
	}()
	return out, nil
}

// serve takes one handover, carrying the token, and answers it.
func serve(ctx context.Context, conn net.Conn, token string, out chan<- Handover) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(handTime))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	var w wire
	if json.Unmarshal(line, &w) != nil || subtle.ConstantTimeCompare([]byte(w.Token), []byte(token)) != 1 {
		return
	}
	select {
	case out <- w.Handover:
		_, _ = conn.Write([]byte("ok\n"))
	case <-ctx.Done():
	}
}
