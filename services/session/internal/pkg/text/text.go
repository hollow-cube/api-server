package text

import "strings"

func StripSpecial(raw string) string {
	var sb strings.Builder
	for _, r := range raw {
		if r >= 0x20 && r <= 0x7E {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
