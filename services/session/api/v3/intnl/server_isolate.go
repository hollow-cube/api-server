package intnl

import (
	"context"
	"time"

	"github.com/google/go-github/v56/github"
)

var (
	resultCache     GetIsolateOverrides200JSONResponse
	resultCacheTime time.Time
)

func (s *serverImpl) GetIsolateOverrides(ctx context.Context, _ GetIsolateOverridesRequestObject) (GetIsolateOverridesResponseObject, error) {
	if resultCache == nil || time.Since(resultCacheTime) > 30*time.Second {
		prs, _, err := s.gh.PullRequests.List(context.Background(), "hollow-cube", "mapmaker", &github.PullRequestListOptions{
			State: "open",
			Base:  "main",
			Sort:  "updated",
		})
		if err != nil {
			return nil, err
		}

		result := make(GetIsolateOverrides200JSONResponse, len(prs))
		for i, pr := range prs {
			result[i] = IsolateOverride{
				Id:          *pr.Head.Ref,
				LastUpdated: pr.UpdatedAt.Time,
			}
		}
		resultCache = result
		resultCacheTime = time.Now()
	}

	return resultCache, nil
}
