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
	Name     string    `yaml:"name"`
	In       string    `yaml:"in"`
	Required bool      `yaml:"required"`
	Schema   *oaSchema `yaml:"schema"`
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
		pathItem, ok := spec.Paths[ep.Path]
		if !ok {
			pathItem = make(oaPathItem)
			spec.Paths[ep.Path] = pathItem
		}

		op := &oaOperation{
			OperationID: lcFirst(ep.Name),
			Summary:     ep.Description,
			Responses:   make(map[string]*oaResponse),
		}

		for _, p := range ep.Params {
			op.Parameters = append(op.Parameters, oaParameter{
				Name:     p.Name,
				In:       p.Location,
				Required: p.Required,
				Schema:   &oaSchema{Type: p.OAPIType, Format: p.OAPIFmt},
			})
		}

		// Add request body if present
		if ep.RequestBody != nil {
			op.RequestBody = &oaRequestBody{
				Required: ep.RequestBody.Required,
				Content: map[string]*oaMediaType{
					"application/json": {
						Schema: &oaSchema{
							Type: "object",
						},
					},
				},
			}
		}

		statusStr := fmt.Sprintf("%d", ep.Response.StatusCode)
		resp := &oaResponse{
			Description: "Successful response",
		}
		if ep.Response.OAPIType != "" && ep.Response.StatusCode != 204 {
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
