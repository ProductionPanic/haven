package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProductionPanic/haven/v2/internal/store"
	"github.com/ProductionPanic/haven/v2/internal/vfs"
	"github.com/ProductionPanic/haven/v2/internal/vfs/vfstest"
)

// env is a haven database in a temp dir whose hosts all "connect" to the
// local disk through an in-process SFTP server.
type env struct {
	t   *testing.T
	dir string
	db  string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	e := &env{t: t, dir: dir, db: filepath.Join(dir, "haven.db")}
	e.ok("add", "appel", "web@appel.test", "--no-check", "--path", "/var/www", "-t", "wp")
	e.ok("add", "prod", "web@prod.test", "--no-check", "-e", "production")
	e.ok("add", "prod2", "web@prod2.test", "--no-check", "-e", "production")
	return e
}

func (e *env) runIn(stdin string, args ...string) (string, string, error) {
	e.t.Helper()
	a := &app{
		checkRun: fakeSSH,
		dial: func(_ context.Context, h store.Host) (*vfs.RemoteFS, error) {
			return vfstest.NewRemote(e.t), nil
		},
	}
	defer a.close()
	root := a.rootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{"--db", e.db}, args...))
	err := root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func (e *env) run(args ...string) (string, error) {
	e.t.Helper()
	out, _, err := e.runIn("", args...)
	return out, err
}

func (e *env) ok(args ...string) string {
	e.t.Helper()
	out, err := e.run(args...)
	if err != nil {
		e.t.Fatalf("haven %s: %v", strings.Join(args, " "), err)
	}
	return out
}

func (e *env) fails(want string, args ...string) {
	e.t.Helper()
	out, err := e.run(args...)
	if err == nil {
		e.t.Fatalf("haven %s succeeded, want error containing %q\n%s", strings.Join(args, " "), want, out)
	}
	if !strings.Contains(err.Error(), want) {
		e.t.Fatalf("haven %s: error %q does not contain %q", strings.Join(args, " "), err, want)
	}
}

func (e *env) path(name string) string { return filepath.Join(e.dir, name) }

func (e *env) read(name string) string {
	e.t.Helper()
	b, err := os.ReadFile(e.path(name))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func TestFileLocalLifecycle(t *testing.T) {
	e := newEnv(t)
	f := e.path("notes.txt")

	if out := e.ok("file", "create", f, "--content", "one\ntwo\n"); !strings.Contains(out, "created") {
		t.Errorf("create output = %q", out)
	}
	if got := e.read("notes.txt"); got != "one\ntwo\n" {
		t.Errorf("content = %q", got)
	}
	e.fails("already exists", "file", "create", f, "--content", "x")
	e.ok("file", "create", f, "--content", "three\n", "--force")
	if got := e.read("notes.txt"); got != "three\n" {
		t.Errorf("after --force: %q", got)
	}

	if got := e.ok("file", "read", f); got != "three\n" {
		t.Errorf("read = %q", got)
	}

	e.ok("file", "modify", f, "--old", "three", "--new", "four")
	if got := e.read("notes.txt"); got != "four\n" {
		t.Errorf("after replace: %q", got)
	}
	out, _, err := e.runIn("five\n", "file", "modify", f, "--append")
	if err != nil || !strings.Contains(out, "appended to") {
		t.Fatalf("append: %q %v", out, err)
	}
	if got := e.read("notes.txt"); got != "four\nfive\n" {
		t.Errorf("after append: %q", got)
	}
	e.ok("file", "modify", f, "--overwrite", "--content", "fresh")
	if got := e.read("notes.txt"); got != "fresh" {
		t.Errorf("after overwrite: %q", got)
	}

	e.ok("file", "delete", f)
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Errorf("file still there: %v", err)
	}
	e.fails("does not exist", "file", "delete", f)
	e.ok("file", "delete", "--ignore-missing", f)
}

func TestFileCreateGuards(t *testing.T) {
	e := newEnv(t)

	e.fails("content is empty", "file", "create", e.path("a.txt")) // empty stdin
	if _, err := os.Stat(e.path("a.txt")); err == nil {
		t.Error("empty file created without --empty")
	}
	e.ok("file", "create", e.path("empty.txt"), "--empty")
	if got := e.read("empty.txt"); got != "" {
		t.Errorf("empty = %q", got)
	}

	e.fails("does not exist", "file", "create", e.path("sub/b.txt"), "--content", "x")
	e.ok("file", "create", "--parents", e.path("sub/deep/b.txt"), "--content", "x")
	if got := e.read("sub/deep/b.txt"); got != "x" {
		t.Errorf("parents: %q", got)
	}

	// stdin content, including a trailing newline
	out, _, err := e.runIn("from stdin\n", "file", "create", e.path("s.txt"))
	if err != nil {
		t.Fatalf("%q %v", out, err)
	}
	if got := e.read("s.txt"); got != "from stdin\n" {
		t.Errorf("stdin: %q", got)
	}

	e.fails("is a directory", "file", "create", e.path("sub"), "--content", "x", "--force")
	if err := os.Symlink(e.path("s.txt"), e.path("link")); err != nil {
		t.Fatal(err)
	}
	e.fails("symlink", "file", "create", e.path("link"), "--content", "x", "--force")
	e.fails("symlink", "file", "modify", e.path("link"), "--append", "--content", "x")
}

func TestFileModifyGuards(t *testing.T) {
	e := newEnv(t)
	f := e.path("m.txt")
	e.ok("file", "create", f, "--content", "a b a\n")
	if err := os.Chmod(f, 0o600); err != nil {
		t.Fatal(err)
	}

	e.fails("exactly one mode", "file", "modify", f)
	e.fails("exactly one mode", "file", "modify", f, "--append", "--overwrite", "--content", "x")
	e.fails("go together", "file", "modify", f, "--old", "a")
	e.fails("not found", "file", "modify", f, "--old", "zzz", "--new", "y")
	e.fails("occurs 2 times", "file", "modify", f, "--old", "a", "--new", "y")
	e.fails("must not be empty", "file", "modify", f, "--old", "", "--new", "y")
	e.fails("does not exist", "file", "modify", e.path("nope"), "--append", "--content", "x")
	e.fails("is a directory", "file", "modify", e.dir, "--append", "--content", "x")
	e.fails("content is empty", "file", "modify", f, "--overwrite")
	if got := e.read("m.txt"); got != "a b a\n" {
		t.Errorf("failed modifies changed the file: %q", got)
	}

	out := e.ok("file", "modify", f, "--old", "a", "--new", "y", "--all", "--json")
	var res changeResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}
	if res.Replacements != 2 || res.Action != "modified" || res.Path != f {
		t.Errorf("result = %+v", res)
	}
	if got := e.read("m.txt"); got != "y b y\n" {
		t.Errorf("content = %q", got)
	}
	e.ok("file", "modify", f, "--old", " b", "--new", "")
	if got := e.read("m.txt"); got != "y y\n" {
		t.Errorf("deleting text: %q", got)
	}
	e.ok("file", "modify", f, "--overwrite", "--empty", "--content", "")
	if got := e.read("m.txt"); got != "" {
		t.Errorf("emptied: %q", got)
	}
	if fi, _ := os.Stat(f); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode not kept: %v", fi.Mode())
	}

	if err := os.WriteFile(e.path("bin"), []byte{0, 1, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	e.fails("binary", "file", "modify", e.path("bin"), "--append", "--content", "x")
	e.fails("binary", "file", "read", e.path("bin"))
}

func TestFileRead(t *testing.T) {
	e := newEnv(t)
	f := e.path("lines.txt")
	e.ok("file", "create", f, "--content", "a\nb\nc\nd\n")

	if got := e.ok("file", "read", f, "--offset", "2", "--limit", "2", "--numbered"); got != "     2\tb\n     3\tc\n" {
		t.Errorf("numbered range = %q", got)
	}
	var res readResult
	if err := json.Unmarshal([]byte(e.ok("file", "read", f, "--limit", "2", "--json")), &res); err != nil {
		t.Fatal(err)
	}
	if res.Content != "a\nb\n" || res.StartLine != 1 || res.EndLine != 2 || !res.Truncated || res.Size != 8 {
		t.Errorf("json = %+v", res)
	}
	_, stderr, err := e.runIn("", "file", "read", f, "--limit", "1")
	if err != nil || !strings.Contains(stderr, "--offset 2") {
		t.Errorf("truncation note = %q (%v)", stderr, err)
	}
	e.fails("is a directory", "file", "read", e.dir)
	e.fails("--offset starts at 1", "file", "read", f, "--offset", "0")
}

func TestReadLines(t *testing.T) {
	res, err := readLines(strings.NewReader("a\nb\nc"), 2, 0, 100)
	if err != nil || res.Content != "b\nc" || res.StartLine != 2 || res.EndLine != 3 || res.Truncated {
		t.Errorf("no trailing newline: %+v %v", res, err)
	}
	res, _ = readLines(strings.NewReader("a\nb\nc\n"), 1, 0, 3)
	if res.Content != "a\n" || !res.Truncated {
		t.Errorf("byte cap: %+v", res)
	}
	res, _ = readLines(strings.NewReader("abcdefgh\n"), 1, 0, 4)
	if res.Content != "abcd" || !res.Truncated || res.EndLine != 1 {
		t.Errorf("single long line: %+v", res)
	}
	res, _ = readLines(strings.NewReader("a\n"), 5, 0, 100)
	if res.Content != "" || res.EndLine != 4 {
		t.Errorf("offset past the end: %+v", res)
	}
	if _, err := readLines(strings.NewReader("a\x00b"), 1, 0, 100); err == nil {
		t.Error("binary content accepted")
	}
}

func TestFileList(t *testing.T) {
	e := newEnv(t)
	e.ok("file", "create", e.path("dir/b.txt"), "--parents", "--content", "bb")
	e.ok("file", "create", e.path("dir/.hidden"), "--content", "h")
	e.ok("file", "create", e.path("dir/a.txt"), "--content", "a")
	if err := os.Mkdir(e.path("dir/sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	var res listResult
	if err := json.Unmarshal([]byte(e.ok("file", "list", e.path("dir"), "--json")), &res); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, en := range res.Entries {
		names = append(names, en.Type+":"+en.Name)
	}
	if got := strings.Join(names, " "); got != "dir:sub file:.hidden file:a.txt file:b.txt" {
		t.Errorf("entries = %s", got)
	}
	if out := e.ok("file", "list", e.path("dir")); !strings.Contains(out, "sub/") {
		t.Errorf("text listing:\n%s", out)
	}
	e.fails("is a file", "file", "list", e.path("dir/a.txt"))
}

func TestFileDelete(t *testing.T) {
	e := newEnv(t)
	e.ok("file", "create", e.path("d/x.txt"), "--parents", "--content", "x")
	e.ok("file", "create", e.path("keep.txt"), "--content", "k")

	e.fails("pass --recursive", "file", "delete", e.path("d"))
	// one bad path must stop the whole job before anything is deleted
	e.fails("pass --recursive", "file", "delete", e.path("keep.txt"), e.path("d"))
	if got := e.read("keep.txt"); got != "k" {
		t.Error("keep.txt was deleted although the command failed")
	}
	e.fails("refusing to delete the home directory", "file", "delete", "--recursive", "~")
	e.fails("refusing to delete /", "file", "delete", "--recursive", "/")

	out := e.ok("file", "delete", "--recursive", "--json", e.path("d"))
	var res deleteResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || !res.Dir || res.Action != "deleted" {
		t.Errorf("result = %+v %v", res, err)
	}
	if _, err := os.Stat(e.path("d")); !os.IsNotExist(err) {
		t.Error("directory still there")
	}

	// a symlink to a directory is removed without --recursive and the target survives
	if err := os.Mkdir(e.path("target"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(e.path("target"), e.path("ln")); err != nil {
		t.Fatal(err)
	}
	e.ok("file", "delete", e.path("ln"))
	if _, err := os.Stat(e.path("target")); err != nil {
		t.Error("symlink target was deleted")
	}
}

func TestFileOnHost(t *testing.T) {
	e := newEnv(t)
	remote := e.path("remote")
	if err := os.Mkdir(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	f := "appel:" + filepath.Join(remote, "site.txt")

	e.ok("file", "create", f, "--content", "hello world\n")
	if got := e.read("remote/site.txt"); got != "hello world\n" {
		t.Errorf("remote content = %q", got)
	}
	if got := e.ok("file", "read", f); got != "hello world\n" {
		t.Errorf("remote read = %q", got)
	}
	out := e.ok("file", "modify", f, "--old", "world", "--new", "haven", "--json")
	var res changeResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Host != "appel" || res.Replacements != 1 {
		t.Errorf("result = %+v %v", res, err)
	}
	if got := e.read("remote/site.txt"); got != "hello haven\n" {
		t.Errorf("remote modify = %q", got)
	}
	if out := e.ok("file", "list", "appel:"+remote); !strings.Contains(out, "site.txt") {
		t.Errorf("remote list:\n%s", out)
	}
	if out := e.ok("file", "modify", f, "--append", "--content", "!"); !strings.Contains(out, "appel:") {
		t.Errorf("output does not name the host: %q", out)
	}
	e.ok("file", "delete", f)
	if _, err := os.Stat(e.path("remote/site.txt")); !os.IsNotExist(err) {
		t.Error("remote file still there")
	}
	e.fails("no host matches", "file", "read", "nowhere:/etc/hostname")
}

func TestFileProductionGuard(t *testing.T) {
	e := newEnv(t)
	remote := e.path("remote")
	if err := os.Mkdir(remote, 0o755); err != nil {
		t.Fatal(err)
	}
	f := "prod:" + filepath.Join(remote, "x.txt")

	e.fails("--allow-production", "file", "create", f, "--content", "x")
	if _, err := os.Stat(e.path("remote/x.txt")); err == nil {
		t.Fatal("file written to a production host without the flag")
	}
	e.ok("file", "create", f, "--content", "x", "--allow-production")

	// reading and listing never need the flag
	e.ok("file", "read", f)
	e.ok("file", "list", "prod:"+remote)

	e.fails("--allow-production", "file", "modify", f, "--append", "--content", "y")
	e.fails("--allow-production", "file", "delete", f)
	e.ok("file", "modify", f, "--append", "--content", "y", "--allow-production")
	e.ok("file", "delete", f, "--allow-production")
}

func TestFileAmbiguousHost(t *testing.T) {
	e := newEnv(t)
	// "prod" is an exact name and wins over "prod2"; "pro" matches both.
	e.fails("ambiguous", "file", "list", "pro:/tmp")
}

func TestServersCommand(t *testing.T) {
	e := newEnv(t)
	e.ok("check", "appel")

	var all []serverInfo
	if err := json.Unmarshal([]byte(e.ok("servers")), &all); err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d servers: %+v", len(all), all)
	}
	by := map[string]serverInfo{}
	for _, s := range all {
		by[s.Name] = s
	}
	appel, prod := by["appel"], by["prod"]
	if appel.Target != "web@appel.test" || appel.Port != 22 || appel.RemotePath != "/var/www" ||
		appel.FilePrefix != "appel:" || appel.Production || len(appel.Tags) != 1 {
		t.Errorf("appel = %+v", appel)
	}
	if appel.SSHOK == nil || !*appel.SSHOK || appel.CheckedAt == nil {
		t.Errorf("appel check = %+v", appel)
	}
	if !prod.Production || prod.SSHOK != nil {
		t.Errorf("prod = %+v", prod)
	}

	if out := e.ok("servers", "--tag", "wp"); !strings.Contains(out, `"appel"`) || strings.Contains(out, `"prod"`) {
		t.Errorf("--tag:\n%s", out)
	}
	if out := e.ok("servers", "appel"); strings.Contains(out, `"prod"`) {
		t.Errorf("query:\n%s", out)
	}
	if out := e.ok("servers", "nothing-like-this"); strings.TrimSpace(out) != "[]" {
		t.Errorf("no match should print []: %q", out)
	}
	if out := e.ok("servers"); strings.Contains(out, "identity") || strings.Contains(out, "extra_args") {
		t.Errorf("servers leaks ssh options:\n%s", out)
	}
}

func TestLLMGuide(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	db := filepath.Join(dir, "never-created.db")

	out, err := run(t, db, "llm")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(db); err == nil {
		t.Error("haven llm created the host database")
	}
	if !strings.HasPrefix(out, "# haven for LLMs") {
		t.Errorf("guide starts with %q", out[:min(40, len(out))])
	}

	// The guide must keep up with the commands.
	a := &app{}
	for _, c := range a.fileCmd().Commands() {
		if !strings.Contains(out, "haven file "+c.Name()) {
			t.Errorf("the guide doesn't mention \"haven file %s\"", c.Name())
		}
	}
	for _, want := range []string{"haven servers", "--allow-production", "host:path", "haven cp"} {
		if !strings.Contains(out, want) {
			t.Errorf("the guide doesn't mention %q", want)
		}
	}
}

func TestReservedNames(t *testing.T) {
	for _, n := range []string{"file", "servers", "llm"} {
		if !reserved(n) {
			t.Errorf("%q should be reserved", n)
		}
	}
}
