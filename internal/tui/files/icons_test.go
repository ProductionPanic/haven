package files

import (
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/tui/theme"
	"github.com/ProductionPanic/rootnet-cli/v2/internal/vfs"
)

func TestIconFor(t *testing.T) {
	cases := []struct {
		e    vfs.Entry
		want string
	}{
		{vfs.Entry{Name: "index.php"}, ""},
		{vfs.Entry{Name: "App.TSX"}, ""},                 // case-insensitive
		{vfs.Entry{Name: "wp-config.php"}, ""},           // name beats extension
		{vfs.Entry{Name: "wp-content", IsDir: true}, ""}, // special directory
		{vfs.Entry{Name: "src", IsDir: true}, glyphDir},   // plain directory
		{vfs.Entry{Name: "backup.tar.gz"}, ""},           // last extension
		{vfs.Entry{Name: "Dockerfile"}, ""},
		{vfs.Entry{Name: "deploy", Mode: 0o755}, glyphExec}, // executable without extension
		{vfs.Entry{Name: "README"}, glyphFile},              // unknown
		{vfs.Entry{Name: "x.", Mode: 0o644}, glyphFile},     // trailing dot
		{vfs.Entry{Name: "dump.sql.rootnet-part"}, ""},     // in-progress upload
		{vfs.Entry{Name: "current", IsDir: true, Link: "/srv/releases/42"}, glyphLinkDir},
		{vfs.Entry{Name: "gone", Link: "/nope", Broken: true}, glyphBrokenLink},
		{vfs.Entry{Name: "conf", Link: "/etc/x", Mode: fs.ModeSymlink}, glyphLinkFile},
		{vfs.Entry{Name: parentName, IsDir: true}, glyphParent},
	}
	for _, c := range cases {
		if got := iconFor(c.e).glyph; got != c.want {
			t.Errorf("iconFor(%q) = %U, want %U", c.e.Name, []rune(got), []rune(c.want))
		}
	}
}

func TestIconsFromEnv(t *testing.T) {
	for v, want := range map[string]bool{"": true, "nerd": true, "off": false, "0": false, "NONE": false} {
		t.Setenv("ROOTNET_ICONS", v)
		if got := IconsFromEnv(); got != want {
			t.Errorf("ROOTNET_ICONS=%q → %v, want %v", v, got, want)
		}
	}
}

func TestRowIconsKeepAlignment(t *testing.T) {
	th := theme.New(true)
	for _, icons := range []bool{true, false} {
		p := newPane("Local")
		p.icons = icons
		var widths []int
		for _, e := range []vfs.Entry{{Name: "index.php", Size: 6}, {Name: "wp-content", IsDir: true}, {Name: "x"}} {
			row := ansi.Strip(p.row(th, e, false, false, 60, time.Now()))
			widths = append(widths, ansi.StringWidth(row))
			if icons && !strings.ContainsAny(row, "") {
				t.Errorf("no icon in %q", row)
			}
		}
		if widths[0] != widths[1] || widths[1] != widths[2] {
			t.Errorf("icons=%v: rows have different widths %v", icons, widths)
		}
	}
}

func TestLongTitleStaysOnOneLine(t *testing.T) {
	p := newPane("bengelmedia.nl")
	p.fs = vfs.LocalFS{}
	p.cwd = "/projects/bengelmed_da/public_html/wp-content/themes/bengel/assets/build"
	p.width, p.height = 40, 6
	out := ansi.Strip(p.view(theme.New(true), true, time.Now()))
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[1], "…") || !strings.HasSuffix(strings.TrimRight(lines[1], " │"), "assets/build") {
		t.Errorf("title not truncated on the left:\n%s", out)
	}
	if strings.Contains(lines[2], "build") {
		t.Errorf("title wrapped:\n%s", out)
	}
}
