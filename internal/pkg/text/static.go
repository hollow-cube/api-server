package text

import (
	"context"
	"go.uber.org/zap"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"strings"
	"unicode"
)

var charactersToReplace = map[rune]rune{
	'4': 'a',
	'@': 'a',
	'3': 'e',
	'1': 'i',
	'0': 'o',
	'5': 's',
	'7': 't',
	'8': 'b',
	'9': 'g',
	'+': 't',
	'$': 's',
	'(': 'c',
	'{': 'c',
	'[': 'c',
	'!': 'i',
	'|': 'i',
	'£': 'e',
	'€': 'e',
	'¥': 'y',
	'¢': 'c',
	'<': 'c',
}
var multiCharactersToReplace = map[rune]map[rune]rune{
	'(': {
		')': 'o',
	},
	'[': {
		']': 'o',
	},
	'{': {
		'}': 'o',
	},
	'<': {
		'>': 'o',
	},
}

var _ Filter = &StaticFilter{}

type StaticFilter struct {
	trie FilterTrie
}

func NewStaticFilter() *StaticFilter {
	trie := FilterTrie{}
	for word, negatives := range profanities {
		trie.Put(word, negatives...)
	}

	return &StaticFilter{trie: trie}
}

func (c StaticFilter) Test(_ context.Context, text string) (result Result) {
	sanitized := sanitize(text)
	matched := c.trie.Test(sanitized)

	result.Engine = "custom"
	result.Matched = matched != nil
	if matched != nil {
		result.MatchedText = *matched
	}
	return
}

func sanitize(text string) string {
	// If transforming fails it's not the end of the world, we can just use the original text
	transformed, _, err := transform.String(
		transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC),
		text,
	)
	if err != nil {
		zap.S().Errorf("failed to sanitize text: %v", err)
		transformed = text
	}

	builder := strings.Builder{}
	for i := 0; i < len(transformed); i++ {
		char := rune(transformed[i])

		multiReplacement, isPartOfMulti := multiCharactersToReplace[char]
		if isPartOfMulti && i+1 < len(transformed) {
			nextChar := rune(transformed[i+1])

			if replacement, ok := multiReplacement[nextChar]; ok {
				builder.WriteRune(replacement)
				i++ // Skip the next character since it's part of the multi-character replacement
				continue
			}
		}

		if replacement, ok := charactersToReplace[char]; ok {
			builder.WriteRune(replacement)
		} else if char >= 'A' && char <= 'Z' {
			builder.WriteRune(char + ('a' - 'A'))
		} else if char >= 'a' && char <= 'z' {
			builder.WriteRune(char)
		} else if char >= '0' && char <= '9' {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}
