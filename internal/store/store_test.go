package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "rootnet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCRUD(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	h, err := s.Create(ctx, Host{Name: "appelenburg.nl", User: "web", Hostname: "nuthatch.example", Tags: []string{"WordPress, prod", "prod"}})
	if err != nil {
		t.Fatal(err)
	}
	if h.ID == 0 || h.Port != 22 || h.CreatedAt.IsZero() {
		t.Fatalf("unexpected host after create: %+v", h)
	}
	if want := []string{"prod", "wordpress"}; !reflect.DeepEqual(h.Tags, want) {
		t.Fatalf("tags = %v, want %v", h.Tags, want)
	}
	for _, dup := range []string{"appelenburg.nl", "APPELENBURG.NL"} {
		if _, err := s.Create(ctx, Host{Name: dup, Hostname: "x"}); err == nil {
			t.Fatalf("expected duplicate name error for %q", dup)
		}
	}

	got, err := s.Get(ctx, "Appelenburg.NL")
	if err != nil {
		t.Fatal(err)
	}
	if got.Target() != "web@nuthatch.example" {
		t.Fatalf("target = %q", got.Target())
	}

	got.Port = 2222
	got.Tags = []string{"staging"}
	got, err = s.Update(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 2222 || !reflect.DeepEqual(got.Tags, []string{"staging"}) {
		t.Fatalf("update not applied: %+v", got)
	}
	tags, _ := s.Tags(ctx)
	if !reflect.DeepEqual(tags, []string{"staging"}) {
		t.Fatalf("orphan tags not cleaned: %v", tags)
	}

	if err := s.MarkUsed(ctx, got.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, "appelenburg.nl")
	if got.UseCount != 1 || got.LastUsedAt == nil {
		t.Fatalf("usage not recorded: %+v", got)
	}

	list, err := s.List(ctx, "staging")
	if err != nil || len(list) != 1 || list[0].Tags[0] != "staging" {
		t.Fatalf("List(staging) = %v, %v", list, err)
	}
	list, _ = s.List(ctx, "nope")
	if len(list) != 0 {
		t.Fatalf("List(nope) = %v", list)
	}

	if err := s.Delete(ctx, got.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "appelenburg.nl"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete: %v", err)
	}
}

func TestValidate(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Create(context.Background(), Host{Name: "x"}); err == nil {
		t.Fatal("expected missing hostname error")
	}
}

func TestReopenKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Create(context.Background(), Host{Name: "a", Hostname: "b"})
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Get(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
}

func TestDirs(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	h, _ := s.Create(ctx, Host{Name: "a", Hostname: "b"})
	if l, r, err := s.Dirs(ctx, h.ID); err != nil || l != "" || r != "" {
		t.Fatalf("empty Dirs = %q %q %v", l, r, err)
	}
	s.SaveDirs(ctx, h.ID, "/home/me/site", "/var/www")
	s.SaveDirs(ctx, h.ID, "/home/me/site2", "/var/www")
	if l, r, _ := s.Dirs(ctx, h.ID); l != "/home/me/site2" || r != "/var/www" {
		t.Fatalf("Dirs = %q %q", l, r)
	}
	s.Delete(ctx, h.ID)
	if l, _, _ := s.Dirs(ctx, h.ID); l != "" {
		t.Error("state not removed with host")
	}
}
