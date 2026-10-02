// Package match resolves a user query to hosts and ranks them by frecency.
package match

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/store"
)

// Frecency scores a host by how often and how recently it was used.
// A host that was never used scores 0.
func Frecency(h store.Host, now time.Time) float64 {
	if h.UseCount == 0 || h.LastUsedAt == nil {
		return 0
	}
	age := now.Sub(*h.LastUsedAt)
	var weight float64
	switch {
	case age < time.Hour:
		weight = 8
	case age < 24*time.Hour:
		weight = 4
	case age < 7*24*time.Hour:
		weight = 2
	case age < 30*24*time.Hour:
		weight = 1
	default:
		weight = 0.25
	}
	// log damping so a host used 500 times long ago doesn't bury
	// one used a handful of times today.
	return weight * (1 + math.Log1p(float64(h.UseCount)))
}

// Rank sorts hosts by frecency (highest first), then by name.
func Rank(hosts []store.Host, now time.Time) []store.Host {
	out := append([]store.Host(nil), hosts...)
	sort.SliceStable(out, func(i, j int) bool {
		fi, fj := Frecency(out[i], now), Frecency(out[j], now)
		if fi != fj {
			return fi > fj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Find returns the hosts matching query, ranked by frecency.
//
// Every whitespace-separated term must appear (case-insensitively) in the
// host's name, target or tags. An exact name match always wins and is
// returned on its own. An empty query matches everything.
func Find(hosts []store.Host, query string, now time.Time) []store.Host {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return Rank(hosts, now)
	}
	for _, h := range hosts {
		if strings.ToLower(h.Name) == q {
			return []store.Host{h}
		}
	}
	terms := strings.Fields(q)
	var matches []store.Host
	for _, h := range hosts {
		hay := strings.ToLower(h.Name + " " + h.Target() + " " + strings.Join(h.Tags, " "))
		ok := true
		for _, t := range terms {
			if !strings.Contains(hay, t) {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, h)
		}
	}
	return Rank(matches, now)
}
