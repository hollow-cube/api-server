package db

import (
	"context"

	"github.com/google/uuid"
)

// SafeLookupPlayerIdByIdOrUsername looks up a player ID by their ID or username to see if they exist (or convert username -> uuid)
// does it in a safe manner so that an invalid uuid is not passed to the DB query method.
func (q *Queries) SafeLookupPlayerIdByIdOrUsername(ctx context.Context, idOrUsername string) (string, error) {
	parsedId, _ := uuid.Parse(idOrUsername) // ignore the error as it might be a username to search by - we just can't pass in an invalid UUID
	return q.LookupPlayerByIdOrUsername(ctx, parsedId.String(), idOrUsername)
}
