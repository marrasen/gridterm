package agent

import (
	"strings"
	"unicode"
)

// CleanSecretAsk cuts an agent's own words, asking for a secret, down to
// one plain line for the window to show.
//
// It is the agent's text on the user's screen, so it carries nothing
// that could draw somewhere else or pretend to be the window talking.
func CleanSecretAsk(what string) string {
	var out strings.Builder
	shown := 0
	for _, r := range what {
		switch {
		case r == '\n' || r == '\t' || r == '\r':
			out.WriteByte(' ')
		case r < ' ' || r == 0x7f:
			// Dropped: an escape sequence in it would draw.
		case r == '"':
			// The quotes around it are the window's, and a quote inside
			// would look like the end of them.
			out.WriteByte('\'')
		case unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cs, r) || unicode.Is(unicode.Co, r):
			// Zero-width marks, direction overrides and the like: they
			// take no room and change how the rest reads.
		case r == '-' && strings.HasSuffix(out.String(), "-"):
			// Two dashes are how one of the window's own remarks opens
			// and closes, and the agent's words are not one.
		default:
			out.WriteRune(r)
		}
		if shown++; shown >= MostSecretWords {
			break
		}
	}
	return out.String()
}

// MostSecretWords is how many characters of an agent's asking go on the
// screen. Enough to name what is wanted, short enough that the line it
// sits in is still read.
const MostSecretWords = 60
