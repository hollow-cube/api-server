package v4Internal

import (
	"context"
	"strconv"
	"strings"

	"github.com/hollow-cube/api-server/internal/mapdb"
)

type (
	PaginatedHeadList struct {
		Count   int    `json:"count"`
		Results []Head `json:"results"`
	}
	Head struct {
		Category string   `json:"category"`
		Id       string   `json:"id"`
		Name     string   `json:"name"`
		Tags     []string `json:"tags"`
		Texture  string   `json:"texture"`
	}
)

type SearchHeadDatabaseRequest struct {
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
	Query    string `query:"query"`
}

// GET /head-database/search
func (s *Server) SearchHeadDatabase(ctx context.Context, request SearchHeadDatabaseRequest) (*PaginatedHeadList, error) {
	offset, limit := defaultPageParams(request.Page, request.PageSize)

	queryString := strings.TrimSpace(request.Query)
	if queryString == "" {
		heads, err := s.mapStore.GetRandomHeads(ctx, limit)
		if err != nil {
			return nil, err
		}

		results := make([]Head, len(heads))
		for i, head := range heads {
			results[i] = s.hydrateHead(head)
		}
		return &PaginatedHeadList{
			Results: results,
			Count:   int(limit),
		}, nil
	}

	query := "%" + queryString + "%"
	heads, err := s.mapStore.GetHeadsWithSearch(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}

	result := PaginatedHeadList{Results: make([]Head, len(heads))}
	for i, head := range heads {
		result.Results[i] = s.hydrateHead(head.HeadDb)
		result.Count = int(head.TotalCount)
	}
	return &result, nil
}

type GetHeadDatabaseCategoryRequest struct {
	Category string `path:"category"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

// GET /head-database/{category}
func (s *Server) GetHeadDatabaseCategory(ctx context.Context, request GetHeadDatabaseCategoryRequest) (*PaginatedHeadList, error) {
	offset, limit := defaultPageParams(request.Page, request.PageSize)

	heads, err := s.mapStore.GetHeadsWithCategory(ctx, request.Category, limit, offset)
	if err != nil {
		return nil, err
	}

	result := PaginatedHeadList{Results: make([]Head, len(heads))}
	for i, head := range heads {
		result.Results[i] = s.hydrateHead(head.HeadDb)
		result.Count = int(head.TotalCount)
	}
	return &result, nil
}

func (s *Server) hydrateHead(head mapdb.HeadDb) Head {
	return Head{
		Id:       strconv.Itoa(head.ID),
		Category: head.Category,
		Name:     head.Name,
		Tags:     head.Tags,
		Texture:  head.Texture,
	}
}
