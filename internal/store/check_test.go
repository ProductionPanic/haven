package store

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestSaveCheck(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	h, err := s.Create(ctx, Host{Name: "a", Hostname: "a.example", Tags: []string{"wp"}})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := s.Create(ctx, Host{Name: "b", Hostname: "b.example"})

	if h.Check != nil {
		t.Fatalf("new host has Check = %+v", h.Check)
	}

	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, c := range []Check{
		{At: at, OK: true},
		{At: at.Add(time.Hour), OK: false, Error: "Connection refused"},
	} {
		if err := s.SaveCheck(ctx, h.ID, c); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(ctx, "a")
		if err != nil {
			t.Fatal(err)
		}
		if got.Check == nil || !got.Check.At.Equal(c.At) || got.Check.OK != c.OK || got.Check.Error != c.Error {
			t.Fatalf("Get check = %+v, want %+v", got.Check, c)
		}
		list, err := s.List(ctx, "")
		if err != nil || len(list) != 2 {
			t.Fatalf("List = %v, %v", list, err)
		}
		if list[0].Check == nil || !list[0].Check.At.Equal(c.At) || list[0].Check.OK != c.OK || list[0].Check.Error != c.Error {
			t.Fatalf("List check = %+v, want %+v", list[0].Check, c)
		}
		if list[1].Check != nil {
			t.Fatalf("unchecked host has Check = %+v", list[1].Check)
		}
	}
	if got, _ := s.Get(ctx, "b"); got.Check != nil || got.ID != other.ID {
		t.Fatalf("host b = %+v", got)
	}

	// The tag filter still works with the join, and keeps the check.
	tagged, err := s.List(ctx, "wp")
	if err != nil || len(tagged) != 1 || tagged[0].Name != "a" || tagged[0].Check == nil {
		t.Fatalf("List(wp) = %+v, %v", tagged, err)
	}
	if !reflect.DeepEqual(tagged[0].Tags, []string{"wp"}) {
		t.Fatalf("tags = %v", tagged[0].Tags)
	}
	if none, _ := s.List(ctx, "nope"); len(none) != 0 {
		t.Fatalf("List(nope) = %v", none)
	}

	// Updating the host keeps its check.
	got, _ := s.Get(ctx, "a")
	got.Notes = "x"
	if got, err = s.Update(ctx, got); err != nil || got.Check == nil {
		t.Fatalf("Update dropped check: %+v, %v", got, err)
	}
}

func TestSaveCheckAndDirsShareRow(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	check := Check{At: at, OK: true}

	tests := []struct {
		name  string
		order func(s *Store, id int64) error
	}{
		{"dirs then check", func(s *Store, id int64) error {
			if err := s.SaveDirs(ctx, id, "/l", "/r"); err != nil {
				return err
			}
			return s.SaveCheck(ctx, id, check)
		}},
		{"check then dirs", func(s *Store, id int64) error {
			if err := s.SaveCheck(ctx, id, check); err != nil {
				return err
			}
			return s.SaveDirs(ctx, id, "/l", "/r")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := openTemp(t)
			h, _ := s.Create(ctx, Host{Name: "a", Hostname: "a.example"})
			if err := tt.order(s, h.ID); err != nil {
				t.Fatal(err)
			}
			l, r, err := s.Dirs(ctx, h.ID)
			if err != nil || l != "/l" || r != "/r" {
				t.Fatalf("Dirs = %q, %q, %v", l, r, err)
			}
			got, _ := s.Get(ctx, "a")
			if got.Check == nil || !got.Check.OK || !got.Check.At.Equal(at) {
				t.Fatalf("check lost: %+v", got.Check)
			}
			var n int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM host_state WHERE host_id = ?`, h.ID).Scan(&n); err != nil || n != 1 {
				t.Fatalf("host_state rows = %d, %v", n, err)
			}
		})
	}
}

func TestDeleteHostRemovesState(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	h, _ := s.Create(ctx, Host{Name: "a", Hostname: "a.example"})
	if err := s.SaveCheck(ctx, h.ID, Check{At: time.Now(), OK: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDirs(ctx, h.ID, "/l", "/r"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, h.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM host_state`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("host_state rows after delete = %d, %v", n, err)
	}
}

func TestStale(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	maxAge := 5 * time.Minute
	tests := []struct {
		name  string
		check *Check
		want  bool
	}{
		{"never checked", nil, true},
		{"fresh", &Check{At: now.Add(-time.Minute), OK: true}, false},
		{"just now", &Check{At: now, OK: true}, false},
		{"exactly max age", &Check{At: now.Add(-maxAge)}, true},
		{"old", &Check{At: now.Add(-time.Hour), OK: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Host{Check: tt.check}).Stale(now, maxAge); got != tt.want {
				t.Errorf("Stale = %v, want %v", got, tt.want)
			}
		})
	}
}
