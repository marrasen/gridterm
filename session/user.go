package session

import (
	"errors"
	"os"
	"os/user"
)

// currentUser returns the local account name, used as the SSH user when
// none was given.
func currentUser() (string, error) {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username, nil
	}
	// os/user needs cgo or a readable passwd database; the environment
	// is a reasonable second source.
	for _, k := range []string{"USER", "USERNAME", "LOGNAME"} {
		if v := os.Getenv(k); v != "" {
			return v, nil
		}
	}
	return "", errors.New("no current user")
}
