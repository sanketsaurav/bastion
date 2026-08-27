// Package banner renders a box name as compact half-block ASCII art for the
// `bastion ssh` nameplate. Box names are DNS labels, so the embedded 3×5
// pixel font only needs a-z, 0-9, and '-'; each glyph renders into three
// text rows using upper/lower half-block characters.
package banner

import (
	"hash/fnv"
	"strings"
)

// glyphs is the pixel font: five rows per glyph, 'X' = on. Widths vary.
var glyphs = map[rune][5]string{
	'a': {".X.", "X.X", "XXX", "X.X", "X.X"},
	'b': {"XX.", "X.X", "XX.", "X.X", "XX."},
	'c': {".XX", "X..", "X..", "X..", ".XX"},
	'd': {"XX.", "X.X", "X.X", "X.X", "XX."},
	'e': {"XXX", "X..", "XX.", "X..", "XXX"},
	'f': {"XXX", "X..", "XX.", "X..", "X.."},
	'g': {".XX", "X..", "X.X", "X.X", ".XX"},
	'h': {"X.X", "X.X", "XXX", "X.X", "X.X"},
	'i': {"XXX", ".X.", ".X.", ".X.", "XXX"},
	'j': {"..X", "..X", "..X", "X.X", ".X."},
	'k': {"X.X", "XX.", "X..", "XX.", "X.X"},
	'l': {"X..", "X..", "X..", "X..", "XXX"},
	'm': {"X...X", "XX.XX", "X.X.X", "X...X", "X...X"},
	'n': {"X..X", "XX.X", "X.XX", "X..X", "X..X"},
	'o': {".X.", "X.X", "X.X", "X.X", ".X."},
	'p': {"XX.", "X.X", "XX.", "X..", "X.."},
	'q': {".XX.", "X..X", "X..X", ".XX.", "...X"},
	'r': {"XX.", "X.X", "XX.", "X.X", "X.X"},
	's': {".XX", "X..", ".X.", "..X", "XX."},
	't': {"XXX", ".X.", ".X.", ".X.", ".X."},
	'u': {"X.X", "X.X", "X.X", "X.X", "XXX"},
	'v': {"X.X", "X.X", "X.X", "X.X", ".X."},
	'w': {"X...X", "X...X", "X.X.X", "XX.XX", "X...X"},
	'x': {"X.X", "X.X", ".X.", "X.X", "X.X"},
	'y': {"X.X", "X.X", ".X.", ".X.", ".X."},
	'z': {"XXX", "..X", ".X.", "X..", "XXX"},
	'0': {"XXX", "X.X", "X.X", "X.X", "XXX"},
	'1': {".X.", "XX.", ".X.", ".X.", "XXX"},
	'2': {"XX.", "..X", ".X.", "X..", "XXX"},
	'3': {"XXX", "..X", ".XX", "..X", "XXX"},
	'4': {"X.X", "X.X", "XXX", "..X", "..X"},
	'5': {"XXX", "X..", "XX.", "..X", "XX."},
	'6': {".XX", "X..", "XXX", "X.X", "XXX"},
	'7': {"XXX", "..X", ".X.", ".X.", ".X."},
	'8': {"XXX", "X.X", "XXX", "X.X", "XXX"},
	'9': {"XXX", "X.X", "XXX", "..X", "XX."},
	'-': {"...", "...", "XXX", "...", "..."},
}

// Art renders name as three rows of half-block art. Runes outside the font
// are skipped; an empty result yields three empty rows.
func Art(name string) [3]string {
	var pixels [5]strings.Builder
	first := true
	for _, r := range name {
		g, ok := glyphs[r]
		if !ok {
			continue
		}
		for row := 0; row < 5; row++ {
			if !first {
				pixels[row].WriteByte('.')
			}
			pixels[row].WriteString(g[row])
		}
		first = false
	}

	cell := func(top, bottom byte) rune {
		switch {
		case top == 'X' && bottom == 'X':
			return '█'
		case top == 'X':
			return '▀'
		case bottom == 'X':
			return '▄'
		}
		return ' '
	}
	var out [3]string
	width := pixels[0].Len()
	rows := [5]string{pixels[0].String(), pixels[1].String(), pixels[2].String(), pixels[3].String(), pixels[4].String()}
	for i, pair := range [3][2]int{{0, 1}, {2, 3}, {4, -1}} {
		var b strings.Builder
		for col := 0; col < width; col++ {
			bottom := byte('.')
			if pair[1] >= 0 {
				bottom = rows[pair[1]][col]
			}
			b.WriteRune(cell(rows[pair[0]][col], bottom))
		}
		out[i] = strings.TrimRight(b.String(), " ")
	}
	return out
}

// palette holds visually distinct ANSI 256 colors that read on both dark and
// light terminals.
var palette = []int{75, 114, 141, 168, 178, 203, 209, 44}

// Color returns the ANSI 256-color code for name — stable per name, so every
// box keeps its own accent and a glance says which machine this is.
func Color(name string) int {
	h := fnv.New32a()
	h.Write([]byte(name))
	return palette[int(h.Sum32())%len(palette)]
}
