package vfs

import (
	"errors"
	"fmt"
	"strings"
)

// PlainName refuses, saying why, a name that is not a name in a folder:
// none, a path, or one of the two that name folders themselves.
func PlainName(f FS, name string) error {
	switch {
	case name == "":
		return errors.New("it needs a name")
	case name == "." || name == "..":
		return fmt.Errorf("%q is not a name to use", name)
	case strings.ContainsRune(name, rune(f.Sep())), strings.ContainsRune(name, '/'):
		return fmt.Errorf("%q is a path, and a name is wanted", name)
	}
	return nil
}

// NameFree refuses, saying so, when folder at holds an entry named
// exactly name.
func NameFree(f FS, at, name string) error {
	entries, err := f.ReadDir(at)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name == name {
			return fmt.Errorf("%s is already there", name)
		}
	}
	return nil
}
