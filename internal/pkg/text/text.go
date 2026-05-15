package text

import (
	"strings"
	"unicode"
)

func StripDisallowed(raw string) string {
	var sb strings.Builder
	for _, r := range raw {
		// Allow printable ASCII
		if r >= 0x20 && r <= 0x7E {
			sb.WriteRune(r)
			continue
		}
		// Allow letters (including accented: ç, é, ñ, etc.) and combining marks. Doesn't allow out-of-range chars or emojis.
		if unicode.IsLetter(r) || // Allow regular letters
			unicode.IsMark(r) || // Allow combining marks (ç, é, ñ, etc.). Doesn't allow out-of-range chars or emojis.
			unicode.Is(unicode.Sc, r) { // Allow currency symbols (£, €, ¥, etc.) - they get normalized by filter code
			sb.WriteRune(r)
			continue
		}
	}
	return sb.String()
}
