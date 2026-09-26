// Package fonts holds the faces compiled into gridterm that need no
// file on the machine.
package fonts

import _ "embed" // for the faces below

// DOS is the IBM VGA 8x16 character set, compiled in so a theme can ask
// for it on a machine that has no such font installed. See README.md
// for where it came from and what it may be used for.
//
//go:embed PxPlus_IBM_VGA8.ttf
var DOS []byte
