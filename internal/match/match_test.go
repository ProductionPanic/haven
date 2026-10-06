package match

import (
	"testing"
	"time"

	"github.com/ProductionPanic/haven/v2/internal/store"
)

func names(hs []store.Host) []string {
	var out []string
	for _, h := range hs {
		out = append(out, h.Name)
	}
	return out
}

func TestFind(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-2 * time.Hour)
	old := now.Add(-90 * 24 * time.Hour)
	hosts := []store.Host{
		{Name: "foo.nl", User: "foo", Hostname: "srv1"},
		{Name: "foobar.com", User: "fb", Hostname: "srv2", UseCount: 3, LastUsedAt: &recent},
		{Name: "barfoo.org", User: "bf", Hostname: "srv1", UseCount: 100, LastUsedAt: &old, Tags: []string{"wordpress"}},
		{Name: "other.io", User: "o", Hostname: "srv3"},
	}

	cases := []struct {
		q    string
		want []string
	}{
		{"FOO.NL", []string{"foo.nl"}},                          // exact wins
		{"foo", []string{"foobar.com", "barfoo.org", "foo.nl"}}, // frecency order
		{"srv1", []string{"barfoo.org", "foo.nl"}},              // matches target
		{"foo wordpress", []string{"barfoo.org"}},               // all terms, tags
		{"zzz", nil}, // no match
		{"", []string{"foobar.com", "barfoo.org", "foo.nl", "other.io"}}, // all, ranked
	}
	for _, c := range cases {
		got := names(Find(hosts, c.q, now))
		if len(got) != len(c.want) {
			t.Errorf("Find(%q) = %v, want %v", c.q, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("Find(%q) = %v, want %v", c.q, got, c.want)
				break
			}
		}
	}
}
