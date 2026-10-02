package legacy

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	in := `appelenburg.nl | web@monotone-nuthatch.sys.rootnet.io
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
	if h := hosts[0]; h.Name != "appelenburg.nl" || h.User != "web" || h.Hostname != "monotone-nuthatch.sys.rootnet.io" {
		t.Errorf("unexpected first host %+v", h)
	}
	if h := hosts[1]; h.User != "" || h.Hostname != "justahost" {
		t.Errorf("unexpected second host %+v", h)
	}
}
