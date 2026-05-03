package gen

import (
	"errors"
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseDirectives(t *testing.T) {
	tests := []struct {
		name         string
		doc          string
		wantMethod   string
		wantPath     string
		wantDesc     string
		wantProduces []string
		wantConsumes []string
		wantErr      bool
		wantNoRoute  bool // err should be errNoRouteComment
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
			name:       "first line route with other ox directives filtered",
			doc:        "PUT /items/{id}\nSome description.\nox:tags items",
			wantMethod: "PUT",
			wantPath:   "/items/{id}",
			wantDesc:   "Some description.",
		},
		{
			name:        "empty doc",
			doc:         "",
			wantErr:     true,
			wantNoRoute: true,
		},
		{
			name:        "no route declaration",
			doc:         "This is just a comment.\nNothing useful here.",
			wantErr:     true,
			wantNoRoute: true,
		},
		{
			name:        "ox:route is no longer supported",
			doc:         "Some description.\nox:route GET /items/{id}",
			wantErr:     true,
			wantNoRoute: true,
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
		{
			name:         "ox:produces single type",
			doc:          "GET /maps/{id}/world\nox:produces application/vnd.hollowcube.polar",
			wantMethod:   "GET",
			wantPath:     "/maps/{id}/world",
			wantProduces: []string{"application/vnd.hollowcube.polar"},
		},
		{
			name:       "ox:produces multiple types",
			doc:        "GET /maps/{id}/world\nox:produces application/vnd.hollowcube.polar, application/vnd.hollowcube.anvil, application/vnd.hollowcube.anvil-legacy",
			wantMethod: "GET",
			wantPath:   "/maps/{id}/world",
			wantProduces: []string{
				"application/vnd.hollowcube.polar",
				"application/vnd.hollowcube.anvil",
				"application/vnd.hollowcube.anvil-legacy",
			},
		},
		{
			name:         "ox:produces with description",
			doc:          "GET /maps/{id}/world\nStream the world data.\nox:produces application/octet-stream",
			wantMethod:   "GET",
			wantPath:     "/maps/{id}/world",
			wantDesc:     "Stream the world data.",
			wantProduces: []string{"application/octet-stream"},
		},
		{
			name:         "ox:consumes single type",
			doc:          "PUT /maps/{id}/world\nox:consumes application/vnd.hollowcube.polar",
			wantMethod:   "PUT",
			wantPath:     "/maps/{id}/world",
			wantConsumes: []string{"application/vnd.hollowcube.polar"},
		},
		{
			name:       "ox:consumes multiple types",
			doc:        "PUT /maps/{id}/world\nox:consumes application/vnd.hollowcube.polar, application/vnd.hollowcube.anvil",
			wantMethod: "PUT",
			wantPath:   "/maps/{id}/world",
			wantConsumes: []string{
				"application/vnd.hollowcube.polar",
				"application/vnd.hollowcube.anvil",
			},
		},
		{
			name:    "ox:produces missing slash",
			doc:     "GET /foo\nox:produces notamimetype",
			wantErr: true,
		},
		{
			name:    "ox:produces with whitespace inside type",
			doc:     "GET /foo\nox:produces application/foo bar",
			wantErr: true,
		},
		{
			name:    "ox:consumes missing slash",
			doc:     "PUT /foo\nox:consumes notamimetype",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := parseDirectives(tt.doc)
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantNoRoute {
					require.True(t, errors.Is(err, errNoRouteComment), "expected errNoRouteComment, got %v", err)
				} else {
					require.False(t, errors.Is(err, errNoRouteComment), "expected non-sentinel error, got %v", err)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantMethod, d.Method)
			require.Equal(t, tt.wantPath, d.Path)
			require.Equal(t, tt.wantDesc, d.Description)
			require.Equal(t, tt.wantProduces, d.Produces)
			require.Equal(t, tt.wantConsumes, d.Consumes)
		})
	}
}

func TestValidateResponse(t *testing.T) {
	tests := []struct {
		name    string
		resp    Response
		wantErr bool
	}{
		{
			name: "stream with produces",
			resp: Response{
				IsStream:   true,
				Produces:   []string{"application/octet-stream"},
				StatusCode: 200,
			},
		},
		{
			name: "stream without produces",
			resp: Response{
				IsStream:   true,
				StatusCode: 200,
			},
			wantErr: true,
		},
		{
			name: "non-stream with produces",
			resp: Response{
				Produces:   []string{"application/octet-stream"},
				StatusCode: 200,
			},
			wantErr: true,
		},
		{
			name: "stream with 204",
			resp: Response{
				IsStream:   true,
				Produces:   []string{"application/octet-stream"},
				StatusCode: 204,
			},
			wantErr: true,
		},
		{
			name: "ordinary response unchanged",
			resp: Response{
				StatusCode:  200,
				GoType:      "User",
				ContentType: "application/json",
				OAPIType:    "object",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResponse(tt.resp)
			if tt.wantErr {
				require.Error(t, err)
				require.False(t, errors.Is(err, errNoRouteComment))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRequestBody(t *testing.T) {
	tests := []struct {
		name     string
		body     *RequestBody
		consumes []string
		wantErr  bool
	}{
		{name: "no body, no consumes", body: nil},
		{name: "no body but consumes set", consumes: []string{"application/octet-stream"}, wantErr: true},
		{
			name:     "stream body with consumes",
			body:     &RequestBody{IsStream: true, GoName: "Body", GoType: "*ox.Stream"},
			consumes: []string{"application/octet-stream"},
		},
		{
			name:    "stream body without consumes",
			body:    &RequestBody{IsStream: true, GoName: "Body", GoType: "*ox.Stream"},
			wantErr: true,
		},
		{
			name:     "json body with consumes",
			body:     &RequestBody{GoName: "Body", GoType: "Foo"},
			consumes: []string{"application/octet-stream"},
			wantErr:  true,
		},
		{
			name: "json body without consumes",
			body: &RequestBody{GoName: "Body", GoType: "Foo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRequestBody(tt.body, tt.consumes)
			if tt.wantErr {
				require.Error(t, err)
				require.False(t, errors.Is(err, errNoRouteComment))
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestExtractParamsAndBody_StreamBody(t *testing.T) {
	const oxPath = "github.com/hollow-cube/api-server/pkg/ox"
	streamPkg := types.NewPackage(oxPath, "ox")
	streamNamed := types.NewNamed(
		types.NewTypeName(0, streamPkg, "Stream", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	streamPtr := types.NewPointer(streamNamed)

	// struct { MapID string `path:"mapId"`; Body *ox.Stream }
	fields := []*types.Var{
		types.NewField(0, nil, "MapID", types.Typ[types.String], false),
		types.NewField(0, nil, "Body", streamPtr, false),
	}
	tags := []string{
		`path:"mapId"`,
		``,
	}
	st := types.NewStruct(fields, tags)

	params, body := extractParamsAndBody(st, oxPath)

	require.Len(t, params, 1)
	require.Equal(t, "mapId", params[0].Name)

	require.NotNil(t, body)
	require.True(t, body.IsStream)
	require.Equal(t, "Body", body.GoName)
}

func TestExtractParamsAndBody_StreamWithCustomFieldName(t *testing.T) {
	const oxPath = "github.com/hollow-cube/api-server/pkg/ox"
	streamPkg := types.NewPackage(oxPath, "ox")
	streamNamed := types.NewNamed(
		types.NewTypeName(0, streamPkg, "Stream", nil),
		types.NewStruct(nil, nil),
		nil,
	)
	streamPtr := types.NewPointer(streamNamed)

	// struct { Stream *ox.Stream } — name doesn't have to be "Body" for streams
	fields := []*types.Var{
		types.NewField(0, nil, "Stream", streamPtr, false),
	}
	tags := []string{``}
	st := types.NewStruct(fields, tags)

	_, body := extractParamsAndBody(st, oxPath)

	require.NotNil(t, body)
	require.True(t, body.IsStream)
	require.Equal(t, "Stream", body.GoName)
}

func TestIsOxStream(t *testing.T) {
	const oxPath = "github.com/hollow-cube/api-server/pkg/ox"

	streamPkg := types.NewPackage(oxPath, "ox")
	streamNamed := types.NewNamed(
		types.NewTypeName(0, streamPkg, "Stream", nil),
		types.NewStruct(nil, nil),
		nil,
	)

	otherPkg := types.NewPackage("example.com/other", "other")
	otherStream := types.NewNamed(
		types.NewTypeName(0, otherPkg, "Stream", nil),
		types.NewStruct(nil, nil),
		nil,
	)

	wrongName := types.NewNamed(
		types.NewTypeName(0, streamPkg, "NotStream", nil),
		types.NewStruct(nil, nil),
		nil,
	)

	tests := []struct {
		name string
		typ  types.Type
		want bool
	}{
		{"pointer to ox.Stream", types.NewPointer(streamNamed), true},
		{"value ox.Stream rejected", streamNamed, false},
		{"pointer to other-package Stream", types.NewPointer(otherStream), false},
		{"pointer to wrong-name type", types.NewPointer(wrongName), false},
		{"basic type", types.Typ[types.String], false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isOxStream(tt.typ, oxPath))
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

	params, body := extractParamsAndBody(st, "")

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

	params, body := extractParamsAndBody(st, "")

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

	params, body := extractParamsAndBody(st, "")

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

	params, body := extractParamsAndBody(st, "")

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

	params, body := extractParamsAndBody(st, "")

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

func TestGetTypeName(t *testing.T) {
	tests := []struct {
		name string
		typ  types.Type
		want string
	}{
		{
			name: "basic string type",
			typ:  types.Typ[types.String],
			want: "string",
		},
		{
			name: "basic int type",
			typ:  types.Typ[types.Int],
			want: "int",
		},
		{
			name: "named type in same package",
			typ: types.NewNamed(
				types.NewTypeName(0, types.NewPackage("example.com/test", "test"), "MyType", nil),
				types.Typ[types.String],
				nil,
			),
			want: "MyType",
		},
		{
			name: "pointer to named type",
			typ: types.NewPointer(types.NewNamed(
				types.NewTypeName(0, types.NewPackage("example.com/test", "test"), "MyType", nil),
				types.Typ[types.String],
				nil,
			)),
			want: "*MyType",
		},
		{
			name: "slice of named type",
			typ: types.NewSlice(types.NewNamed(
				types.NewTypeName(0, types.NewPackage("example.com/test", "test"), "Item", nil),
				types.Typ[types.String],
				nil,
			)),
			want: "[]Item",
		},
		{
			name: "named type from external package",
			typ: types.NewNamed(
				types.NewTypeName(0, types.NewPackage("example.com/internal/interaction", "interaction"), "Command", nil),
				types.NewStruct(nil, nil),
				nil,
			),
			want: "Command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getTypeName(tt.typ)
			require.Equal(t, tt.want, got)
		})
	}
}
