package serve

import (
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

// PicturePutter takes a picture a client sent and puts it somewhere the
// programs on this machine can paste it, which is the system clipboard.
//
// It is called from the goroutine serving that client, so an
// implementation that touches a window has to hand the work over to
// whatever draws. It is called with the bytes of a PNG.
//
// A nil one means this window does not take pictures, and a client that
// sends one is told so.
type PicturePutter func(png []byte) error

// runClipboard reads a picture from a client and hands it to the window.
//
// The answer goes back down the channel: nothing at all when the picture
// landed, and the reason when it did not. A client reads to the end and
// takes what it read as the failure.
func (s *Server) runClipboard(ctx context.Context, nch ssh.NewChannel) {
	if s.cfg.Picture == nil {
		_ = nch.Reject(ssh.Prohibited, "this gridterm does not take pictures")
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		s.onError(fmt.Errorf("serve: take a picture: %w", err))
		return
	}
	defer func() { _ = ch.Close() }()
	go ssh.DiscardRequests(reqs)

	if err := s.takePicture(ctx, ch); err != nil {
		s.onError(fmt.Errorf("serve: a picture from a client: %w", err))
		// Said down the channel as well, because the client is waiting
		// to hear and has nowhere else to learn it from.
		_, _ = io.WriteString(ch, err.Error())
	}
}

// takePicture reads the bytes and puts them on the clipboard.
func (s *Server) takePicture(ctx context.Context, ch io.Reader) error {
	// One byte past the limit is read on purpose: a read that stopped
	// exactly at it cannot tell a picture that fits from one that was
	// cut off.
	raw, err := io.ReadAll(io.LimitReader(ch, mostClipboardBytes+1))
	if err != nil {
		return fmt.Errorf("read it: %w", err)
	}
	if len(raw) == 0 {
		return errors.New("the picture was empty")
	}
	if len(raw) > mostClipboardBytes {
		return fmt.Errorf("the picture is larger than the %d bytes this window takes", mostClipboardBytes)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.cfg.Picture(raw)
}
