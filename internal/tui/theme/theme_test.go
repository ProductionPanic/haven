package theme

import (
	"image/color"
	"math"
	"testing"
)

func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		s := float64(v) / 0xffff
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

func contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Help text must stay readable (WCAG AA, 4.5:1) on typical backgrounds,
// including the darker/lighter ends a translucent terminal can show.
func TestHelpContrast(t *testing.T) {
	backgrounds := map[bool][]color.Color{
		true:  {color.RGBA{0x00, 0x00, 0x00, 0xff}, color.RGBA{0x1e, 0x1e, 0x2e, 0xff}, color.RGBA{0x2a, 0x30, 0x40, 0xff}},
		false: {color.RGBA{0xff, 0xff, 0xff, 0xff}, color.RGBA{0xee, 0xee, 0xee, 0xff}},
	}
	for isDark, bgs := range backgrounds {
		h := New(isDark).Help()
		for _, bg := range bgs {
			for name, fg := range map[string]color.Color{
				"key":  h.ShortKey.GetForeground(),
				"desc": h.ShortDesc.GetForeground(),
			} {
				if c := contrast(fg, bg); c < 4.5 {
					t.Errorf("dark=%v %s on %v: contrast %.1f < 4.5", isDark, name, bg, c)
				}
			}
		}
	}
}
