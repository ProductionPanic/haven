package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// runApp executes the root command with a (a fresh app per call) and returns
// stdout, stderr and the error. It fails the test if the command hangs, for
// instance because it opened an interactive form.
func runApp(t *testing.T, a *app, db string, args ...string) (string, string, error) {
	t.Helper()
	defer a.close()
	root := a.rootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append([]string{"--db", db}, args...))
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(context.Background()) }()
	select {
	case err := <-done:
		return out.String(), errOut.String(), err
	case <-time.After(10 * time.Second):
		t.Fatalf("haven %s hung", strings.Join(args, " "))
		return "", "", nil
	}
}

// runErr is like run but also returns stderr.
func runErr(t *testing.T, db string, args ...string) (string, string, error) {
	t.Helper()
	return runApp(t, &app{checkRun: fakeSSH}, db, args...)
}

func checkDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return filepath.Join(dir, "haven.db")
}

func mustRunErr(t *testing.T, db string, args ...string) (string, string) {
	t.Helper()
	out, errOut, err := runErr(t, db, args...)
	if err != nil {
		t.Fatalf("haven %s: %v", strings.Join(args, " "), err)
	}
	return out, errOut
}

func TestCheckCommand(t *testing.T) {
	db := checkDB(t)
	mustRunErr(t, db, "add", "alpha", "web@alpha.example", "-t", "wp", "--no-check")
	mustRunErr(t, db, "add", "beta", "beta.example", "--no-check")

	out, _ := mustRunErr(t, db, "check")
	for _, want := range []string{"NAME", "TARGET", "STATUS", "ERROR", "alpha", "web@alpha.example", "beta", "ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "failed") {
		t.Errorf("unexpected failure:\n%s", out)
	}

	mustRunErr(t, db, "add", "gamma", "gamma-down.example", "-t", "other", "--no-check")
	out, _, err := runErr(t, db, "check")
	if err == nil || !strings.Contains(err.Error(), "1 of 3 hosts failed") {
		t.Fatalf("err = %v, want \"1 of 3 hosts failed\"", err)
	}
	if !strings.Contains(out, "failed") || !strings.Contains(out, "Connection refused") {
		t.Errorf("table:\n%s", out)
	}
}

func TestCheckSelection(t *testing.T) {
	db := checkDB(t)
	mustRunErr(t, db, "add", "alpha", "alpha.example", "-t", "wp", "--no-check")
	mustRunErr(t, db, "add", "beta", "beta.example", "--no-check")
	mustRunErr(t, db, "add", "gamma", "gamma.example", "-t", "wp", "--no-check")

	var calls int32
	counting := func(ctx context.Context, args []string) error {
		atomic.AddInt32(&calls, 1)
		return fakeSSH(ctx, args)
	}
	tests := []struct {
		name      string
		args      []string
		wantCalls int32
		contains  []string
		absent    []string
	}{
		{"query", []string{"check", "beta"}, 1, []string{"beta"}, []string{"alpha", "gamma"}},
		{"tag", []string{"check", "--tag", "wp"}, 2, []string{"alpha", "gamma"}, []string{"beta"}},
		{"tag and query", []string{"check", "--tag", "wp", "gam"}, 1, []string{"gamma"}, []string{"alpha", "beta"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			atomic.StoreInt32(&calls, 0)
			out, _, err := runApp(t, &app{checkRun: counting}, db, tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if got := atomic.LoadInt32(&calls); got != tt.wantCalls {
				t.Errorf("runner calls = %d, want %d", got, tt.wantCalls)
			}
			for _, s := range tt.contains {
				if !strings.Contains(out, s) {
					t.Errorf("missing %q:\n%s", s, out)
				}
			}
			for _, s := range tt.absent {
				if strings.Contains(out, s) {
					t.Errorf("unexpected %q:\n%s", s, out)
				}
			}
		})
	}

	t.Run("no match", func(t *testing.T) {
		atomic.StoreInt32(&calls, 0)
		_, _, err := runApp(t, &app{checkRun: counting}, db, "check", "zzz-nothing")
		if err == nil || !strings.Contains(err.Error(), "no host matches") {
			t.Errorf("err = %v", err)
		}
		if atomic.LoadInt32(&calls) != 0 {
			t.Error("runner was called")
		}
	})
}

func TestCheckJSONAndSaved(t *testing.T) {
	db := checkDB(t)
	mustRunErr(t, db, "add", "alpha", "alpha.example", "--no-check")
	mustRunErr(t, db, "add", "gamma", "gamma-down.example", "--no-check")

	// Nothing checked yet.
	if out, _ := mustRunErr(t, db, "ls"); !strings.Contains(out, "SSH") || strings.Contains(out, "failed") {
		t.Errorf("ls before check:\n%s", out)
	}
	if out, _ := mustRunErr(t, db, "ls", "--json"); strings.Contains(out, "last_check") {
		t.Errorf("ls --json before check:\n%s", out)
	}

	out, _, err := runErr(t, db, "check", "--json")
	if err == nil {
		t.Fatal("expected error for failing host")
	}
	var hosts []struct {
		Name      string `json:"name"`
		LastCheck *struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"last_check"`
	}
	if err := json.Unmarshal([]byte(out), &hosts); err != nil {
		t.Fatalf("bad json: %v\n%s", err, out)
	}
	if len(hosts) != 2 {
		t.Fatalf("hosts = %+v", hosts)
	}
	for _, h := range hosts {
		if h.LastCheck == nil {
			t.Fatalf("%s has no last_check:\n%s", h.Name, out)
		}
		if want := h.Name == "alpha"; h.LastCheck.OK != want {
			t.Errorf("%s ok = %v", h.Name, h.LastCheck.OK)
		}
		if h.Name == "gamma" && h.LastCheck.Error != "Connection refused" {
			t.Errorf("gamma error = %q", h.LastCheck.Error)
		}
	}

	// Results were saved: ls shows them.
	ls, _ := mustRunErr(t, db, "ls")
	for _, line := range strings.Split(ls, "\n") {
		switch {
		case strings.HasPrefix(line, "alpha"):
			if !strings.HasSuffix(strings.TrimSpace(line), "ok") {
				t.Errorf("alpha line: %q", line)
			}
		case strings.HasPrefix(line, "gamma"):
			if !strings.HasSuffix(strings.TrimSpace(line), "failed") {
				t.Errorf("gamma line: %q", line)
			}
		}
	}
	if out, _ := mustRunErr(t, db, "ls", "--json"); !strings.Contains(out, `"last_check"`) || !strings.Contains(out, `"ok": true`) {
		t.Errorf("ls --json after check:\n%s", out)
	}
}

func TestAddWarnsWhenDown(t *testing.T) {
	db := checkDB(t)
	out, errOut, err := runErr(t, db, "add", "gamma", "web@gamma-down.example")
	if err != nil {
		t.Fatalf("add of a down host failed: %v", err)
	}
	_ = out
	if !strings.Contains(errOut, "Warning:") || !strings.Contains(errOut, "Connection refused") {
		t.Errorf("stderr = %q", errOut)
	}
	if got, _ := mustRunErr(t, db, "get", "gamma"); strings.TrimSpace(got) != "web@gamma-down.example" {
		t.Errorf("host not saved: %q", got)
	}
	if ls, _ := mustRunErr(t, db, "ls"); !strings.Contains(ls, "failed") {
		t.Errorf("ls after add:\n%s", ls)
	}

	// A working host adds quietly and shows ok.
	_, errOut = mustRunErr(t, db, "add", "alpha", "alpha.example")
	if strings.Contains(errOut, "Warning:") {
		t.Errorf("unexpected warning: %q", errOut)
	}
	if ls, _ := mustRunErr(t, db, "ls"); !strings.Contains(ls, "ok") {
		t.Errorf("ls:\n%s", ls)
	}
}

func TestNoCheckSkipsRunner(t *testing.T) {
	db := checkDB(t)
	var calls int32
	counting := func(ctx context.Context, args []string) error {
		atomic.AddInt32(&calls, 1)
		return fakeSSH(ctx, args)
	}

	if _, errOut, err := runApp(t, &app{checkRun: counting}, db, "add", "gamma", "gamma-down.example", "--no-check"); err != nil {
		t.Fatal(err)
	} else if strings.Contains(errOut, "Warning:") {
		t.Errorf("stderr = %q", errOut)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("add --no-check ran the runner %d times", n)
	}
	if ls, _ := mustRunErr(t, db, "ls"); strings.Contains(ls, "failed") || strings.Contains(ls, " ok") {
		t.Errorf("ls shows a check:\n%s", ls)
	}

	// edit with a field flag and --no-check saves without checking.
	_, errOut, err := runApp(t, &app{checkRun: counting}, db, "edit", "gamma", "--notes", "n", "--no-check")
	if err != nil {
		t.Fatalf("edit --no-check: %v", err)
	}
	if !strings.Contains(errOut, "Saved gamma") {
		t.Errorf("stderr = %q", errOut)
	}
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("edit --no-check ran the runner %d times", n)
	}

	// edit with a field change checks the host and warns.
	_, errOut, err = runApp(t, &app{checkRun: counting}, db, "edit", "gamma", "--env", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&calls); n != 1 || !strings.Contains(errOut, "Warning:") {
		t.Errorf("calls = %d, stderr = %q", n, errOut)
	}
}
