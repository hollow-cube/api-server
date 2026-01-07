package intnl

import (
	"context"
	"github.com/hollow-cube/hc-services/services/map-service/internal/db"
	"strconv"
)

func (s *server) SearchHeadDatabase(ctx context.Context, request SearchHeadDatabaseRequestObject) (SearchHeadDatabaseResponseObject, error) {
	offset, limit := sanitizePageParams(request.Params.Page, request.Params.PageSize)
	if request.Params.Query == nil || *request.Params.Query == "" {
		heads, err := s.store.GetRandomHeads(ctx, limit)
		if err != nil {
			return nil, err
		}
		return SearchHeadDatabase200JSONResponse{Results: dbHeadsToAPI(heads)}, nil
	} else {
		query := "%" + *request.Params.Query + "%"
		heads, err := s.store.GetHeadsWithSearch(ctx, query, limit, offset)
		if err != nil {
			return nil, err
		}
		if offset == 0 {
			total, err := s.store.GetHeadCountWithSearch(ctx, query)
			if err != nil {
				return nil, err
			}
			var pages = int(total) / int(limit)
			return SearchHeadDatabase200JSONResponse{
				Results: dbHeadsToAPI(heads),
				Pages:   &pages,
			}, nil
		} else {
			return SearchHeadDatabase200JSONResponse{
				Results: dbHeadsToAPI(heads),
			}, nil
		}
	}
}

func (s *server) GetHeadDatabaseCategory(ctx context.Context, request GetHeadDatabaseCategoryRequestObject) (GetHeadDatabaseCategoryResponseObject, error) {
	offset, limit := sanitizePageParams(request.Params.Page, request.Params.PageSize)
	heads, err := s.store.GetHeadsWithCategory(ctx, request.Category, limit, offset)
	if err != nil {
		return nil, err
	} else if offset == 0 {
		total, err := s.store.GetHeadCountWithCategory(ctx, request.Category)
		if err != nil {
			return nil, err
		}
		var pages = int(total) / int(limit)
		return GetHeadDatabaseCategory200JSONResponse{
			Results: dbHeadsToAPI(heads),
			Pages:   &pages,
		}, nil
	} else {
		return GetHeadDatabaseCategory200JSONResponse{Results: dbHeadsToAPI(heads)}, nil
	}
}

func sanitizePageParams(page *int, pageSize *int) (int32, int32) {
	var offset, limit int32
	if pageSize == nil || *pageSize <= 0 || *pageSize > 100 {
		limit = int32(10)
	} else {
		limit = int32(*pageSize)
	}
	if page == nil || *page <= 0 {
		offset = int32(0)
	} else {
		offset = int32(*page) * limit
	}
	return offset, limit
}

func dbHeadsToAPI(heads []db.HeadDb) []HeadDbEntry {
	result := make([]HeadDbEntry, len(heads))
	for i, head := range heads {
		result[i] = dbHeadToAPI(head)
	}
	return result
}

func dbHeadToAPI(head db.HeadDb) HeadDbEntry {
	return HeadDbEntry{
		Id:       strconv.Itoa(head.ID),
		Category: head.Category,
		Name:     head.Name,
		Tags:     head.Tags,
		Texture:  head.Texture,
	}
}
