package files

import (
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// maxHighlight is the largest text that gets syntax highlighting; bigger
// files are shown plain so opening them stays instant.
const maxHighlight = 512 << 10

// highlightLines returns text split into lines with ANSI colours, or nil
// when no lexer applies. Each line is self-contained (colours are reset at
// the end of every token), so lines can be truncated or wrapped freely.
// Token backgrounds are ignored to keep the terminal's own background.
func highlightLines(name, text string, dark bool) []string {
	if len(text) > maxHighlight {
		return nil
	}
	lexer := lexers.Match(name)
	if lexer == nil {
		lexer = lexers.Analyse(text)
	}
	if lexer == nil {
		return nil
	}
	styleName := "catppuccin-latte"
	if dark {
		styleName = "catppuccin-mocha"
	}
	style := styles.Get(styleName)
	it, err := chroma.Coalesce(lexer).Tokenise(nil, text)
	if err != nil {
		return nil
	}

	sgr := map[chroma.TokenType]string{}
	sgrFor := func(t chroma.TokenType) string {
		if s, ok := sgr[t]; ok {
			return s
		}
		e := style.Get(t)
		var codes []string
		if e.Bold == chroma.Yes {
			codes = append(codes, "1")
		}
		if e.Italic == chroma.Yes {
			codes = append(codes, "3")
		}
		if e.Colour.IsSet() {
			codes = append(codes, fmt.Sprintf("38;2;%d;%d;%d", e.Colour.Red(), e.Colour.Green(), e.Colour.Blue()))
		}
		s := ""
		if len(codes) > 0 {
			s = "\x1b[" + strings.Join(codes, ";") + "m"
		}
		sgr[t] = s
		return s
	}

	var lines []string
	var cur strings.Builder
	for tok := it(); tok != chroma.EOF; tok = it() {
		s := sgrFor(tok.Type)
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				lines = append(lines, cur.String())
				cur.Reset()
			}
			if part == "" {
				continue
			}
			if s == "" {
				cur.WriteString(part)
			} else {
				cur.WriteString(s + part + "\x1b[0m")
			}
		}
	}
	lines = append(lines, cur.String())
	return lines
}
