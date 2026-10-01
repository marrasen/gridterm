package remote

import (
	"bufio"
	"os"
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

// ReadSSHConfig reads the machines an OpenSSH client config at path
// names, and the files it includes, in the order they come.
//
// Only Host blocks naming a machine count: a pattern with * or ? in it,
// or a negated one, names no one machine, and a Match block's
// conditions are not followed. Within a block the first value of a
// setting is the one kept, as ssh keeps it.
func ReadSSHConfig(path string) ([]SSHConfigHost, error) {
	var out []SSHConfigHost
	byAlias := map[string]int{}
	err := readSSHConfig(path, 0, &out, byAlias)
	return out, err
}

// sshConfigDepth is how deep Include goes, as a loop of includes would
// otherwise never end.
const sshConfigDepth = 8

func readSSHConfig(path string, depth int, out *[]SSHConfigHost, byAlias map[string]int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	// The blocks the lines go to now: indexes into out, none outside a
	// Host block or inside a Match one.
	var in []int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, args := sshConfigLine(sc.Text())
		if key == "" || len(args) == 0 {
			continue
		}
		switch key {
		case "host":
			in = in[:0]
			for _, pat := range args {
				if strings.ContainsAny(pat, "*?!") {
					continue
				}
				i, ok := byAlias[strings.ToLower(pat)]
				if !ok {
					i = len(*out)
					byAlias[strings.ToLower(pat)] = i
					*out = append(*out, SSHConfigHost{Alias: pat})
				}
				in = append(in, i)
			}
			continue
		case "match":
			in = in[:0]
			continue
		case "include":
			if depth >= sshConfigDepth {
				continue
			}
			for _, pat := range args {
				pat = expandHome(pat)
				if !filepath.IsAbs(pat) {
					pat = filepath.Join(filepath.Dir(path), pat)
				}
				matches, _ := filepath.Glob(pat)
				for _, m := range matches {
					_ = readSSHConfig(m, depth+1, out, byAlias)
				}
			}
			continue
		}
		for _, i := range in {
			h := &(*out)[i]
			v := args[0]
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
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}
