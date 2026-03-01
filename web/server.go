package web

import (
	"context"
	"embed"
	"io"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/hollow-cube/api-server/web/components"
)

//go:embed static
var static embed.FS

func Run(r chi.Router) {
	r.Handle("/static/*", http.FileServer(http.FS(static)))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		contents := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
			return components.HomePage().Render(ctx, w)
		})
		ctx := templ.WithChildren(r.Context(), contents)

		components.Layout("Test Page").Render(ctx, w)
	})

}
