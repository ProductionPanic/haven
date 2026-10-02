package transfer

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/vfs"
	"github.com/ProductionPanic/rootnet-cli/v2/internal/vfs/vfstest"
)

func mkfile(t *testing.T, p, content string, mtime time.Time) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(p, mtime, mtime)
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// drain consumes events, answering conflicts with answer (if non-nil), and
// returns the conflicts it saw. It stops when the returned func is called.
func drain(e *Engine, answer func(Conflict) Decision) (stop func() []Conflict) {
	done := make(chan struct{})
	result := make(chan []Conflict, 1)
	go func() {
		var seen []Conflict
		for {
			select {
			case ev := <-e.Events():
				if c, ok := ev.(Conflict); ok {
					seen = append(seen, c)
					c.Reply <- answer(c)
				}
			case <-done:
				result <- seen
				return
			}
		}
	}()
	return func() []Conflict { close(done); return <-result }
}

var t0 = time.Date(2025, 3, 12, 9, 30, 0, 0, time.UTC)

func TestUploadAndDownloadTree(t *testing.T) {
	remote := vfstest.NewRemote(t)
	local := vfs.LocalFS{}
	src, dst, back := t.TempDir(), t.TempDir(), t.TempDir()

	mkfile(t, filepath.Join(src, "site", "index.php"), "<?php echo 1;", t0)
	mkfile(t, filepath.Join(src, "site", "wp-content", "a.css"), strings.Repeat("x", 300_000), t0)
	os.MkdirAll(filepath.Join(src, "site", "empty"), 0o755)
	mkfile(t, filepath.Join(src, "dump.sql"), "CREATE TABLE", t0)

	e := New(3)
	stop := drain(e, nil)
	p := e.Run(context.Background(), Request{
		Src: local, Sources: []string{filepath.Join(src, "site"), filepath.Join(src, "dump.sql")},
		Dst: remote, DstDir: dst,
	})
	stop()
	if p.Err != nil || p.Failed != 0 {
		t.Fatalf("upload: %+v", p)
	}
	if p.Files != 4 || p.Done != p.Total || p.Total != int64(13+300_000+12) {
		t.Errorf("progress totals: %+v", p)
	}
	if got := readFile(t, filepath.Join(dst, "site", "wp-content", "a.css")); len(got) != 300_000 {
		t.Errorf("a.css size %d", len(got))
	}
	if fi, err := os.Stat(filepath.Join(dst, "site", "empty")); err != nil || !fi.IsDir() {
		t.Errorf("empty dir not created: %v", err)
	}
	fi, _ := os.Stat(filepath.Join(dst, "dump.sql"))
	if !fi.ModTime().Equal(t0) || fi.Mode().Perm() != 0o640 {
		t.Errorf("mtime/mode not preserved: %v %v", fi.ModTime(), fi.Mode())
	}
	assertNoParts(t, dst)

	// And back down again.
	stop = drain(e, nil)
	p = e.Run(context.Background(), Request{Src: remote, Sources: []string{filepath.Join(dst, "site")}, Dst: local, DstDir: back})
	stop()
	if p.Err != nil || readFile(t, filepath.Join(back, "site", "index.php")) != "<?php echo 1;" {
		t.Fatalf("download: %+v", p)
	}
}

func assertNoParts(t *testing.T, dir string) {
	t.Helper()
	filepath.Walk(dir, func(p string, _ os.FileInfo, _ error) error {
		if strings.HasSuffix(p, PartSuffix) {
			t.Errorf("leftover part file %s", p)
		}
		return nil
	})
}

func TestConflictPolicies(t *testing.T) {
	newer, older := t0.Add(time.Hour), t0.Add(-time.Hour)
	cases := []struct {
		policy  Policy
		srcTime time.Time
		want    string // content of dst/f.txt
		extra   string // expected extra file
	}{
		{Overwrite, older, "new", ""},
		{Skip, newer, "old", ""},
		{OverwriteIfNewer, newer, "new", ""},
		{OverwriteIfNewer, older, "old", ""},
		{RenameNew, newer, "old", "f (1).txt"},
	}
	for _, c := range cases {
		src, dst := t.TempDir(), t.TempDir()
		mkfile(t, filepath.Join(src, "f.txt"), "new", c.srcTime)
		mkfile(t, filepath.Join(dst, "f.txt"), "old", t0)
		e := New(1)
		stop := drain(e, nil)
		p := e.Run(context.Background(), Request{Src: vfs.LocalFS{}, Sources: []string{filepath.Join(src, "f.txt")},
			Dst: vfs.LocalFS{}, DstDir: dst, Policy: c.policy})
		stop()
		if p.Err != nil {
			t.Fatalf("%v: %v", c.policy, p.Err)
		}
		if got := readFile(t, filepath.Join(dst, "f.txt")); got != c.want {
			t.Errorf("%v (src newer=%v): dst = %q, want %q", c.policy, c.srcTime.After(t0), got, c.want)
		}
		if c.extra != "" && readFile(t, filepath.Join(dst, c.extra)) != "new" {
			t.Errorf("%v: %s missing", c.policy, c.extra)
		}
	}
}

func TestAskForAll(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	var sources []string
	for _, n := range []string{"a", "b", "c", "d"} {
		mkfile(t, filepath.Join(src, n), "new", t0)
		mkfile(t, filepath.Join(dst, n), "old", t0)
		sources = append(sources, filepath.Join(src, n))
	}
	e := New(4)
	stop := drain(e, func(Conflict) Decision { return Decision{Policy: Skip, ForAll: true} })
	p := e.Run(context.Background(), Request{Src: vfs.LocalFS{}, Sources: sources, Dst: vfs.LocalFS{}, DstDir: dst})
	seen := stop()
	if len(seen) != 1 {
		t.Errorf("asked %d times, want 1", len(seen))
	}
	if p.Settled != 4 || p.Failed != 0 {
		t.Errorf("progress %+v", p)
	}
	for _, n := range []string{"a", "b", "c", "d"} {
		if readFile(t, filepath.Join(dst, n)) != "old" {
			t.Errorf("%s was overwritten", n)
		}
	}
}

// slowFS makes reads slow so a transfer can be cancelled mid-file.
type slowFS struct{ vfs.LocalFS }

type slowReader struct{ io.ReadCloser }

func (s slowReader) Read(b []byte) (int, error) {
	time.Sleep(2 * time.Millisecond)
	if len(b) > 1024 {
		b = b[:1024]
	}
	return s.ReadCloser.Read(b)
}

func (s slowFS) Open(p string) (io.ReadCloser, error) {
	r, err := s.LocalFS.Open(p)
	return slowReader{r}, err
}

func TestCancel(t *testing.T) {
	remote := vfstest.NewRemote(t)
	src, dst := t.TempDir(), t.TempDir()
	mkfile(t, filepath.Join(src, "big.bin"), strings.Repeat("z", 2<<20), t0)
	mkfile(t, filepath.Join(dst, "big.bin"), "precious", t0)

	e := New(1)
	var id int
	started := make(chan struct{}, 1)
	finished := make(chan Progress, 1)
	go func() {
		for ev := range e.Events() {
			switch ev := ev.(type) {
			case Progress:
				if ev.Done > 0 {
					select {
					case started <- struct{}{}:
					default:
					}
				}
				if ev.Finished {
					finished <- ev
					return
				}
			}
		}
	}()
	id = e.Start(context.Background(), Request{Src: slowFS{}, Sources: []string{filepath.Join(src, "big.bin")},
		Dst: remote, DstDir: dst, Policy: Overwrite})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("transfer never started")
	}
	e.Cancel(id)
	var p Progress
	select {
	case p = <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("transfer did not stop")
	}
	if !p.Cancelled {
		t.Errorf("not marked cancelled: %+v", p)
	}
	if got := readFile(t, filepath.Join(dst, "big.bin")); got != "precious" {
		t.Errorf("destination clobbered by cancelled transfer (%d bytes)", len(got))
	}
	assertNoParts(t, dst)
}

func TestFreeName(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "a.tar.gz"), "", t0)
	mkfile(t, filepath.Join(dir, "a.tar (1).gz"), "", t0)
	if got := filepath.Base(FreeName(vfs.LocalFS{}, filepath.Join(dir, "a.tar.gz"))); got != "a.tar (2).gz" {
		t.Errorf("freeName = %q", got)
	}
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int64]string{512: "512B", 1536: "1.5K", 48_200_000: "46.0M", 300 << 20: "300M"} {
		if got := FormatBytes(n); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestDstName(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	mkfile(t, filepath.Join(src, "a.txt"), "hi", t0)
	e := New(1)
	stop := drain(e, nil)
	e.Run(context.Background(), Request{Src: vfs.LocalFS{}, Sources: []string{filepath.Join(src, "a.txt")},
		Dst: vfs.LocalFS{}, DstDir: dst, DstName: "b.txt"})
	stop()
	if readFile(t, filepath.Join(dst, "b.txt")) != "hi" {
		t.Error("not copied under new name")
	}
}

func TestPutFile(t *testing.T) {
	remote := vfstest.NewRemote(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "wp-config.php")
	mkfile(t, p, "old", t0)
	os.Chmod(p, 0o600)
	if err := PutFile(remote, p, strings.NewReader("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); got != "new" {
		t.Errorf("content %q", got)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	assertNoParts(t, dir)
}
