package hostlist

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	in := `acme-shop.example | web@web1.example.com
# comment

bare | justahost
broken line
 | nobody@nowhere
`
	hosts, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("got %d hosts: %+v", len(hosts), hosts)
	}
	if h := hosts[0]; h.Name != "acme-shop.example" || h.User != "web" || h.Hostname != "web1.example.com" {
		t.Errorf("unexpected first host %+v", h)
	}
	if h := hosts[1]; h.User != "" || h.Hostname != "justahost" {
		t.Errorf("unexpected second host %+v", h)
	}
}
