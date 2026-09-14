package remote

import (
	"fmt"
	"sync"

	"github.com/pkg/sftp"
)

// Files is an SFTP session on a connection.
//
// It rides on the connection: closing the connection closes it, and the
// file browser and every transfer going through it stop.
type Files struct {
	conn   *Conn
	client *sftp.Client

	closeOnce sync.Once
	closeErr  error
}

// Files starts an SFTP session on the connection.
//
// One per call rather than one shared: each has its own channel, and two
// browser panes on one machine should not wait on each other's reads.
func (c *Conn) Files() (*Files, error) {
	// Asked before a channel is opened, so a closed connection says so
	// rather than reporting whatever the dead transport failed with.
	if c.isClosing() {
		return nil, fmt.Errorf("remote: %s: %w", c, ErrClosed)
	}
	client, err := sftp.NewClient(c.client)
	if err != nil {
		return nil, fmt.Errorf("remote: start SFTP on %s: %w", c, err)
	}

	f := &Files{conn: c, client: client}
	if err := c.register(f); err != nil {
		_ = f.closeRider()
		return nil, err
	}
	return f, nil
}

// Client is the SFTP client itself, for whatever works on files.
func (f *Files) Client() *sftp.Client { return f.client }

// On returns the connection it runs over.
func (f *Files) On() *Conn { return f.conn }

// Close ends the session and lets go of the connection's record of it.
func (f *Files) Close() error {
	err := f.closeRider()
	f.conn.drop(f)
	return err
}

// closeRider is Close without the deregistering, for a connection that
// is closing its riders and will throw the whole record away anyway.
func (f *Files) closeRider() error {
	f.closeOnce.Do(func() {
		if err := f.client.Close(); err != nil {
			f.closeErr = fmt.Errorf("remote: close SFTP on %s: %w", f.conn, err)
		}
	})
	return f.closeErr
}
