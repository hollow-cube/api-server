package gen

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateOpenAPI(t *testing.T) {
	api := &API{
		PackageName: "v4",
		StructName:  "Server",
		ModulePath:  "github.com/example/app",
		Endpoints: []Endpoint{
			{
				Name:        "GetUser",
				Method:      "GET",
				Path:        "/users/{id}",
				Description: "Get a user by ID",
				RequestType: "GetUserRequest",
				Params: []Param{
					{Name: "id", GoName: "ID", GoType: "string", Location: "path", Required: true, OAPIType: "string"},
					{Name: "expand", GoName: "Expand", GoType: "string", Location: "query", Required: false, OAPIType: "string"},
				},
				Response: Response{StatusCode: 200, GoType: "User", OAPIType: "object"},
			},
			{
				Name:        "CreateUser",
				Method:      "POST",
				Path:        "/users",
				Description: "Create a new user",
				Response:    Response{StatusCode: 201, GoType: "User", OAPIType: "object"},
			},
			{
				Name:     "DeleteUser",
				Method:   "DELETE",
				Path:     "/users/{id}",
				Params:   []Param{{Name: "id", GoName: "ID", GoType: "string", Location: "path", Required: true, OAPIType: "string"}},
				Response: Response{StatusCode: 204},
			},
		},
	}

	yamlBytes, err := GenerateOpenAPI(api)
	require.NoError(t, err)

	yaml := string(yamlBytes)

	// Check basic structure
	require.Contains(t, yaml, "openapi: 3.0.3")
	require.Contains(t, yaml, "title: Server API")

	// Check operation IDs
	require.Contains(t, yaml, "operationId: getUser")
	require.Contains(t, yaml, "operationId: createUser")
	require.Contains(t, yaml, "operationId: deleteUser")

	// Check params are present
	require.Contains(t, yaml, "name: id")
	require.Contains(t, yaml, "in: path")
	require.Contains(t, yaml, "name: expand")
	require.Contains(t, yaml, "in: query")

	// Check response codes
	require.Contains(t, yaml, `"200":`)
	require.Contains(t, yaml, `"201":`)
	require.Contains(t, yaml, `"204":`)

	// 204 should not have content
	// The 204 response should have description but no content key at all would be ideal,
	// but our implementation omits content when StatusCode==204
	require.Contains(t, yaml, "description: Successful response")
}

func TestGenerateOpenAPI_NoContent204(t *testing.T) {
	api := &API{
		PackageName: "v4",
		StructName:  "Server",
		ModulePath:  "github.com/example/app",
		Endpoints: []Endpoint{
			{
				Name:     "DeleteItem",
				Method:   "DELETE",
				Path:     "/items/{id}",
				Response: Response{StatusCode: 204},
			},
		},
	}

	yamlBytes, err := GenerateOpenAPI(api)
	require.NoError(t, err)

	yaml := string(yamlBytes)
	require.Contains(t, yaml, `"204":`)
	// Should have description but the 204 response should not have application/json content
	require.NotContains(t, yaml, "application/json")
}

func TestLcFirst(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"GetUser", "getUser"},
		{"A", "a"},
		{"already", "already"},
		{"", ""},
		{"ABC", "aBC"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.want, lcFirst(tt.input))
		})
	}
}
