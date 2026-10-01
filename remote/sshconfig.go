package remote

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// SSHConfigHost is one machine an OpenSSH client config names, as much
// of it as a saved server holds: the alias it goes by, the host it
// reaches, the user, the port, the first key file, and the first hop of
// its ProxyJump, by its alias.
type SSHConfigHost struct {
	Alias, HostName, User string
	Port                  int
	Identity, Jump        string
}

// Host is h as a saved server: named after its alias, reached at its
// HostName, or at the alias when it has none, through Jump.
func (h SSHConfigHost) Host() Host {
	s := Host{Name: h.Alias, Address: h.HostName, User: h.User, Port: h.Port, Via: h.Jump}
	if s.Address == "" {
		s.Address = h.Alias
	}
	if h.Identity != "" {
		s.Identities = []string{h.Identity}
	}
	return s
}

// sshBlock is one Host block: the patterns it applies to, and its
// settings in the order they come.
type sshBlock struct {
	patterns []string
	settings [][2]string
}

// matches reports whether the block applies to alias, as ssh decides:
// one of its patterns matches, and none of its negated ones does.
func (b sshBlock) matches(alias string) bool {
	alias = strings.ToLower(alias)
	hit := false
	for _, p := range b.patterns {
		neg := strings.HasPrefix(p, "!")
		p = strings.ToLower(strings.TrimPrefix(p, "!"))
		ok, err := path.Match(p, alias)
		if err != nil || !ok {
			continue
		}
		if neg {
			return false
		}
		hit = true
	}
	return hit
}

// ReadSSHConfig reads the machines an OpenSSH client config at path
// names, in the order they come, through the files it includes.
//
// A machine is a Host pattern with no * or ? in it, which names one
// alias. Its settings are taken as ssh takes them: from every block that
// applies to it, wildcard blocks such as Host * included, in the order
// they come, the first value of each setting the one kept. A Match
// block's conditions are not followed, and its settings are not taken.
// A relative Include is read from ~/.ssh, as ssh reads it for a user's
// config.
func ReadSSHConfig(file string) ([]SSHConfigHost, error) {
	var blocks []sshBlock
	var aliases []string
	seen := map[string]bool{}
	if err := readSSHBlocks(file, 0, &blocks, &aliases, seen); err != nil {
		return nil, err
	}
	out := make([]SSHConfigHost, 0, len(aliases))
	for _, alias := range aliases {
		h := SSHConfigHost{Alias: alias}
		for _, b := range blocks {
			if b.matches(alias) {
				for _, kv := range b.settings {
					h.set(kv[0], kv[1])
				}
			}
		}
		out = append(out, h)
	}
	return out, nil
}

// set takes setting key's value v, when it has none yet.
func (h *SSHConfigHost) set(key, v string) {
	switch key {
	case "hostname":
		if h.HostName == "" && !strings.Contains(v, "%") {
			h.HostName = v
		}
	case "user":
		if h.User == "" && !strings.Contains(v, "%") {
			h.User = v
		}
	case "port":
		if n, err := strconv.Atoi(v); err == nil && h.Port == 0 && n > 0 && n < 65536 {
			h.Port = n
		}
	case "identityfile":
		if h.Identity == "" && !strings.Contains(v, "%") {
			h.Identity = expandHome(v)
		}
	case "proxyjump":
		if h.Jump == "" && !strings.EqualFold(v, "none") {
			h.Jump = jumpHost(v)
		}
	}
}

// sshConfigDepth is how deep Include goes, as a loop of includes would
// otherwise never end.
const sshConfigDepth = 8

// readSSHBlocks reads file's Host blocks into blocks, and the aliases
// they name into aliases, following Include.
func readSSHBlocks(file string, depth int, blocks *[]sshBlock, aliases *[]string, seen map[string]bool) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	// The block lines go to: -1 before any Host line, and inside a Match
	// one, whose settings are not taken.
	at := -1
	first := true
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if first {
			// A config saved by a Windows editor may start with a BOM.
			line = strings.TrimPrefix(line, "\ufeff")
			first = false
		}
		key, args := sshConfigLine(line)
		if key == "" || len(args) == 0 {
			continue
		}
		switch key {
		case "host":
			*blocks = append(*blocks, sshBlock{patterns: args})
			at = len(*blocks) - 1
			for _, p := range args {
				if strings.ContainsAny(p, "*?!") || seen[strings.ToLower(p)] {
					continue
				}
				seen[strings.ToLower(p)] = true
				*aliases = append(*aliases, p)
			}
		case "match":
			at = -1
		case "include":
			if depth >= sshConfigDepth {
				continue
			}
			for _, pat := range args {
				pat = expandHome(pat)
				if !filepath.IsAbs(pat) {
					if home, err := os.UserHomeDir(); err == nil {
						pat = filepath.Join(home, ".ssh", pat)
					}
				}
				matches, _ := filepath.Glob(pat)
				for _, m := range matches {
					_ = readSSHBlocks(m, depth+1, blocks, aliases, seen)
				}
			}
		default:
			if at >= 0 {
				(*blocks)[at].settings = append((*blocks)[at].settings, [2]string{key, args[0]})
			}
		}
	}
	return sc.Err()
}

// sshConfigLine is a config line's keyword, lower case, and its
// arguments, quotes taken off; an empty keyword for a blank line or a
// comment. A keyword is separated from its arguments by space or by =.
func sshConfigLine(line string) (string, []string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil
	}
	end := strings.IndexAny(line, " \t=")
	if end < 0 {
		return strings.ToLower(line), nil
	}
	key := strings.ToLower(line[:end])
	rest := strings.TrimLeft(line[end:], " \t")
	rest = strings.TrimLeft(strings.TrimPrefix(rest, "="), " \t")
	var args []string
	for rest != "" {
		if rest[0] == '"' {
			if j := strings.IndexByte(rest[1:], '"'); j >= 0 {
				args = append(args, rest[1:1+j])
				rest = strings.TrimLeft(rest[2+j:], " \t")
				continue
			}
		}
		j := strings.IndexAny(rest, " \t")
		if j < 0 {
			args = append(args, rest)
			break
		}
		args = append(args, rest[:j])
		rest = strings.TrimLeft(rest[j:], " \t")
	}
	return key, args
}

// jumpHost is the first hop of a ProxyJump, user@host:port,..., as the
// host alone, which names the server to go through.
func jumpHost(v string) string {
	v, _, _ = strings.Cut(v, ",")
	if _, h, ok := strings.Cut(v, "@"); ok {
		v = h
	}
	if strings.HasPrefix(v, "[") {
		if j := strings.Index(v, "]"); j > 0 {
			return v[1:j]
		}
	}
	if h, _, ok := strings.Cut(v, ":"); ok {
		v = h
	}
	return v
}

// expandHome is path with a leading ~ the home folder.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
