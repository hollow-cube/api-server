package ox

import "time"

// Event is a single Server-Sent Event frame.
//
// Handlers that stream events return iter.Seq2[Event[T], error] where T is
// the payload type. Empty ID and Name and a zero Retry are omitted from the
// wire frame; Data is always emitted, JSON-marshaled.
type Event[T any] struct {
	ID    string
	Name  string
	Data  T
	Retry time.Duration
}
