// Package check tests whether an ssh login to a host still works.
package check

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ProductionPanic/haven/v2/internal/sshx"
	"github.com/ProductionPanic/haven/v2/internal/store"
)

// MaxAge is how long a check result counts as fresh for automatic checks.
const MaxAge = 5 * time.Minute

// Workers is how many hosts are checked at the same time.
const Workers = 8

// Runner runs ssh with args and returns an error when the login fails.
type Runner func(ctx context.Context, args []string) error

// Args returns the ssh argv (excluding "ssh") for a non-interactive login
// that runs "true" on h.
func Args(h store.Host) ([]string, error) {
	opts, err := sshx.Options(h)
	if err != nil {
		return nil, err
	}
	args := append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5"}, opts...)
	return append(args, h.Target(), "true"), nil
}

// SSH runs the system ssh binary. The error is ssh's last line of stderr.
func SSH(ctx context.Context, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New("timed out")
	}
	if msg := lastLine(stderr.String()); msg != "" {
		return errors.New(msg)
	}
	return err
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// Host checks h with run at time now.
func Host(ctx context.Context, run Runner, h store.Host, now time.Time) store.Check {
	args, err := Args(h)
	if err == nil {
		err = run(ctx, args)
	}
	c := store.Check{At: now, OK: err == nil}
	if err != nil {
		c.Error = err.Error()
	}
	return c
}

// Hosts checks hosts concurrently (at most Workers at a time) and returns
// the results in the same order.
func Hosts(ctx context.Context, run Runner, hosts []store.Host, now func() time.Time) []store.Check {
	out := make([]store.Check, len(hosts))
	sem := make(chan struct{}, Workers)
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = Host(ctx, run, h, now())
		}()
	}
	wg.Wait()
	return out
}

// Status is a short word for a check result: "ok", "failed" or "-".
func Status(c *store.Check) string {
	switch {
	case c == nil:
		return "-"
	case c.OK:
		return "ok"
	default:
		return "failed"
	}
}

// Summary describes a failed check for messages.
func Summary(h store.Host, c store.Check) string {
	return fmt.Sprintf("ssh to %s (%s) failed: %s", h.Name, h.Target(), c.Error)
}
