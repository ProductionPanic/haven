// Package sshx builds ssh invocations for stored hosts.
package sshx

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/ProductionPanic/haven/v2/internal/store"
)

// Options returns the ssh options for h (port, identity, jump host and
// extra args), without the destination.
func Options(h store.Host) ([]string, error) {
	var args []string
	if h.Port != 0 && h.Port != 22 {
		args = append(args, "-p", strconv.Itoa(h.Port))
	}
	if h.IdentityFile != "" {
		args = append(args, "-i", h.IdentityFile)
	}
	if h.JumpHost != "" {
		args = append(args, "-J", h.JumpHost)
	}
	extra, err := SplitArgs(h.ExtraArgs)
	if err != nil {
		return nil, fmt.Errorf("extra args for %s: %w", h.Name, err)
	}
	return append(args, extra...), nil
}

// ConnectArgs returns the full argv (excluding "ssh" itself) for an
// interactive login. If the host has a remote path, the session starts in it.
func ConnectArgs(h store.Host) ([]string, error) {
	args, err := Options(h)
	if err != nil {
		return nil, err
	}
	if h.RemotePath == "" {
		return append(args, h.Target()), nil
	}
	remote := "cd " + Quote(h.RemotePath) + ` && exec "$SHELL" -l`
	return append(append([]string{"-t"}, args...), h.Target(), remote), nil
}

// Quote quotes s for a POSIX shell. A leading "~/" is left unquoted so the
// remote shell still expands it.
func Quote(s string) string {
	if s == "~" {
		return s
	}
	if rest, ok := strings.CutPrefix(s, "~/"); ok {
		return "~/" + Quote(rest)
	}
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("/._-+:@%,=", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SplitArgs splits a string into arguments using POSIX-shell-like rules for
// whitespace, single quotes, double quotes and backslashes.
func SplitArgs(s string) ([]string, error) {
	var (
		args    []string
		cur     strings.Builder
		inArg   bool
		quote   rune
		escaped bool
	)
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inArg = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inArg = r, true
		case unicode.IsSpace(r):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if escaped {
		return nil, fmt.Errorf("trailing backslash")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}
