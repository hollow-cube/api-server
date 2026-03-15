package gen

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateServer(t *testing.T) {
	api := &API{
		PackageName: "v4",
		StructName:  "Server",
		ModulePath:  "github.com/example/app",
		Endpoints: []Endpoint{
			{
				Name:        "GetUser",
				Method:      "GET",
				Path:        "/users/{id}",
				RequestType: "GetUserRequest",
				Params: []Param{
					{Name: "id", GoName: "ID", GoType: "string", Location: "path", Required: true},
					{Name: "age", GoName: "Age", GoType: "int", Location: "query", Required: false},
				},
				Response: Response{StatusCode: 200, GoType: "User"},
			},
			{
				Name:     "CreateUser",
				Method:   "POST",
				Path:     "/users",
				Response: Response{StatusCode: 201, GoType: "User"},
			},
			{
				Name:        "DeleteUser",
				Method:      "DELETE",
				Path:        "/users/{id}",
				RequestType: "DeleteUserRequest",
				Params: []Param{
					{Name: "id", GoName: "ID", GoType: "string", Location: "path", Required: true},
				},
				Response: Response{StatusCode: 204, GoType: "User"},
			},
		},
	}

	code, err := GenerateServer(api)
	require.NoError(t, err)

	src := string(code)

	// Verify it parses as valid Go
	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "server.gen.go", src, 0)
	require.NoError(t, err, "generated code should be valid Go")

	// Check RegisterParams type
	require.Contains(t, src, "type RegisterParams struct")
	require.Contains(t, src, "Mux     *http.ServeMux")
	require.Contains(t, src, "BaseURL string")

	// Check RegisterRoutes signature
	require.Contains(t, src, "func RegisterRoutes(s *Server, params RegisterParams)")

	// Check route registrations - BaseURL should be after the method
	require.Contains(t, src, `params.Mux.HandleFunc("GET "+params.BaseURL+"/users/{id}"`)
	require.Contains(t, src, `params.Mux.HandleFunc("POST "+params.BaseURL+"/users"`)
	require.Contains(t, src, `params.Mux.HandleFunc("DELETE "+params.BaseURL+"/users/{id}"`)

	// Check handlers struct
	require.Contains(t, src, "type handlers struct")
	require.Contains(t, src, "server *Server")

	// Check handler methods (lowercase first letter)
	require.Contains(t, src, "func (h *handlers) getUser(w http.ResponseWriter, r *http.Request)")
	require.Contains(t, src, "func (h *handlers) createUser(w http.ResponseWriter, r *http.Request)")
	require.Contains(t, src, "func (h *handlers) deleteUser(w http.ResponseWriter, r *http.Request)")

	// Check param bindings
	require.Contains(t, src, `r.PathValue("id")`)
	require.Contains(t, src, `r.URL.Query().Get("age")`)

	// Check runtime calls
	require.Contains(t, src, "runtime.HandleError")
	require.Contains(t, src, "runtime.WriteJSON")
	require.Contains(t, src, "runtime.WriteBadRequest")

	// Check runtime import path
	require.Contains(t, src, `"github.com/example/app/pkg/ox/runtime"`)

	// Check strconv is imported (has int param)
	require.Contains(t, src, `"strconv"`)

	// 204 should use WriteHeader, not WriteJSON
	require.Contains(t, src, "w.WriteHeader(204)")
}

func TestGenerateServer_NoParams(t *testing.T) {
	api := &API{
		PackageName: "v4",
		StructName:  "Server",
		ModulePath:  "github.com/example/app",
		Endpoints: []Endpoint{
			{
				Name:     "ListUsers",
				Method:   "GET",
				Path:     "/users",
				Response: Response{StatusCode: 200, GoType: "[]User"},
			},
		},
	}

	code, err := GenerateServer(api)
	require.NoError(t, err)

	src := string(code)

	// Verify it parses as valid Go
	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "server.gen.go", src, 0)
	require.NoError(t, err, "generated code should be valid Go")

	// strconv should NOT be imported when all params are strings (or no params)
	require.NotContains(t, src, "strconv")

	// Should not have var req declaration
	require.NotContains(t, src, "var req")
}

func TestNeedsStrconv(t *testing.T) {
	tests := []struct {
		name string
		api  *API
		want bool
	}{
		{
			name: "no endpoints",
			api:  &API{},
			want: false,
		},
		{
			name: "all string params",
			api: &API{
				Endpoints: []Endpoint{
					{Params: []Param{{GoType: "string"}, {GoType: "string"}}},
				},
			},
			want: false,
		},
		{
			name: "has int param",
			api: &API{
				Endpoints: []Endpoint{
					{Params: []Param{{GoType: "string"}, {GoType: "int"}}},
				},
			},
			want: true,
		},
		{
			name: "has int64 param",
			api: &API{
				Endpoints: []Endpoint{
					{Params: []Param{{GoType: "int64"}}},
				},
			},
			want: true,
		},
		{
			name: "has bool param",
			api: &API{
				Endpoints: []Endpoint{
					{Params: []Param{{GoType: "bool"}}},
				},
			},
			want: true,
		},
		{
			name: "no params",
			api: &API{
				Endpoints: []Endpoint{
					{Params: nil},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, needsStrconv(tt.api))
		})
	}
}

func TestGenerateServer_HeaderParam(t *testing.T) {
	api := &API{
		PackageName: "v4",
		StructName:  "Server",
		ModulePath:  "github.com/example/app",
		Endpoints: []Endpoint{
			{
				Name:        "GetUser",
				Method:      "GET",
				Path:        "/users",
				RequestType: "GetUserRequest",
				Params: []Param{
					{Name: "Authorization", GoName: "Token", GoType: "string", Location: "header", Required: true},
				},
				Response: Response{StatusCode: 200, GoType: "User"},
			},
		},
	}

	code, err := GenerateServer(api)
	require.NoError(t, err)

	src := string(code)
	require.Contains(t, src, `r.Header.Get("Authorization")`)

	// No strconv needed for string-only
	lines := strings.Split(src, "\n")
	hasStrconv := false
	for _, l := range lines {
		if strings.Contains(l, `"strconv"`) {
			hasStrconv = true
		}
	}
	require.False(t, hasStrconv)
}

func TestGenerateServer_EmbeddedRequestBody(t *testing.T) {
	api := &API{
		PackageName: "v4",
		StructName:  "Server",
		ModulePath:  "github.com/example/app",
		Endpoints: []Endpoint{
			{
				Name:        "UpdatePlayer",
				Method:      "PATCH",
				Path:        "/players/{id}",
				RequestType: "UpdatePlayerRequest",
				Params: []Param{
					{Name: "id", GoName: "ID", GoType: "string", Location: "path", Required: true},
				},
				RequestBody: &RequestBody{
					GoName:   "UpdatePlayerRequestBody",
					GoType:   "UpdatePlayerRequestBody",
					Required: true,
				},
				Response: Response{StatusCode: 200},
			},
		},
	}

	code, err := GenerateServer(api)
	require.NoError(t, err)

	src := string(code)

	// Verify it parses as valid Go
	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "server.gen.go", src, 0)
	require.NoError(t, err, "generated code should be valid Go")

	// Should declare the request struct
	require.Contains(t, src, "var req UpdatePlayerRequest")

	// Should bind path parameter
	require.Contains(t, src, `req.ID = r.PathValue("id")`)

	// Should decode the embedded body
	require.Contains(t, src, "runtime.DecodeJSON(r, &req.UpdatePlayerRequestBody)")

	// Should call the handler with just the request struct (not separate body param)
	require.Contains(t, src, "h.server.UpdatePlayer(r.Context(), req)")
}

func TestGenerateServer_BodyField(t *testing.T) {
	api := &API{
		PackageName: "v4",
		StructName:  "Server",
		ModulePath:  "github.com/example/app",
		Endpoints: []Endpoint{
			{
				Name:        "UpdateItem",
				Method:      "PATCH",
				Path:        "/items/{id}",
				RequestType: "UpdateItemRequest",
				Params: []Param{
					{Name: "id", GoName: "ID", GoType: "string", Location: "path", Required: true},
				},
				RequestBody: &RequestBody{
					GoName:   "Body",
					GoType:   "UpdateItemBody",
					Required: true,
				},
				Response: Response{StatusCode: 200},
			},
		},
	}

	code, err := GenerateServer(api)
	require.NoError(t, err)

	src := string(code)

	// Verify it parses as valid Go
	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "server.gen.go", src, 0)
	require.NoError(t, err, "generated code should be valid Go")

	// Should decode the Body field
	require.Contains(t, src, "runtime.DecodeJSON(r, &req.Body)")

	// Should call the handler with just the request struct
	require.Contains(t, src, "h.server.UpdateItem(r.Context(), req)")
}

func TestGenerateServer_SeparateBodyParam(t *testing.T) {
	api := &API{
		PackageName: "v4",
		StructName:  "Server",
		ModulePath:  "github.com/example/app",
		Endpoints: []Endpoint{
			{
				Name:   "CreateUser",
				Method: "POST",
				Path:   "/users",
				RequestBody: &RequestBody{
					GoName:   "body",
					GoType:   "CreateUserRequest",
					Required: true,
				},
				Response: Response{StatusCode: 201, GoType: "User"},
			},
		},
	}

	code, err := GenerateServer(api)
	require.NoError(t, err)

	src := string(code)

	// Verify it parses as valid Go
	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "server.gen.go", src, 0)
	require.NoError(t, err, "generated code should be valid Go")

	// Should declare a separate body variable
	require.Contains(t, src, "var body CreateUserRequest")

	// Should decode into the body variable
	require.Contains(t, src, "runtime.DecodeJSON(r, &body)")

	// Should call the handler with the body parameter
	require.Contains(t, src, "h.server.CreateUser(r.Context(), body)")
}

func TestGenerateServer_BaseURLPlacement(t *testing.T) {
	api := &API{
		PackageName: "api",
		StructName:  "Server",
		ModulePath:  "github.com/example/app",
		Endpoints: []Endpoint{
			{
				Name:        "GetItem",
				Method:      "GET",
				Path:        "/items/{id}",
				RequestType: "GetItemRequest",
				Params: []Param{
					{Name: "id", GoName: "ID", GoType: "string", Location: "path", Required: true},
				},
				Response: Response{StatusCode: 200, GoType: "Item"},
			},
			{
				Name:   "CreateItem",
				Method: "POST",
				Path:   "/items",
				RequestBody: &RequestBody{
					GoName:   "body",
					GoType:   "CreateItemRequest",
					Required: true,
				},
				Response: Response{StatusCode: 201, GoType: "Item"},
			},
		},
	}

	code, err := GenerateServer(api)
	require.NoError(t, err)

	src := string(code)

	// Verify it parses as valid Go
	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "server.gen.go", src, 0)
	require.NoError(t, err, "generated code should be valid Go")

	// BaseURL must come AFTER the HTTP method, not before
	// Correct: "GET "+params.BaseURL+"/items/{id}"
	// Incorrect: params.BaseURL+"GET /items/{id}"
	require.Contains(t, src, `"GET "+params.BaseURL+"/items/{id}"`, "BaseURL should come after HTTP method for GET")
	require.Contains(t, src, `"POST "+params.BaseURL+"/items"`, "BaseURL should come after HTTP method for POST")

	// Make sure the wrong format is NOT present
	require.NotContains(t, src, `params.BaseURL+"GET `, "BaseURL should not come before HTTP method")
	require.NotContains(t, src, `params.BaseURL+"POST `, "BaseURL should not come before HTTP method")
}

func TestGenerateServer_BooleanQueryParams(t *testing.T) {
	api := &API{
		PackageName: "v4",
		StructName:  "Server",
		ModulePath:  "github.com/example/app",
		Endpoints: []Endpoint{
			{
				Name:        "SearchItems",
				Method:      "GET",
				Path:        "/items",
				RequestType: "SearchItemsRequest",
				Params: []Param{
					{Name: "active", GoName: "Active", GoType: "bool", Location: "query", Required: true},
					{Name: "verified", GoName: "Verified", GoType: "bool", Location: "query", Required: false},
				},
				Response: Response{StatusCode: 200, GoType: "[]Item"},
			},
		},
	}

	code, err := GenerateServer(api)
	require.NoError(t, err)

	src := string(code)

	// Verify it parses as valid Go
	fset := token.NewFileSet()
	_, err = parser.ParseFile(fset, "server.gen.go", src, 0)
	require.NoError(t, err, "generated code should be valid Go")

	// Check required boolean parameter handling
	require.Contains(t, src, `strconv.ParseBool(r.URL.Query().Get("active"))`)
	require.Contains(t, src, `req.Active = v`)
	require.Contains(t, src, `runtime.WriteBadRequest(w, "invalid query parameter: active")`)

	// Check optional boolean parameter handling
	require.Contains(t, src, `if qs := r.URL.Query().Get("verified"); qs != ""`)
	require.Contains(t, src, `strconv.ParseBool(qs)`)
	require.Contains(t, src, `req.Verified = v`)
	require.Contains(t, src, `runtime.WriteBadRequest(w, "invalid query parameter: verified")`)

	// strconv should be imported for bool params
	require.Contains(t, src, `"strconv"`)
}
