package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the root command with args against db and returns stdout.
func run(t *testing.T, db string, args ...string) (string, error) {
	t.Helper()
	a := &app{}
	defer a.close()
	root := a.rootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"--db", db}, args...))
	err := root.ExecuteContext(context.Background())
	return out.String(), err
}

func TestCommands(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir) // no legacy file to import
	db := filepath.Join(dir, "rootnet.db")

	mustRun := func(args ...string) string {
		t.Helper()
		out, err := run(t, db, args...)
		if err != nil {
			t.Fatalf("rootnet %s: %v", strings.Join(args, " "), err)
		}
		return out
	}

	mustRun("add", "appelenburg.nl", "web@nuthatch", "--path", "/var/www", "-t", "wp")
	mustRun("add", "other.nl", "srv2", "-p", "2222")

	if got := mustRun("get", "appel"); got != "web@nuthatch" {
		t.Errorf("get = %q", got)
	}
	if got := mustRun("get", "other", "--format", "{{.Hostname}}:{{.Port}}"); got != "srv2:2222\n" {
		t.Errorf("get --format = %q", got)
	}
	if got := mustRun("ls", "--tag", "wp"); !strings.Contains(got, "appelenburg.nl") || strings.Contains(got, "other.nl") {
		t.Errorf("ls --tag wp:\n%s", got)
	}

	mustRun("edit", "other", "--name", "renamed.nl", "--env", "production")
	if got := mustRun("get", "renamed", "--json"); !strings.Contains(got, `"environment": "production"`) {
		t.Errorf("edit not applied:\n%s", got)
	}

	exported := mustRun("export")
	if !strings.Contains(exported, `name = "renamed.nl"`) {
		t.Errorf("export:\n%s", exported)
	}

	mustRun("rm", "renamed", "--yes")
	if got := mustRun("ls", "--json"); strings.Contains(got, "renamed") {
		t.Errorf("rm did not remove host:\n%s", got)
	}

	// Re-import the export into a fresh database.
	file := filepath.Join(dir, "hosts.toml")
	os.WriteFile(file, []byte(exported), 0o600)
	db2 := filepath.Join(dir, "other.db")
	if _, err := run(t, db2, "import", file); err != nil {
		t.Fatal(err)
	}
	if out, _ := run(t, db2, "ls"); !strings.Contains(out, "renamed.nl") || !strings.Contains(out, "appelenburg.nl") {
		t.Errorf("import into fresh db:\n%s", out)
	}
}

func TestLegacyAutoImport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	legacyFile := filepath.Join(dir, "rootnet_hosts.txt")
	os.WriteFile(legacyFile, []byte("a.nl | u@a\nb.nl | u@b\n"), 0o600)

	out, err := run(t, filepath.Join(dir, "rootnet.db"), "ls")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.nl") || !strings.Contains(out, "b.nl") {
		t.Errorf("hosts not imported:\n%s", out)
	}
	if _, err := os.Stat(legacyFile + ".bak"); err != nil {
		t.Errorf("legacy file not renamed: %v", err)
	}
}
