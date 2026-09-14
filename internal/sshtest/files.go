package sshtest

import (
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
	if s.noSFTP {
		// A machine that will not do SFTP, which is what an sshd with the
		// subsystem turned off looks like.
		return false
	}
	server, err := sftp.NewServer(ch)
	if err != nil {
		return false
	}
	s.mu.Lock()
	s.sftps++
	s.mu.Unlock()

	go func() {
		// Serve returns when the client closes the session, which is the
		// end of this channel.
		_ = server.Serve()
		_ = server.Close()
		_ = ch.Close()
	}()
	return true
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
