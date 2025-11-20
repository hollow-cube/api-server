package text

import (
	"context"

	goaway "github.com/TwiN/go-away"
)

var _ Filter = &StaticFilter{}

type StaticFilter struct {
	detector *goaway.ProfanityDetector
}

func NewStaticFilter() *StaticFilter {
	detector := goaway.NewProfanityDetector().
		WithCustomDictionary(profanities, falsePositives, falseNegatives)

	return &StaticFilter{detector}
}

func (s *StaticFilter) Test(ctx context.Context, text string) (result Result) {
	result.Engine = "static"
	result.MatchedText = s.detector.ExtractProfanity(text)
	result.Matched = len(result.MatchedText) != 0
	return
}
