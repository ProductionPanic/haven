// Package hostlist reads plain-text host lists ("name | user@host" per
// line).
package hostlist

import (
	"bufio"
	"io"
	"strings"

	"github.com/ProductionPanic/haven/v2/internal/store"
)

// Parse reads a plain-text host list. Blank lines, comments (#) and
// malformed lines are skipped.
func Parse(r io.Reader) ([]store.Host, error) {
	var hosts []store.Host
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		target := strings.TrimSpace(parts[1])
		if name == "" || target == "" {
			continue
		}
		h := store.Host{Name: name, Hostname: target}
		if user, host, ok := strings.Cut(target, "@"); ok {
			h.User, h.Hostname = user, host
		}
		hosts = append(hosts, h)
	}
	return hosts, sc.Err()
}
