package text

import "context"

type Result struct {
	Engine      string
	Matched     bool
	MatchedText string
}

type Filter interface {
	Test(ctx context.Context, text string) Result
}
