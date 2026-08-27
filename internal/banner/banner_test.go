package banner

import "testing"

func TestArt(t *testing.T) {
	want := [3]string{
		"█▀▄ █▀▀ █ █     ▄█",
		"█ █ █▀  █ █ ▀▀▀  █",
		"▀▀  ▀▀▀  ▀      ▀▀▀",
	}
	if got := Art("dev-1"); got != want {
		t.Errorf("Art(dev-1):\n%s\n%s\n%s\nwant:\n%s\n%s\n%s",
			got[0], got[1], got[2], want[0], want[1], want[2])
	}
	// Runes outside the font are skipped, not rendered as garbage.
	if Art("d_e_v") != Art("dev") {
		t.Error("unknown runes must be skipped")
	}
}

// Box names are DNS labels: every rune the schema can produce needs a glyph,
// and each glyph must be rectangular or the half-block packing tears.
func TestFontCoversDNSLabels(t *testing.T) {
	for _, r := range "abcdefghijklmnopqrstuvwxyz0123456789-" {
		g, ok := glyphs[r]
		if !ok {
			t.Errorf("no glyph for %q", r)
			continue
		}
		for row := 1; row < 5; row++ {
			if len(g[row]) != len(g[0]) {
				t.Errorf("glyph %q: row %d width %d != %d", r, row, len(g[row]), len(g[0]))
			}
		}
	}
}

func TestColorStableAndDistinct(t *testing.T) {
	for _, name := range []string{"valhalla", "dev-1", "mjqwz0"} {
		c := Color(name)
		if c != Color(name) {
			t.Errorf("Color(%q) is not deterministic", name)
		}
		found := false
		for _, p := range palette {
			if c == p {
				found = true
			}
		}
		if !found {
			t.Errorf("Color(%q) = %d is outside the palette", name, c)
		}
	}
	if Color("valhalla") == Color("dev-1") && Color("dev-1") == Color("mjqwz0") {
		t.Error("palette hashing degenerated to one color")
	}
}
