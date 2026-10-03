//go:build windows && !amd64

package conpty

import "errors"

// bundled reports that kakel carries OpenConsole for x64 alone.
func bundled() (*host, error) {
	return nil, errors.New("kakel carries OpenConsole for x64 only")
}
