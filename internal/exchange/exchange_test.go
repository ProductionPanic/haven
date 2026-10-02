package exchange

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ProductionPanic/rootnet-cli/internal/store"
)

func TestTOMLRoundTrip(t *testing.T) {
	in := []store.Host{
		{Name: "a.nl", User: "web", Hostname: "srv", Port: 22, Tags: []string{"wp"}},
		{Name: "b.nl", Hostname: "alias", Port: 2222, JumpHost: "bastion", Notes: "multi\nline"},
	}
	var buf bytes.Buffer
	if err := WriteTOML(&buf, in); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "port = 22\n") {
		t.Errorf("default port should be omitted:\n%s", buf.String())
	}
	out, err := ReadTOML(&buf)
	if err != nil {
		t.Fatal(err)
	}
	in[0].Port = 0
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip mismatch\n in %+v\nout %+v", in, out)
	}
	if _, err := ReadTOML(strings.NewReader("[[host]]\nname='x'\nbogus=1\n")); err == nil {
		t.Error("expected unknown key error")
	}
}

func TestSSHConfig(t *testing.T) {
	var buf bytes.Buffer
	WriteSSHConfig(&buf, []store.Host{{Name: "my site.nl", User: "u", Hostname: "h", Port: 2200, JumpHost: "j"}})
	want := "\nHost my-site.nl\n    HostName h\n    User u\n    Port 2200\n    ProxyJump j\n"
	if !strings.HasSuffix(buf.String(), want) {
		t.Errorf("got:\n%s", buf.String())
	}
}

func TestImport(t *testing.T) {
	ctx := context.Background()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Create(ctx, store.Host{Name: "a", Hostname: "old"})
	hosts := []store.Host{{Name: "a", Hostname: "new"}, {Name: "b", Hostname: "x"}}

	res := Import(ctx, s, hosts, false)
	if res.Created != 1 || res.Updated != 0 || len(res.Errors) != 1 {
		t.Fatalf("no-overwrite result %+v", res)
	}
	res = Import(ctx, s, hosts, true)
	if res.Created != 0 || res.Updated != 2 || len(res.Errors) != 0 {
		t.Fatalf("overwrite result %+v", res)
	}
	if h, _ := s.Get(ctx, "a"); h.Hostname != "new" {
		t.Errorf("a not updated: %+v", h)
	}
}
