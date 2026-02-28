package player

import (
	"context"
	"errors"
	"time"

	"github.com/hollow-cube/hc-services/services/session-service/internal/pkg/posthog"
	"go.uber.org/zap"
)

func (t *Tracker) playerCountReportLoop() {
	for {
		select {
		case <-t.countReportCtx.Done():
			return
		default: // Continue
		}

		t.lockAndLoopCount()
	}
}

func (t *Tracker) lockAndLoopCount() {
	ctx, cancel, err := t.countReportLocker.WithContext(t.countReportCtx, "count_v1")
	if errors.Is(err, context.Canceled) {
		return
	} else if err != nil {
		zap.S().Errorw("failed to acquire lock for count report", "err", err)
		return
	}
	defer cancel()

	for {
		// Want to run it every 5 minutes aligned to epoch, so if it starts at 03 it will run at 05, 10, etc.
		nextRun := time.Now().Truncate(5 * time.Minute).Add(5 * time.Minute)
		select {
		case <-time.After(time.Until(nextRun)):
			t.reportPlayerCount(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (t *Tracker) reportPlayerCount(ctx context.Context) {
	sessions, err := t.GetAllSessions(ctx)
	if errors.Is(err, context.Canceled) {
		return
	} else if err != nil {
		zap.S().Errorw("failed to fetch player sessions", "err", err)
		return
	}

	posthog.Enqueue(posthog.Capture{
		Event:      "player_count",
		DistinctId: posthog.InternalID,
		Properties: posthog.NewProperties().
			Set("count", len(sessions)),
	})
}
