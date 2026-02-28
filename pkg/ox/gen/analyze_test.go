package gen

import (
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseRouteComment(t *testing.T) {
	tests := []struct {
		name       string
		doc        string
		wantMethod string
		wantPath   string
		wantDesc   string
		wantErr    bool
	}{
		{
			name:       "GET simple path",
			doc:        "GET /users",
			wantMethod: "GET",
			wantPath:   "/users",
		},
		{
			name:       "POST with path param",
			doc:        "POST /users/{id}",
			wantMethod: "POST",
			wantPath:   "/users/{id}",
		},
		{
			name:       "with description",
			doc:        "GET /users\nReturns all users in the system.",
			wantMethod: "GET",
			wantPath:   "/users",
			wantDesc:   "Returns all users in the system.",
		},
		{
			name:       "ox:route directive",
			doc:        "Some description.\nox:route GET /items/{id}",
			wantMethod: "GET",
			wantPath:   "/items/{id}",
			wantDesc:   "Some description.",
		},
		{
			name:       "ox:route directive with other directives filtered",
			doc:        "Some description.\nox:route PUT /items/{id}\nox:tags items",
			wantMethod: "PUT",
			wantPath:   "/items/{id}",
			wantDesc:   "Some description.",
		},
		{
			name:    "empty doc",
			doc:     "",
			wantErr: true,
		},
		{
			name:    "no route declaration",
			doc:     "This is just a comment.\nNothing useful here.",
			wantErr: true,
		},
		{
			name:       "DELETE method",
			doc:        "DELETE /users/{id}",
			wantMethod: "DELETE",
			wantPath:   "/users/{id}",
		},
		{
			name:       "PATCH method",
			doc:        "PATCH /users/{id}",
			wantMethod: "PATCH",
			wantPath:   "/users/{id}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method, path, desc, err := parseRouteComment(tt.doc)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantMethod, method)
			require.Equal(t, tt.wantPath, path)
			require.Equal(t, tt.wantDesc, desc)
		})
	}
}

func TestIsHTTPMethod(t *testing.T) {
	valid := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
	for _, m := range valid {
		require.True(t, isHTTPMethod(m), "expected %q to be valid", m)
	}

	invalid := []string{"get", "post", "TRACE", "", "CONNECT", "gEt"}
	for _, m := range invalid {
		require.False(t, isHTTPMethod(m), "expected %q to be invalid", m)
	}
}

func TestInferStatusCode(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{"GetUser", 200},
		{"GetAll", 200},
		{"CreateUser", 201},
		{"CreateItem", 201},
		{"DeleteUser", 204},
		{"DeleteAll", 204},
		{"UpdateUser", 200},
		{"ListItems", 200},
		{"DoSomething", 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, inferStatusCode(tt.name))
		})
	}
}

func TestTagName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"id", "id"},
		{"name,optional", "name"},
		{"field,omitempty,optional", "field"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.want, tagName(tt.input))
		})
	}
}

func TestParseTagOpts(t *testing.T) {
	tests := []struct {
		input        string
		wantName     string
		wantOptional bool
	}{
		{"id", "id", false},
		{"name,optional", "name", true},
		{"field,omitempty", "field", false},
		{"field,omitempty,optional", "field", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, optional := parseTagOpts(tt.input)
			require.Equal(t, tt.wantName, name)
			require.Equal(t, tt.wantOptional, optional)
		})
	}
}

func TestGoTypeToOpenAPI(t *testing.T) {
	tests := []struct {
		name       string
		typ        types.Type
		wantType   string
		wantFormat string
	}{
		{"string", types.Typ[types.String], "string", ""},
		{"bool", types.Typ[types.Bool], "boolean", ""},
		{"int", types.Typ[types.Int], "integer", ""},
		{"int32", types.Typ[types.Int32], "integer", ""},
		{"int64", types.Typ[types.Int64], "integer", "int64"},
		{"uint", types.Typ[types.Uint], "integer", ""},
		{"uint64", types.Typ[types.Uint64], "integer", "int64"},
		{"float32", types.Typ[types.Float32], "number", "float"},
		{"float64", types.Typ[types.Float64], "number", "double"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typ, format := goTypeToOpenAPI(tt.typ)
			require.Equal(t, tt.wantType, typ)
			require.Equal(t, tt.wantFormat, format)
		})
	}
}

func TestExtractParamsAndBody(t *testing.T) {
	// Build a struct type: struct { ID string `path:"id"`; Page int `query:"page,optional"`; Auth string `header:"Authorization"`; Internal string }
	fields := []*types.Var{
		types.NewField(0, nil, "ID", types.Typ[types.String], false),
		types.NewField(0, nil, "Page", types.Typ[types.Int], false),
		types.NewField(0, nil, "Auth", types.Typ[types.String], false),
		types.NewField(0, nil, "Internal", types.Typ[types.String], false),
	}
	tags := []string{
		`path:"id"`,
		`query:"page,optional"`,
		`header:"Authorization"`,
		``,
	}
	st := types.NewStruct(fields, tags)

	params, body := extractParamsAndBody(st)

	require.Len(t, params, 3) // Internal has no tag, should be skipped
	require.Nil(t, body)      // No Body field

	// path param
	require.Equal(t, "id", params[0].Name)
	require.Equal(t, "ID", params[0].GoName)
	require.Equal(t, "path", params[0].Location)
	require.True(t, params[0].Required)
	require.Equal(t, "string", params[0].OAPIType)

	// query param
	require.Equal(t, "page", params[1].Name)
	require.Equal(t, "Page", params[1].GoName)
	require.Equal(t, "query", params[1].Location)
	require.False(t, params[1].Required) // optional
	require.Equal(t, "integer", params[1].OAPIType)

	// header param
	require.Equal(t, "Authorization", params[2].Name)
	require.Equal(t, "Auth", params[2].GoName)
	require.Equal(t, "header", params[2].Location)
	require.True(t, params[2].Required)
	require.Equal(t, "string", params[2].OAPIType)
}

func TestExtractParamsAndBody_BodyField(t *testing.T) {
	// Build a struct type: struct { ID string `path:"id"`; Body SomeType }
	bodyType := types.NewNamed(
		types.NewTypeName(0, nil, "SomeType", nil),
		types.NewStruct(nil, nil),
		nil,
	)

	fields := []*types.Var{
		types.NewField(0, nil, "ID", types.Typ[types.String], false),
		types.NewField(0, nil, "Body", bodyType, false),
	}
	tags := []string{
		`path:"id"`,
		``,
	}
	st := types.NewStruct(fields, tags)

	params, body := extractParamsAndBody(st)

	require.Len(t, params, 1)
	require.NotNil(t, body)
	require.Equal(t, "Body", body.GoName)
	require.Equal(t, "SomeType", body.GoType)
	require.True(t, body.Required)
}

func TestExtractParamsAndBody_EmbeddedBody(t *testing.T) {
	// Build a struct type: struct { ID string `path:"id"`; UpdateRequest }
	embeddedType := types.NewNamed(
		types.NewTypeName(0, nil, "UpdateRequest", nil),
		types.NewStruct(nil, nil),
		nil,
	)

	fields := []*types.Var{
		types.NewField(0, nil, "ID", types.Typ[types.String], false),
		types.NewField(0, nil, "UpdateRequest", embeddedType, true), // embedded=true
	}
	tags := []string{
		`path:"id"`,
		``, // no tags on embedded field
	}
	st := types.NewStruct(fields, tags)

	params, body := extractParamsAndBody(st)

	require.Len(t, params, 1)
	require.NotNil(t, body)
	require.Equal(t, "UpdateRequest", body.GoName)
	require.Equal(t, "UpdateRequest", body.GoType)
	require.True(t, body.Required)
}

func TestExtractParamsAndBody_EmbeddedBodyWithJSONTags(t *testing.T) {
	// Build a struct type: struct { ID string `path:"id"`; UpdateRequest `json:",inline"` }
	embeddedType := types.NewNamed(
		types.NewTypeName(0, nil, "UpdateRequest", nil),
		types.NewStruct(nil, nil),
		nil,
	)

	fields := []*types.Var{
		types.NewField(0, nil, "ID", types.Typ[types.String], false),
		types.NewField(0, nil, "UpdateRequest", embeddedType, true), // embedded=true
	}
	tags := []string{
		`path:"id"`,
		`json:",inline"`, // json tags are allowed on embedded body
	}
	st := types.NewStruct(fields, tags)

	params, body := extractParamsAndBody(st)

	require.Len(t, params, 1)
	require.NotNil(t, body)
	require.Equal(t, "UpdateRequest", body.GoName)
	require.Equal(t, "UpdateRequest", body.GoType)
	require.True(t, body.Required)
}

func TestExtractParamsAndBody_NoEmbeddedBodyWithParamTag(t *testing.T) {
	// Build a struct type where embedded field has param tag - should NOT be treated as body
	embeddedType := types.NewNamed(
		types.NewTypeName(0, nil, "ParamsStruct", nil),
		types.NewStruct(nil, nil),
		nil,
	)

	fields := []*types.Var{
		types.NewField(0, nil, "ID", types.Typ[types.String], false),
		types.NewField(0, nil, "ParamsStruct", embeddedType, true), // embedded=true
	}
	tags := []string{
		`path:"id"`,
		`query:"filter"`, // has query tag, so should be treated as param not body
	}
	st := types.NewStruct(fields, tags)

	params, body := extractParamsAndBody(st)

	require.Len(t, params, 2) // ID and the embedded struct treated as param
	require.Nil(t, body)      // No body because embedded field has param tag
}

func TestHasParamTag(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		want bool
	}{
		{"path tag", `path:"id"`, true},
		{"query tag", `query:"name"`, true},
		{"header tag", `header:"Authorization"`, true},
		{"json only", `json:"field"`, false},
		{"no tags", ``, false},
		{"multiple with path", `json:"field" path:"id"`, true},
		{"multiple with query", `json:"field" query:"name,optional"`, true},
		{"multiple without param tags", `json:"field" yaml:"field"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag := reflect.StructTag(tt.tag)
			require.Equal(t, tt.want, hasParamTag(tag))
		})
	}
}
