package gen

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type oaSpec struct {
	OpenAPI string                `yaml:"openapi"`
	Info    oaInfo                `yaml:"info"`
	Paths   map[string]oaPathItem `yaml:"paths"`
}

type oaInfo struct {
	Title   string `yaml:"title"`
	Version string `yaml:"version"`
}

type oaPathItem map[string]*oaOperation

type oaOperation struct {
	OperationID string                 `yaml:"operationId"`
	Summary     string                 `yaml:"summary,omitempty"`
	Parameters  []oaParameter          `yaml:"parameters,omitempty"`
	RequestBody *oaRequestBody         `yaml:"requestBody,omitempty"`
	Responses   map[string]*oaResponse `yaml:"responses"`
}

type oaRequestBody struct {
	Required bool                    `yaml:"required"`
	Content  map[string]*oaMediaType `yaml:"content"`
}

type oaParameter struct {
	Name        string    `yaml:"name"`
	In          string    `yaml:"in"`
	Required    bool      `yaml:"required"`
	Description string    `yaml:"description,omitempty"`
	Schema      *oaSchema `yaml:"schema"`
}

type oaResponse struct {
	Description string                  `yaml:"description"`
	Content     map[string]*oaMediaType `yaml:"content,omitempty"`
}

type oaMediaType struct {
	Schema *oaSchema `yaml:"schema"`
}

type oaSchema struct {
	Type   string `yaml:"type"`
	Format string `yaml:"format,omitempty"`
}

// GenerateOpenAPI produces an OpenAPI 3.0.3 YAML spec from the analyzed API.
func GenerateOpenAPI(api *API) ([]byte, error) {
	spec := oaSpec{
		OpenAPI: "3.0.3",
		Info: oaInfo{
			Title:   api.StructName + " API",
			Version: "1.0",
		},
		Paths: make(map[string]oaPathItem),
	}

	for _, ep := range api.Endpoints {
		// OpenAPI has no wildcard path syntax — strip the leading * from
		// {*name} segments. The wildcard nature is conveyed via the param
		// description on the corresponding oaParameter entry.
		oaPath := strings.ReplaceAll(ep.Path, "{*", "{")
		pathItem, ok := spec.Paths[oaPath]
		if !ok {
			pathItem = make(oaPathItem)
			spec.Paths[oaPath] = pathItem
		}

		op := &oaOperation{
			OperationID: lcFirst(ep.Name),
			Summary:     ep.Description,
			Responses:   make(map[string]*oaResponse),
		}

		for _, p := range ep.Params {
			oap := oaParameter{
				Name:     p.Name,
				In:       p.Location,
				Required: p.Required,
				Schema:   &oaSchema{Type: p.OAPIType, Format: p.OAPIFmt},
			}
			if p.IsWildcard {
				oap.Description = "Wildcard path segment: captures one or more path segments (no leading slash)."
			}
			op.Parameters = append(op.Parameters, oap)
		}

		// Add request body if present
		if ep.RequestBody != nil {
			rb := &oaRequestBody{Required: ep.RequestBody.Required}
			switch {
			case ep.RequestBody.IsStream:
				rb.Content = make(map[string]*oaMediaType, len(ep.RequestBody.Consumes))
				for _, mt := range ep.RequestBody.Consumes {
					rb.Content[mt] = &oaMediaType{
						Schema: &oaSchema{Type: "string", Format: "binary"},
					}
				}
			case ep.RequestBody.IsRawBytes:
				mts := ep.RequestBody.Consumes
				if len(mts) == 0 {
					mts = []string{"application/octet-stream"}
				}
				rb.Content = make(map[string]*oaMediaType, len(mts))
				for _, mt := range mts {
					rb.Content[mt] = &oaMediaType{
						Schema: &oaSchema{Type: "string", Format: "binary"},
					}
				}
			default:
				rb.Content = map[string]*oaMediaType{
					"application/json": {
						Schema: &oaSchema{Type: "object"},
					},
				}
			}
			op.RequestBody = rb
		}

		statusStr := fmt.Sprintf("%d", ep.Response.StatusCode)
		resp := &oaResponse{
			Description: "Successful response",
		}
		switch {
		case ep.Response.IsSSE:
			resp.Description = "Server-Sent Events stream"
			resp.Content = map[string]*oaMediaType{
				"text/event-stream": {
					Schema: &oaSchema{Type: "string"},
				},
			}
		case ep.Response.IsStream:
			resp.Content = make(map[string]*oaMediaType, len(ep.Response.Produces))
			for _, mt := range ep.Response.Produces {
				resp.Content[mt] = &oaMediaType{
					Schema: &oaSchema{Type: "string", Format: "binary"},
				}
			}
		case ep.Response.OAPIType != "" && ep.Response.StatusCode != 204:
			resp.Content = map[string]*oaMediaType{
				ep.Response.ContentType: {
					Schema: &oaSchema{
						Type:   ep.Response.OAPIType,
						Format: ep.Response.OAPIFmt,
					},
				},
			}
		}
		op.Responses[statusStr] = resp

		pathItem[strings.ToLower(ep.Method)] = op
	}

	return yaml.Marshal(spec)
}

func lcFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
