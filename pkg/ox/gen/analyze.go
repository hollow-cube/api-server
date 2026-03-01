package gen

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"reflect"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Analyze loads the Go package at pkgPattern, finds the struct named structName,
// and extracts API endpoint information from its methods.
func Analyze(pkgPattern, structName string) (*API, error) {
	// First, try to determine the directory and package name to find server.gen.go
	cfg := &packages.Config{
		Mode: packages.NeedFiles | packages.NeedName,
	}
	tmpPkgs, err := packages.Load(cfg, pkgPattern)
	var serverGenPath string
	var pkgName string
	if err == nil && len(tmpPkgs) > 0 {
		pkgName = tmpPkgs[0].Name
		if len(tmpPkgs[0].GoFiles) > 0 {
			dir := filepath.Dir(tmpPkgs[0].GoFiles[0])
			serverGenPath = filepath.Join(dir, "server.gen.go")
		}
	}

	// Now load with full info, excluding server.gen.go via overlay
	cfg = &packages.Config{
		Mode: packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax |
			packages.NeedName | packages.NeedModule | packages.NeedFiles,
	}

	// Exclude server.gen.go to avoid circular dependency issues when regenerating
	// after compilation errors in the generated file
	if serverGenPath != "" && pkgName != "" {
		cfg.Overlay = map[string][]byte{
			serverGenPath: []byte("package " + pkgName + "\n"),
		}
	}

	pkgs, err := packages.Load(cfg, pkgPattern)
	if err != nil {
		return nil, fmt.Errorf("loading package: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found for pattern %q", pkgPattern)
	}
	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		return nil, fmt.Errorf("package error: %v", pkg.Errors[0])
	}

	obj := pkg.Types.Scope().Lookup(structName)
	if obj == nil {
		return nil, fmt.Errorf("type %s not found in package %s", structName, pkg.Name)
	}

	named, ok := obj.Type().(*types.Named)
	if !ok {
		return nil, fmt.Errorf("%s is not a named type", structName)
	}

	api := &API{
		PackageName: pkg.Name,
		StructName:  structName,
	}
	if pkg.Module != nil {
		api.ModulePath = pkg.Module.Path
	}
	if len(pkg.GoFiles) > 0 {
		api.OutputDir = filepath.Dir(pkg.GoFiles[0])
	}

	for i := 0; i < named.NumMethods(); i++ {
		method := named.Method(i)
		if !method.Exported() {
			continue
		}
		doc := findMethodDoc(pkg, method.Name())
		ep, err := analyzeMethod(method, doc)
		if err != nil {
			continue // not an endpoint (no route comment)
		}
		api.Endpoints = append(api.Endpoints, *ep)
	}

	if len(api.Endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints found on %s", structName)
	}

	return api, nil
}

func findMethodDoc(pkg *packages.Package, methodName string) string {
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != methodName {
				continue
			}
			if fn.Doc != nil {
				return fn.Doc.Text()
			}
		}
	}
	return ""
}

func analyzeMethod(method *types.Func, doc string) (*Endpoint, error) {
	sig := method.Type().(*types.Signature)

	httpMethod, path, desc, err := parseRouteComment(doc)
	if err != nil {
		return nil, err
	}

	ep := &Endpoint{
		Name:        method.Name(),
		Method:      httpMethod,
		Path:        path,
		Description: desc,
		Response:    extractResponse(sig, method.Name()),
	}

	// Extract request params and body from method parameters (after context.Context)
	params := sig.Params()
	for i := 1; i < params.Len(); i++ {
		param := params.At(i)
		paramName := param.Name()
		paramType := param.Type()

		// Check if this is a body parameter (parameter named "body")
		if paramName == "body" {
			typeName := getTypeName(paramType)
			ep.RequestBody = &RequestBody{
				GoName:   paramName,
				GoType:   typeName,
				Required: true,
			}
			continue
		}

		// Otherwise, treat as request struct with path/query/header params
		if named, ok := paramType.(*types.Named); ok {
			ep.RequestType = named.Obj().Name()
			if st, ok := named.Underlying().(*types.Struct); ok {
				ep.Params, ep.RequestBody = extractParamsAndBody(st)
			}
		}
	}

	return ep, nil
}

func parseRouteComment(doc string) (method, path, description string, err error) {
	if doc == "" {
		return "", "", "", fmt.Errorf("no doc comment")
	}

	lines := strings.Split(strings.TrimSpace(doc), "\n")

	// Check for //ox:route directive in any line
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ox:route ") {
			rest := strings.TrimPrefix(line, "ox:route ")
			parts := strings.SplitN(rest, " ", 2)
			if len(parts) == 2 && isHTTPMethod(parts[0]) {
				method = parts[0]
				path = strings.TrimSpace(parts[1])
				var descLines []string
				for j, l := range lines {
					if j == i {
						continue
					}
					l = strings.TrimSpace(l)
					if !strings.HasPrefix(l, "ox:") {
						descLines = append(descLines, l)
					}
				}
				description = strings.TrimSpace(strings.Join(descLines, "\n"))
				return
			}
		}
	}

	// Check for "METHOD /path" as the first line
	firstLine := strings.TrimSpace(lines[0])
	parts := strings.SplitN(firstLine, " ", 2)
	if len(parts) == 2 && isHTTPMethod(parts[0]) {
		method = parts[0]
		path = strings.TrimSpace(parts[1])
		if len(lines) > 1 {
			var descLines []string
			for _, l := range lines[1:] {
				l = strings.TrimSpace(l)
				if !strings.HasPrefix(l, "ox:") {
					descLines = append(descLines, l)
				}
			}
			description = strings.TrimSpace(strings.Join(descLines, "\n"))
		}
		return
	}

	return "", "", "", fmt.Errorf("no route declaration found")
}

func isHTTPMethod(s string) bool {
	switch s {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	}
	return false
}

func extractParamsAndBody(st *types.Struct) ([]Param, *RequestBody) {
	var params []Param
	var body *RequestBody

	for i := 0; i < st.NumFields(); i++ {
		field := st.Field(i)
		tag := reflect.StructTag(st.Tag(i))

		// Check if this is a Body field (no tags, named "Body")
		if field.Name() == "Body" && tag == "" {
			typeName := getTypeName(field.Type())
			body = &RequestBody{
				GoName:   field.Name(),
				GoType:   typeName,
				Required: true,
			}
			continue
		}

		// Check if this is an embedded struct that should be treated as the body
		// (anonymous field with no tags, or with only json tags)
		if field.Embedded() && !hasParamTag(tag) {
			typeName := getTypeName(field.Type())
			body = &RequestBody{
				GoName:   field.Name(),
				GoType:   typeName,
				Required: true,
			}
			continue
		}

		var p Param
		p.GoName = field.Name()
		p.GoType = types.TypeString(field.Type(), nil)

		if v, ok := tag.Lookup("path"); ok {
			p.Name = tagName(v)
			p.Location = "path"
			p.Required = true
		} else if v, ok := tag.Lookup("query"); ok {
			name, optional := parseTagOpts(v)
			p.Name = name
			p.Location = "query"
			p.Required = !optional
		} else if v, ok := tag.Lookup("header"); ok {
			p.Name = tagName(v)
			p.Location = "header"
			p.Required = true
		} else {
			continue
		}

		p.OAPIType, p.OAPIFmt = goTypeToOpenAPI(field.Type())
		params = append(params, p)
	}
	return params, body
}

// hasParamTag checks if the tag contains path, query, or header tags
func hasParamTag(tag reflect.StructTag) bool {
	_, hasPath := tag.Lookup("path")
	_, hasQuery := tag.Lookup("query")
	_, hasHeader := tag.Lookup("header")
	return hasPath || hasQuery || hasHeader
}

func tagName(v string) string {
	if i := strings.Index(v, ","); i >= 0 {
		return v[:i]
	}
	return v
}

func parseTagOpts(v string) (name string, optional bool) {
	parts := strings.Split(v, ",")
	name = parts[0]
	for _, opt := range parts[1:] {
		if opt == "optional" {
			optional = true
		}
	}
	return
}

func goTypeToOpenAPI(t types.Type) (typ, format string) {
	switch t := t.Underlying().(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.String:
			return "string", ""
		case types.Bool:
			return "boolean", ""
		case types.Int, types.Int8, types.Int16, types.Int32:
			return "integer", ""
		case types.Int64:
			return "integer", "int64"
		case types.Uint, types.Uint8, types.Uint16, types.Uint32:
			return "integer", ""
		case types.Uint64:
			return "integer", "int64"
		case types.Float32:
			return "number", "float"
		case types.Float64:
			return "number", "double"
		}
	}
	return "object", ""
}

func extractResponse(sig *types.Signature, methodName string) Response {
	resp := Response{
		StatusCode: inferStatusCode(methodName),
	}

	results := sig.Results()
	if results.Len() < 2 {
		return resp
	}

	// First result is the response type, second is error
	t := results.At(0).Type()
	if ptr, ok := t.(*types.Pointer); ok {
		resp.IsPtr = true
		t = ptr.Elem()
	}

	resp.GoType = types.TypeString(t, nil)
	resp.OAPIType, resp.OAPIFmt = goTypeToOpenAPI(t)

	// Determine content type: string -> text/plain, everything else -> application/json
	if basic, ok := t.Underlying().(*types.Basic); ok && basic.Kind() == types.String {
		resp.ContentType = "text/plain"
	} else {
		resp.ContentType = "application/json"
	}

	return resp
}

func inferStatusCode(name string) int {
	switch {
	case strings.HasPrefix(name, "Create"):
		return 201
	case strings.HasPrefix(name, "Delete"):
		return 204
	default:
		return 200
	}
}

// getTypeName extracts the short type name from a types.Type.
// For named types, returns just the name. For pointers, adds "*" prefix.
// Uses a qualifier that omits package paths, assuming types are either
// in the current package or imported.
func getTypeName(t types.Type) string {
	// Use a qualifier that returns empty string (no package prefix)
	// This works because the generated code is in the same package
	qualifier := func(*types.Package) string { return "" }
	return types.TypeString(t, qualifier)
}
