package render

import "strings"

// Runes outside the U+1F300–U+1FAFF block that terminals draw as two-cell emoji.
var wideRunes = map[rune]bool{
	'✅': true, '❌': true, '⏳': true, '✋': true, '❓': true, '❔': true,
}

const (
	variationSelector = '️' // asks for emoji presentation of the previous rune
	zeroWidthJoiner   = '‍'
)

// cellWidth returns the number of terminal cells s occupies, ignoring ANSI sequences.
// Emoji count as 2 cells, everything else used on the line as 1.
func cellWidth(s string) int {
	runes := []rune(stripANSI(s))
	w := 0
	for i, r := range runes {
		switch {
		case r == variationSelector || r == zeroWidthJoiner:
		case r >= 0x1F300 && r <= 0x1FAFF, wideRunes[r]:
			w += 2
		case i+1 < len(runes) && runes[i+1] == variationSelector:
			w += 2 // e.g. ⚙️, ⏭️
		default:
			w++
		}
	}
	return w
}

// truncateLeft cuts p from the left so it fits in limit cells: whole path segments first
// ("…/sub/project"), then characters ("…ject"). The result is at least "…".
func truncateLeft(p string, limit int) string {
	if cellWidth(p) <= limit {
		return p
	}
	segs := strings.Split(p, "/")
	for i := 1; i < len(segs); i++ {
		if c := "…/" + strings.Join(segs[i:], "/"); cellWidth(c) <= limit {
			return c
		}
	}
	runes := []rune(segs[len(segs)-1])
	for i := 1; i < len(runes); i++ {
		if c := "…" + string(runes[i:]); cellWidth(c) <= limit {
			return c
		}
	}
	return "…"
}

// stripANSI removes SGR sequences.
func stripANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
