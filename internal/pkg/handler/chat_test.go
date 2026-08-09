package handler

import (
	"strings"
	"testing"

	"github.com/hollow-cube/api-server/internal/pkg/model"
)

func renderUrlParts(text string) string {
	parts := []model.MessagePart{&model.RawMessagePart{Type: model.PartTypeRaw, Text: text}}
	parts = regexReplaceInMessage(parts, urlRegex, func(match string) model.MessagePart {
		return &model.UrlMessagePart{Type: model.PartTypeUrl, Text: match}
	})

	var sb strings.Builder
	for _, part := range parts {
		switch part := part.(type) {
		case *model.RawMessagePart:
			sb.WriteString(part.Text)
		case *model.UrlMessagePart:
			sb.WriteString("<url:")
			sb.WriteString(part.Text)
			sb.WriteString(">")
		}
	}
	return sb.String()
}

func TestUrlDetection(t *testing.T) {
	texts := map[string]string{
		"play on mc.hypixel.net":         "play on <url:mc.hypixel.net>",
		"visit https://EXAMPLE.com/Path": "visit <url:https://EXAMPLE.com/Path>",
		"EXAMPLE.COM":                    "<url:EXAMPLE.COM>",
		"my site is cool.website":        "my site is <url:cool.website>",
		"join 1.2.3.4:25565":             "join <url:1.2.3.4:25565>",
		"sub.domain.co.uk/path?a=b":      "<url:sub.domain.co.uk/path?a=b>",
		"a example.com b test.org c":     "a <url:example.com> b <url:test.org> c",
		"end of sentence example.com.":   "end of sentence <url:example.com>.",
		"see :cool:example.com":          "see :cool:<url:example.com>",
		"what.ever dude":                 "what.ever dude",
		"lol...what":                     "lol...what",
		"file.txt":                       "file.txt",
		"score was 3.50":                 "score was 3.50",
		"i love u.s. politics":           "i love u.s. politics",
	}

	for text, expected := range texts {
		if actual := renderUrlParts(text); actual != expected {
			t.Errorf("Expected '%s' but got '%s' from '%s'", expected, actual, text)
		}
	}
}
