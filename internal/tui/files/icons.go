package files

import (
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/transfer"
	"github.com/ProductionPanic/rootnet-cli/v2/internal/vfs"
)

// IconsFromEnv reports whether Nerd Font icons should be shown. They are on
// unless ROOTNET_ICONS is set to off/none/0/false.
func IconsFromEnv() bool {
	switch strings.ToLower(os.Getenv("ROOTNET_ICONS")) {
	case "off", "none", "0", "false", "no":
		return false
	}
	return true
}

// icon is a Nerd Font glyph with its colour. A nil colour means "use the
// default for the entry" (accent for directories, plain text otherwise).
type icon struct {
	glyph string
	color color.Color
}

func hex(s string) color.Color { return lipgloss.Color(s) }

// Glyphs (Nerd Fonts v3).
const (
	glyphDir        = "" // nf-fa-folder
	glyphDirOpen    = "" // nf-fa-folder_open
	glyphParent     = "" // nf-fa-level_up
	glyphFile       = "" // nf-fa-file
	glyphLinkFile   = "" // nf-oct-file_symlink_file
	glyphLinkDir    = "" // nf-oct-file_symlink_directory
	glyphBrokenLink = "" // nf-fa-chain_broken
	glyphExec       = "" // nf-oct-terminal
)

var (
	cPHP     = hex("#8892BF")
	cJS      = hex("#E8C547")
	cTS      = hex("#3178C6")
	cGo      = hex("#00ADD8")
	cPy      = hex("#4B8BBE")
	cRuby    = hex("#CC342D")
	cHTML    = hex("#E44D26")
	cCSS     = hex("#42A5F5")
	cSass    = hex("#CD6799")
	cJSON    = hex("#CBCB41")
	cMD      = hex("#519ABA")
	cConfig  = hex("#6D8086")
	cShell   = hex("#4EAA25")
	cDB      = hex("#DAD8D8")
	cImage   = hex("#A074C4")
	cMedia   = hex("#FD971F")
	cArchive = hex("#ECA517")
	cPDF     = hex("#E5252A")
	cOffice  = hex("#2B7CD3")
	cSheet   = hex("#1F9D55")
	cKey     = hex("#E6B422")
	cWP      = hex("#21759B")
	cGit     = hex("#F14E32")
	cDocker  = hex("#2496ED")
	cNode    = hex("#8CC84B")
	cReact   = hex("#61DAFB")
	cVue     = hex("#41B883")
	cRust    = hex("#DEA584")
	cJava    = hex("#E76F00")
	cC       = hex("#599EFF")
	cText    = hex("#9E9E9E")
)

// byName matches exact (lower-cased) file or directory names.
var byName = map[string]icon{
	"wp-config.php":       {"", cWP}, // nf-fa-wordpress
	"wp-content":          {"", cWP},
	"wp-admin":            {"", cWP},
	"wp-includes":         {"", cWP},
	".git":                {"", cGit}, // nf-fa-git
	".gitignore":          {"", cGit},
	".gitattributes":      {"", cGit},
	".gitmodules":         {"", cGit},
	".github":             {"", nil},   // nf-fa-github
	"node_modules":        {"", cNode}, // nf-dev-nodejs_small
	"package.json":        {"", cNode},
	"package-lock.json":   {"", cNode},
	"composer.json":       {"", cPHP}, // nf-dev-composer
	"composer.lock":       {"", cPHP},
	"vendor":              {"", nil},     // nf-fa-archive
	"dockerfile":          {"", cDocker}, // nf-linux-docker
	"docker-compose.yml":  {"", cDocker},
	"docker-compose.yaml": {"", cDocker},
	"compose.yml":         {"", cDocker},
	"compose.yaml":        {"", cDocker},
	".dockerignore":       {"", cDocker},
	"makefile":            {"", cConfig}, // nf-dev-gnu
	".htaccess":           {"", cConfig}, // nf-seti-config
	".env":                {"", cKey},    // nf-fa-key
	"license":             {"", cText},   // nf-fa-book
	"readme.md":           {"", cMD},     // nf-oct-markdown
	"go.mod":              {"", cGo},
	"go.sum":              {"", cGo},
	"cargo.toml":          {"", cRust},
	".ssh":                {"", cKey},
}

// byExt matches lower-cased extensions without the dot.
var byExt = map[string]icon{}

func init() {
	add := func(ic icon, exts ...string) {
		for _, e := range exts {
			byExt[e] = ic
		}
	}
	add(icon{"", cPHP}, "php", "phtml", "inc")         // nf-dev-php
	add(icon{"", cJS}, "js", "mjs", "cjs")             // nf-dev-javascript
	add(icon{"", cTS}, "ts", "mts", "cts")             // nf-seti-typescript
	add(icon{"", cReact}, "jsx", "tsx")                // nf-dev-react
	add(icon{"", cVue}, "vue")                         // nf-seti-vue
	add(icon{"", cGo}, "go")                           // nf-seti-go
	add(icon{"", cPy}, "py", "pyc")                    // nf-seti-python
	add(icon{"", cRuby}, "rb", "erb", "gemspec")       // nf-dev-ruby
	add(icon{"", cRust}, "rs")                         // nf-dev-rust
	add(icon{"", cJava}, "java", "jar", "class")       // nf-dev-java
	add(icon{"", cC}, "c", "h")                        // nf-custom-c
	add(icon{"", cC}, "cpp", "cc", "hpp", "cxx")       // nf-custom-cpp
	add(icon{"", cHTML}, "html", "htm", "twig", "tpl") // nf-fa-html5
	add(icon{"", cCSS}, "css")                         // nf-dev-css3
	add(icon{"", cSass}, "scss", "sass", "less")       // nf-seti-sass
	add(icon{"", cJSON}, "json", "jsonc", "map")       // nf-seti-json
	add(icon{"", cConfig}, "xml", "xsl")               // nf-fa-code
	add(icon{"", cMD}, "md", "markdown", "mdx")        // nf-oct-markdown
	add(icon{"", cConfig}, "yml", "yaml", "toml", "ini", "conf", "cfg", "env", "neon", "properties")
	add(icon{"", cShell}, "sh", "bash", "zsh", "fish", "ps1", "bat", "cmd")
	add(icon{"", cDB}, "sql", "db", "sqlite", "sqlite3", "mdb") // nf-fa-database
	add(icon{"", cImage}, "png", "jpg", "jpeg", "gif", "webp", "svg", "ico", "bmp", "tif", "tiff", "avif", "heic", "psd")
	add(icon{"", cMedia}, "mp4", "mkv", "mov", "webm", "avi", "m4v") // nf-fa-video_camera
	add(icon{"", cMedia}, "mp3", "wav", "flac", "ogg", "m4a", "aac") // nf-fa-music
	add(icon{"", cArchive}, "zip", "tar", "gz", "tgz", "bz2", "xz", "zst", "7z", "rar", "lz4", "wpress")
	add(icon{"", cPDF}, "pdf")
	add(icon{"", cOffice}, "doc", "docx", "odt", "rtf")
	add(icon{"", cSheet}, "xls", "xlsx", "ods", "csv", "tsv")
	add(icon{"", cPDF}, "ppt", "pptx", "odp")
	add(icon{"", cText}, "ttf", "otf", "woff", "woff2", "eot") // nf-fa-font
	add(icon{"", cText}, "txt", "log", "out")                  // nf-fa-file_text
	add(icon{"", cText}, "lock")                               // nf-fa-lock
	add(icon{"", cKey}, "pem", "key", "crt", "cer", "pub", "p12", "pfx", "gpg", "asc")
	add(icon{"", cText}, "bak", "old", "orig", "swp") // nf-fa-archive
}

// iconFor picks the icon for an entry.
func iconFor(e vfs.Entry) icon {
	name := strings.ToLower(e.Name)
	switch {
	case e.Name == parentName:
		return icon{glyphParent, nil}
	case e.Link != "" && e.Broken:
		return icon{glyphBrokenLink, nil}
	case e.Link != "" && e.IsDir:
		return icon{glyphLinkDir, nil}
	}
	if ic, ok := byName[name]; ok {
		return ic
	}
	if e.IsDir {
		return icon{glyphDir, nil}
	}
	if e.Link != "" {
		return icon{glyphLinkFile, nil}
	}
	if strings.HasSuffix(name, transfer.PartSuffix) {
		return icon{"", cText} // nf-fa-spinner: in-progress upload
	}
	if i := strings.LastIndexByte(name, '.'); i >= 0 && i < len(name)-1 {
		if ic, ok := byExt[name[i+1:]]; ok {
			return ic
		}
	}
	if e.Mode&0o111 != 0 {
		return icon{glyphExec, cShell}
	}
	return icon{glyphFile, nil}
}
