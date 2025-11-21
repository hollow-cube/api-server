package v1

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	oapi_rt "github.com/mworzala/openapi-go/pkg/oapi-rt"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type InternalServer interface {
	SearchOrgMaps(ctx context.Context, params *SearchOrgMapsParams) (*SearchOrgMapsResponse, error)
	GetLegacyMaps(ctx context.Context, playerId string) ([]*GetLegacyMapsResponseItem, error)
	ImportLegacyMap(ctx context.Context, playerId string, legacyMapId string) (*MapWithSlot, error)
	GetLegacyMapWorld(ctx context.Context, playerId string, legacyMapId string) (*MapWorldData, error)
}

type InternalServerWrapper struct {
	log         *zap.SugaredLogger
	middlewares []oapi_rt.Middleware
	handler     InternalServer
}

type InternalServerWrapperParams struct {
	fx.In
	Log     *zap.SugaredLogger
	Handler InternalServer

	Middleware []oapi_rt.Middleware `group:"internal_middleware"`
}

func NewInternalServerWrapper(p InternalServerWrapperParams) (*InternalServerWrapper, error) {
	sw := &InternalServerWrapper{
		log:         p.Log.With("handler", "internal (wrapper)"),
		handler:     p.Handler,
		middlewares: p.Middleware,
	}

	return sw, nil
}

func (sw *InternalServerWrapper) Apply(r chi.Router) {
	r.Route("/v1/internal", func(r chi.Router) {
		r.Get("/maps/search_orgs", sw.SearchOrgMaps)
		r.Get("/maps/legacy/{playerId}", sw.GetLegacyMaps)
		r.Post("/maps/legacy/{playerId}/{legacyMapId}/import", sw.ImportLegacyMap)
		r.Get("/maps/legacy/{playerId}/{legacyMapId}/world", sw.GetLegacyMapWorld)
	})
}

func (sw *InternalServerWrapper) SearchOrgMaps(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	var params SearchOrgMapsParams
	if err := oapi_rt.ReadExplodedQuery(r, &params); err != nil {
		oapi_rt.WriteGenericError(w, err)
		return
	}

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		code200, err := sw.handler.SearchOrgMaps(ctx, &params)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}

		if code200 != nil {
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(200)
			if err = json.NewEncoder(w).Encode(code200); err != nil {
				sw.log.Errorw("failed to encode response", "err", err)
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}

		// !! UNDEFINED EMPTY BEHAVIOR !!
		// Set `x-type: empty` on a response to define this behavior.
		sw.log.Errorw("empty response")
		w.WriteHeader(http.StatusInternalServerError)
	})
	for _, middleware := range sw.middlewares {
		handler = middleware.Run(handler)
	}
	handler.ServeHTTP(w, r)
}

func (sw *InternalServerWrapper) GetLegacyMaps(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	playerId := chi.URLParam(r, "playerId")

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		code200, err := sw.handler.GetLegacyMaps(ctx, playerId)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}

		if code200 != nil {
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(200)
			if err = json.NewEncoder(w).Encode(code200); err != nil {
				sw.log.Errorw("failed to encode response", "err", err)
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}

		// !! UNDEFINED EMPTY BEHAVIOR !!
		// Set `x-type: empty` on a response to define this behavior.
		sw.log.Errorw("empty response")
		w.WriteHeader(http.StatusInternalServerError)
	})
	for _, middleware := range sw.middlewares {
		handler = middleware.Run(handler)
	}
	handler.ServeHTTP(w, r)
}

func (sw *InternalServerWrapper) ImportLegacyMap(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	playerId := chi.URLParam(r, "playerId")
	legacyMapId := chi.URLParam(r, "legacyMapId")

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		code200, err := sw.handler.ImportLegacyMap(ctx, playerId, legacyMapId)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}

		if code200 != nil {
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(200)
			if err = json.NewEncoder(w).Encode(code200); err != nil {
				sw.log.Errorw("failed to encode response", "err", err)
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}

		w.WriteHeader(404)
	})
	for _, middleware := range sw.middlewares {
		handler = middleware.Run(handler)
	}
	handler.ServeHTTP(w, r)
}

func (sw *InternalServerWrapper) GetLegacyMapWorld(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	playerId := chi.URLParam(r, "playerId")
	legacyMapId := chi.URLParam(r, "legacyMapId")

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		code200, err := sw.handler.GetLegacyMapWorld(ctx, playerId, legacyMapId)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}

		if code200 != nil {
			switch {
			case code200.Polar != nil:
				w.Header().Set("content-type", "application/vnd.hollowcube.polar")
				w.WriteHeader(200)
				_, _ = w.Write(code200.Polar)
				return
			case code200.Anvil != nil:
				w.Header().Set("content-type", "application/vnd.hollowcube.anvil")
				w.WriteHeader(200)
				_, _ = w.Write(code200.Anvil)
				return
			case code200.Anvil18 != nil:
				w.Header().Set("content-type", "application/vnd.hollowcube.anvil-1_8")
				w.WriteHeader(200)
				_, _ = w.Write(code200.Anvil18)
				return
			}
		}

		// !! UNDEFINED EMPTY BEHAVIOR !!
		// Set `x-type: empty` on a response to define this behavior.
		sw.log.Errorw("empty response")
		w.WriteHeader(http.StatusInternalServerError)
	})
	for _, middleware := range sw.middlewares {
		handler = middleware.Run(handler)
	}
	handler.ServeHTTP(w, r)
}
