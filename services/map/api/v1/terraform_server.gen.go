// Code generated with openapi-go DO NOT EDIT.
package v1

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	oapi_rt "github.com/mworzala/openapi-go/pkg/oapi-rt"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

type TerraformServer interface {
	GetPlayerSession(ctx context.Context, playerId string) ([]byte, error)
	UpdatePlayerSession(ctx context.Context, playerId string, req []byte) error
	GetLocalSession(ctx context.Context, playerId string, worldId string) ([]byte, error)
	UpdateLocalSession(ctx context.Context, playerId string, worldId string, req []byte) error
	ListPlayerSchematics(ctx context.Context, playerId string) ([]*SchematicHeader, error)
	GetSchematicData(ctx context.Context, playerId string, schemName string) ([]byte, error)
	UpdateSchematicHeader(ctx context.Context, playerId string, schemName string, req *UpdateSchematicHeaderRequest) error
	CreateSchematic(ctx context.Context, playerId string, schemName string, dimx int, dimy int, dimz int, fileType string, req []byte) error
	DeleteSchematic(ctx context.Context, playerId string, schemName string) error
}

type TerraformServerWrapper struct {
	log         *zap.SugaredLogger
	middlewares []oapi_rt.Middleware
	handler     TerraformServer
}

type TerraformServerWrapperParams struct {
	fx.In
	Log     *zap.SugaredLogger
	Handler TerraformServer

	Middleware []oapi_rt.Middleware `group:"terraform_middleware"`
}

func NewTerraformServerWrapper(p TerraformServerWrapperParams) (*TerraformServerWrapper, error) {
	sw := &TerraformServerWrapper{
		log:         p.Log.With("handler", "terraform (wrapper)"),
		handler:     p.Handler,
		middlewares: p.Middleware,
	}

	return sw, nil
}

func (sw *TerraformServerWrapper) Apply(r chi.Router) {
	r.Route("/v1/internal/terraform", func(r chi.Router) {
		r.Get("/session/{playerId}", sw.GetPlayerSession)
		r.Put("/session/{playerId}", sw.UpdatePlayerSession)
		r.Get("/session/{playerId}/{worldId}", sw.GetLocalSession)
		r.Put("/session/{playerId}/{worldId}", sw.UpdateLocalSession)
		r.Get("/schem/{playerId}", sw.ListPlayerSchematics)
		r.Get("/schem/{playerId}/{schemName}", sw.GetSchematicData)
		r.Put("/schem/{playerId}/{schemName}", sw.UpdateSchematicHeader)
		r.Post("/schem/{playerId}/{schemName}", sw.CreateSchematic)
		r.Delete("/schem/{playerId}/{schemName}", sw.DeleteSchematic)
	})
}

func (sw *TerraformServerWrapper) GetPlayerSession(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	playerId := chi.URLParam(r, "playerId")

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		code200, err := sw.handler.GetPlayerSession(ctx, playerId)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}

		if code200 != nil {
			w.Header().Set("content-type", "application/vnd.terraform.player_session")
			w.WriteHeader(200)
			_, _ = w.Write(code200)

			return
		}

		w.WriteHeader(404)
	})
	for _, middleware := range sw.middlewares {
		handler = middleware.Run(handler)
	}
	handler.ServeHTTP(w, r)
}

func (sw *TerraformServerWrapper) UpdatePlayerSession(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	playerId := chi.URLParam(r, "playerId")

	// Read Body
	var body []byte

	if body, err = io.ReadAll(r.Body); err != nil {
		oapi_rt.WriteGenericError(w, err)
		return
	}

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		err := sw.handler.UpdatePlayerSession(ctx, playerId, body)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}

		w.WriteHeader(200)
	})
	for _, middleware := range sw.middlewares {
		handler = middleware.Run(handler)
	}
	handler.ServeHTTP(w, r)
}

func (sw *TerraformServerWrapper) GetLocalSession(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	playerId := chi.URLParam(r, "playerId")
	worldId := chi.URLParam(r, "worldId")

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		code200, err := sw.handler.GetLocalSession(ctx, playerId, worldId)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}

		if code200 != nil {
			w.Header().Set("content-type", "application/vnd.terraform.local_session")
			w.WriteHeader(200)
			_, _ = w.Write(code200)

			return
		}

		w.WriteHeader(404)
	})
	for _, middleware := range sw.middlewares {
		handler = middleware.Run(handler)
	}
	handler.ServeHTTP(w, r)
}

func (sw *TerraformServerWrapper) UpdateLocalSession(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	playerId := chi.URLParam(r, "playerId")
	worldId := chi.URLParam(r, "worldId")

	// Read Body
	var body []byte

	if body, err = io.ReadAll(r.Body); err != nil {
		oapi_rt.WriteGenericError(w, err)
		return
	}

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		err := sw.handler.UpdateLocalSession(ctx, playerId, worldId, body)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}

		w.WriteHeader(200)
	})
	for _, middleware := range sw.middlewares {
		handler = middleware.Run(handler)
	}
	handler.ServeHTTP(w, r)
}

func (sw *TerraformServerWrapper) ListPlayerSchematics(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	playerId := chi.URLParam(r, "playerId")

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		code200, err := sw.handler.ListPlayerSchematics(ctx, playerId)
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

func (sw *TerraformServerWrapper) GetSchematicData(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	playerId := chi.URLParam(r, "playerId")
	schemName := chi.URLParam(r, "schemName")

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		code200, err := sw.handler.GetSchematicData(ctx, playerId, schemName)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}

		if code200 != nil {
			w.Header().Set("content-type", "application/vnd.terraform.schematic")
			w.WriteHeader(200)
			_, _ = w.Write(code200)

			return
		}

		w.WriteHeader(404)
	})
	for _, middleware := range sw.middlewares {
		handler = middleware.Run(handler)
	}
	handler.ServeHTTP(w, r)
}

func (sw *TerraformServerWrapper) UpdateSchematicHeader(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	playerId := chi.URLParam(r, "playerId")
	schemName := chi.URLParam(r, "schemName")

	// Read Body
	var body UpdateSchematicHeaderRequest

	if err = json.NewDecoder(r.Body).Decode(&body); err != nil {
		oapi_rt.WriteGenericError(w, err)
		return
	}

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		err := sw.handler.UpdateSchematicHeader(ctx, playerId, schemName, &body)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}

		w.WriteHeader(200)
	})
	for _, middleware := range sw.middlewares {
		handler = middleware.Run(handler)
	}
	handler.ServeHTTP(w, r)
}

func (sw *TerraformServerWrapper) CreateSchematic(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	dimxStr := r.URL.Query().Get("dimx")
	var dimx int
	if dimxStr != "" {
		dimx, err = strconv.Atoi(dimxStr)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}
	}

	dimyStr := r.URL.Query().Get("dimy")
	var dimy int
	if dimyStr != "" {
		dimy, err = strconv.Atoi(dimyStr)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}
	}

	dimzStr := r.URL.Query().Get("dimz")
	var dimz int
	if dimzStr != "" {
		dimz, err = strconv.Atoi(dimzStr)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}
	}

	fileType := r.URL.Query().Get("fileType")

	playerId := chi.URLParam(r, "playerId")
	schemName := chi.URLParam(r, "schemName")

	// Read Body
	var body []byte

	if body, err = io.ReadAll(r.Body); err != nil {
		oapi_rt.WriteGenericError(w, err)
		return
	}

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		err := sw.handler.CreateSchematic(ctx, playerId, schemName, dimx, dimy, dimz, fileType, body)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}

		w.WriteHeader(200)
	})
	for _, middleware := range sw.middlewares {
		handler = middleware.Run(handler)
	}
	handler.ServeHTTP(w, r)
}

func (sw *TerraformServerWrapper) DeleteSchematic(w http.ResponseWriter, r *http.Request) {
	var err error
	_ = err // Sometimes we don't use it but need that not to be an error

	// Read Parameters

	playerId := chi.URLParam(r, "playerId")
	schemName := chi.URLParam(r, "schemName")

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := oapi_rt.NewContext(r.Context(), r)

		err := sw.handler.DeleteSchematic(ctx, playerId, schemName)
		if err != nil {
			oapi_rt.WriteGenericError(w, err)
			return
		}

		w.WriteHeader(200)
	})
	for _, middleware := range sw.middlewares {
		handler = middleware.Run(handler)
	}
	handler.ServeHTTP(w, r)
}
