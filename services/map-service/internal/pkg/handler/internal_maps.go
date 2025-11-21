package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	commonV1 "github.com/hollow-cube/hc-services/libraries/common/pkg/api"
	v1 "github.com/hollow-cube/hc-services/services/map-service/api/v1"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model/transform"
)

func (h *InternalHandler) SearchOrgMaps(ctx context.Context, params *v1.SearchOrgMapsParams) (*v1.SearchOrgMapsResponse, error) {
	var err error

	var pageSize int
	if params.PageSize != nil && *params.PageSize != "" {
		pageSize, err = strconv.Atoi(*params.PageSize)
		if err != nil {
			return nil, fmt.Errorf("invalid page size: %w", err)
		}
		if pageSize > 50 {
			return nil, &commonV1.Error{HTTP: http.StatusBadRequest, Code: "invalid_page_size", Message: "page size must be less than 50"}
		}
	}

	var page int
	if params.Page != nil && *params.Page != "" {
		page, err = strconv.Atoi(*params.Page)
		if err != nil {
			return nil, fmt.Errorf("invalid page: %w", err)
		}
	}

	results, hasNextPage, err := h.storageClient.SearchOrgMaps(ctx, page, pageSize, params.OrgId)
	if err != nil {
		return nil, err
	}

	resultsAPI := make([]*v1.MapData, len(results))
	for i, m := range results {
		resultsAPI[i] = transform.Map2API(m)
	}

	return &v1.SearchOrgMapsResponse{
		Page:     page,
		NextPage: hasNextPage,
		Results:  resultsAPI,
	}, nil
}
