package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the root command with args against db and returns stdout.
func run(t *testing.T, db string, args ...string) (string, error) {
	t.Helper()
	a := &app{checkRun: fakeSSH}
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
	db := filepath.Join(dir, "haven.db")

	mustRun := func(args ...string) string {
		t.Helper()
		out, err := run(t, db, args...)
		if err != nil {
			t.Fatalf("haven %s: %v", strings.Join(args, " "), err)
		}
		return out
	}

	mustRun("add", "acme-shop.example", "web@web1", "--path", "/var/www", "-t", "wp")
	mustRun("add", "other.nl", "srv2", "-p", "2222")
	mustRun("add", "ssh", "deploy@web2.example.com")
	mustRun("add", "ssh", "deploy@other-web2.example.com")

	if got := mustRun("get", "deploy"); got != "deploy@web2.example.com" {
		t.Errorf("get after add ssh = %q", got)
	}
	if got := mustRun("get", "deploy@other"); got != "deploy@other-web2.example.com" {
		t.Errorf("get after add ssh with taken name = %q", got)
	}
	mustRun("rm", "deploy@other", "--yes")
	mustRun("rm", "deploy", "--yes")

	if got := mustRun("get", "acme"); got != "web@web1" {
		t.Errorf("get = %q", got)
	}
	if got := mustRun("get", "other", "--format", "{{.Hostname}}:{{.Port}}"); got != "srv2:2222\n" {
		t.Errorf("get --format = %q", got)
	}
	if got := mustRun("ls", "--tag", "wp"); !strings.Contains(got, "acme-shop.example") || strings.Contains(got, "other.nl") {
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
	if out, _ := run(t, db2, "ls"); !strings.Contains(out, "renamed.nl") || !strings.Contains(out, "acme-shop.example") {
		t.Errorf("import into fresh db:\n%s", out)
	}
}

// fakeSSH succeeds unless the destination contains "down".
func fakeSSH(_ context.Context, args []string) error {
	for _, a := range args {
		if strings.Contains(a, "down") {
			return errors.New("Connection refused")
		}
	}
	return nil
}
