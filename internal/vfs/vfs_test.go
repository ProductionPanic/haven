package vfs_test

import (
	. "github.com/ProductionPanic/haven/v2/internal/vfs"
	"github.com/ProductionPanic/haven/v2/internal/vfs/vfstest"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, f FS, p, content string) {
	t.Helper()
	w, err := f.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, f FS, p string) string {
	t.Helper()
	r, err := f.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func conformance(t *testing.T, f FS) {
	root := t.TempDir()
	j := f.Join

	if err := f.MkdirAll(j(root, "a", "b")); err != nil {
		t.Fatal(err)
	}
	write(t, f, j(root, "a", "file.txt"), "hello")
	write(t, f, j(root, ".hidden"), "x")
	if err := os.Symlink(filepath.Join(root, "a"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	os.Symlink(filepath.Join(root, "nope"), filepath.Join(root, "broken"))

	entries, err := f.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Entry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	if e := byName["a"]; !e.IsDir {
		t.Errorf("a: %+v", e)
	}
	if e := byName["link"]; !e.IsDir || e.Link == "" || e.Broken {
		t.Errorf("link should resolve to a dir: %+v", e)
	}
	if e := byName["broken"]; !e.Broken {
		t.Errorf("broken link not detected: %+v", e)
	}
	if !byName[".hidden"].Hidden() {
		t.Error(".hidden not hidden")
	}

	p := j(root, "a", "file.txt")
	if got := read(t, f, p); got != "hello" {
		t.Errorf("read = %q", got)
	}
	mtime := time.Date(2024, 2, 14, 10, 0, 0, 0, time.UTC)
	if err := f.Chtimes(p, mtime); err != nil {
		t.Fatal(err)
	}
	e, err := f.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if e.Size != 5 || !e.ModTime.Equal(mtime) {
		t.Errorf("stat after chtimes: %+v", e)
	}

	// Rename over an existing file replaces it.
	write(t, f, j(root, "a", "new.txt"), "new")
	if err := f.Rename(j(root, "a", "new.txt"), p); err != nil {
		t.Fatal(err)
	}
	if got := read(t, f, p); got != "new" {
		t.Errorf("after rename = %q", got)
	}

	if err := f.Mkdir(j(root, "a")); err == nil {
		t.Error("mkdir of existing dir should fail")
	}
	if err := f.Remove(j(root, "a")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Stat(j(root, "a")); err == nil {
		t.Error("a still exists after recursive remove")
	}

	if f.Base(p) != "file.txt" || !strings.HasSuffix(f.Dir(p), "a") {
		t.Errorf("Base/Dir: %q %q", f.Base(p), f.Dir(p))
	}
	if h, err := f.Home(); err != nil || h == "" {
		t.Errorf("Home = %q, %v", h, err)
	}
}

func TestLocalFS(t *testing.T)  { conformance(t, LocalFS{}) }
func TestRemoteFS(t *testing.T) { conformance(t, vfstest.NewRemote(t)) }

func TestSort(t *testing.T) {
	t0 := time.Now()
	es := []Entry{
		{Name: "b.txt", Size: 10, ModTime: t0},
		{Name: "Zdir", IsDir: true},
		{Name: "a.txt", Size: 99, ModTime: t0.Add(-time.Hour)},
		{Name: "adir", IsDir: true},
	}
	names := func() string {
		var s []string
		for _, e := range es {
			s = append(s, e.Name)
		}
		return strings.Join(s, ",")
	}
	Sort(es, SortName)
	if got := names(); got != "adir,Zdir,a.txt,b.txt" {
		t.Errorf("name: %s", got)
	}
	Sort(es, SortSize)
	if got := names(); got != "adir,Zdir,a.txt,b.txt" {
		t.Errorf("size: %s", got)
	}
	Sort(es, SortTime)
	if got := names(); got != "adir,Zdir,b.txt,a.txt" {
		t.Errorf("time: %s", got)
	}
}

func TestResolve(t *testing.T) {
	r := vfstest.NewRemote(t)
	home, _ := r.Home()
	for in, want := range map[string]string{
		"":          home,
		"~":         home,
		"~/site":    home + "/site",
		"site/../x": home + "/x",
		"/var/www/": "/var/www",
	} {
		if got, err := r.Resolve(in); err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}
