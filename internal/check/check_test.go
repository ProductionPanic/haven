package check

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ProductionPanic/haven/v2/internal/store"
)

func TestArgs(t *testing.T) {
	tests := []struct {
		name string
		host store.Host
		want []string
	}{
		{"plain", store.Host{Hostname: "srv"},
			[]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "srv", "true"}},
		{"default port is omitted", store.Host{User: "web", Hostname: "srv", Port: 22},
			[]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "web@srv", "true"}},
		{"all options", store.Host{User: "web", Hostname: "srv", Port: 2222, IdentityFile: "/k", JumpHost: "j@bastion"},
			[]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-p", "2222", "-i", "/k", "-J", "j@bastion", "web@srv", "true"}},
		{"extra args", store.Host{Hostname: "srv", ExtraArgs: `-o "StrictHostKeyChecking=no"`},
			[]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "StrictHostKeyChecking=no", "srv", "true"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Args(tt.host)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Args = %q, want %q", got, tt.want)
			}
		})
	}
	if _, err := Args(store.Host{Name: "x", Hostname: "srv", ExtraArgs: `"unterminated`}); err == nil {
		t.Error("expected error for bad extra args")
	}
}

func TestHost(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := store.Host{Hostname: "srv"}

	var gotArgs []string
	ok := Host(context.Background(), func(_ context.Context, args []string) error {
		gotArgs = args
		return nil
	}, h, now)
	if !ok.OK || ok.Error != "" || !ok.At.Equal(now) {
		t.Errorf("ok check = %+v", ok)
	}
	if want, _ := Args(h); !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("runner args = %q, want %q", gotArgs, want)
	}

	bad := Host(context.Background(), func(context.Context, []string) error {
		return errors.New("Permission denied")
	}, h, now)
	if bad.OK || bad.Error != "Permission denied" || !bad.At.Equal(now) {
		t.Errorf("failed check = %+v", bad)
	}

	// A bad host config fails without running ssh.
	called := false
	c := Host(context.Background(), func(context.Context, []string) error { called = true; return nil },
		store.Host{Name: "x", Hostname: "srv", ExtraArgs: `"oops`}, now)
	if c.OK || c.Error == "" || called {
		t.Errorf("bad args: check = %+v, called = %v", c, called)
	}
}

func TestHostsOrderAndConcurrency(t *testing.T) {
	const n = Workers*3 + 1
	hosts := make([]store.Host, n)
	for i := range hosts {
		hosts[i] = store.Host{Hostname: string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".example"}
		if i%3 == 0 {
			hosts[i].Hostname = "down-" + hosts[i].Hostname
		}
	}
	var running, peak int32
	run := func(_ context.Context, args []string) error {
		cur := atomic.AddInt32(&running, 1)
		defer atomic.AddInt32(&running, -1)
		for {
			p := atomic.LoadInt32(&peak)
			if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		if len(args) >= 2 && len(args[len(args)-2]) > 4 && args[len(args)-2][:4] == "down" {
			return errors.New("down: " + args[len(args)-2])
		}
		return nil
	}
	now := time.Now()
	got := Hosts(context.Background(), run, hosts, func() time.Time { return now })
	if len(got) != n {
		t.Fatalf("got %d results, want %d", len(got), n)
	}
	for i, h := range hosts {
		wantDown := i%3 == 0
		if got[i].OK == wantDown {
			t.Errorf("result %d OK = %v, want %v", i, got[i].OK, !wantDown)
		}
		if wantDown && got[i].Error != "down: "+h.Hostname {
			t.Errorf("result %d error = %q (out of order?)", i, got[i].Error)
		}
	}
	if p := atomic.LoadInt32(&peak); p > Workers || p < 2 {
		t.Errorf("peak concurrency = %d, want between 2 and %d", p, Workers)
	}
	if got := Hosts(context.Background(), run, nil, time.Now); len(got) != 0 {
		t.Errorf("Hosts(nil) = %v", got)
	}
}

func TestStatus(t *testing.T) {
	tests := []struct {
		c    *store.Check
		want string
	}{
		{nil, "-"},
		{&store.Check{OK: true}, "ok"},
		{&store.Check{OK: false, Error: "x"}, "failed"},
	}
	for _, tt := range tests {
		if got := Status(tt.c); got != tt.want {
			t.Errorf("Status(%+v) = %q, want %q", tt.c, got, tt.want)
		}
	}
}

func TestSummary(t *testing.T) {
	h := store.Host{Name: "a.nl", User: "web", Hostname: "srv"}
	want := "ssh to a.nl (web@srv) failed: Connection refused"
	if got := Summary(h, store.Check{Error: "Connection refused"}); got != want {
		t.Errorf("Summary = %q, want %q", got, want)
	}
}
