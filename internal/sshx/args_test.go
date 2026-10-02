package sshx

import (
	"reflect"
	"testing"

	"github.com/ProductionPanic/rootnet-cli/internal/store"
)

func TestConnectArgs(t *testing.T) {
	cases := []struct {
		h    store.Host
		want []string
	}{
		{store.Host{User: "web", Hostname: "srv", Port: 22}, []string{"web@srv"}},
		{store.Host{Hostname: "alias"}, []string{"alias"}},
		{
			store.Host{User: "u", Hostname: "h", Port: 2222, IdentityFile: "~/.ssh/k", JumpHost: "bastion", ExtraArgs: `-o "ServerAliveInterval 30" -A`},
			[]string{"-p", "2222", "-i", "~/.ssh/k", "-J", "bastion", "-o", "ServerAliveInterval 30", "-A", "u@h"},
		},
		{
			store.Host{User: "u", Hostname: "h", RemotePath: "/var/www/my site"},
			[]string{"-t", "u@h", `cd '/var/www/my site' && exec "$SHELL" -l`},
		},
		{
			store.Host{User: "u", Hostname: "h", RemotePath: "~/public_html"},
			[]string{"-t", "u@h", `cd ~/public_html && exec "$SHELL" -l`},
		},
	}
	for _, c := range cases {
		got, err := ConnectArgs(c.h)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ConnectArgs(%+v)\n got %q\nwant %q", c.h, got, c.want)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	got, err := SplitArgs(`a  'b c' "d \"e\"" f\ g ''`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b c", `d "e"`, "f g", ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q", got, want)
	}
	if _, err := SplitArgs(`'open`); err == nil {
		t.Error("expected error for unterminated quote")
	}
}

func TestQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/var/www": "/var/www",
		"it's":     `'it'\''s'`,
		"~":        "~",
		"~/a b":    "~/'a b'",
		"$HOME/x":  "'$HOME/x'",
	} {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %q, want %q", in, got, want)
		}
	}
}
