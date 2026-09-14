package sshtest

import (
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sftpRequest answers a request to start the SFTP subsystem.
//
// It runs the real server from github.com/pkg/sftp over the channel, on
// this machine's own filesystem, so a test drives the same protocol a
// client would meet on a real machine. The test decides what it can
// reach by only naming paths under its own temporary directory.
func (s *Server) sftpRequest(ch ssh.Channel, req *ssh.Request) bool {
	// The payload is a length-prefixed name: the subsystem being asked
	// for.
	name, _, ok := sshString(req.Payload)
	if !ok || name != "sftp" {
		return false
	}
	s.mu.Lock()
	refuse := s.noSFTP
	s.mu.Unlock()
	if refuse {
		// A machine that will not do SFTP, which is what an sshd with the
		// subsystem turned off looks like.
		return false
	}
	// Writes go through the server's own lock: x/crypto documents
	// concurrent writes to one channel as unsafe, and the request loop
	// writes to this one too.
	server, err := sftp.NewServer(locked{Channel: ch, mu: &s.writeMu})
	if err != nil {
		// The harness itself, not the machine refusing. Told apart,
		// because a test that fails for this reason would otherwise read
		// as a test of a machine without SFTP.
		panic("sshtest: could not start the SFTP server: " + err.Error())
	}

	go func() {
		// Serve returns when the client closes the session, and also on
		// a packet it could not read. Which it was is kept, so a test
		// can tell one from the other.
		err := server.Serve()
		s.mu.Lock()
		s.sftpErr = err
		s.mu.Unlock()
		select {
		case <-s.frozen:
			// A machine that stopped answering without hanging up: the
			// channel is left open, so whatever is waiting for it to
			// close waits for ever. Until the test ends.
			<-s.stopped
		default:
		}
		// Serve has already closed the connection it was given; this is
		// the channel underneath it.
		_ = ch.Close()
	}()

	s.mu.Lock()
	s.sftps++
	s.mu.Unlock()
	return true
}

// SFTPError returns why the last SFTP session ended, which is nil for
// one the client closed.
func (s *Server) SFTPError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sftpErr
}

// Freeze makes the server leave its SFTP sessions open without
// answering, which is what a machine that has dropped off the network
// looks like from the other end: nothing arrives, and nothing is closed
// either.
//
// Everything else the server does carries on, so a test can tell a
// wedged session from a connection that has gone.
func (s *Server) Freeze() {
	s.freezeOnce.Do(func() { close(s.frozen) })
}

// locked is a channel whose writes are serialised with the rest of the
// server's.
type locked struct {
	ssh.Channel
	mu *sync.Mutex
}

func (l locked) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Channel.Write(p)
}

// RefuseSFTP makes the server turn down a request to start SFTP, which
// is what a machine with the subsystem turned off does.
func (s *Server) RefuseSFTP() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.noSFTP = true
}

// SFTPs returns how many SFTP sessions the server has started, so a test
// can tell one session from several.
func (s *Server) SFTPs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sftps
}
