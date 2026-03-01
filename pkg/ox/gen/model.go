package gen

// API represents the analyzed server struct and its endpoints.
type API struct {
	PackageName string
	StructName  string
	ModulePath  string
	OutputDir   string
	Endpoints   []Endpoint
}

// Endpoint represents a single API endpoint derived from a method.
type Endpoint struct {
	Name        string // Go method name
	Method      string // HTTP method (GET, POST, etc.)
	Path        string // URL path with {param} placeholders
	Description string
	RequestType string // Go type name of the request struct, empty if none
	Params      []Param
	RequestBody *RequestBody // Optional JSON request body
	Response    Response
}

// RequestBody represents a JSON request body parameter.
type RequestBody struct {
	GoName   string // Parameter name or "Body" if from struct field
	GoType   string // Go type as string
	Required bool   // Always true for body parameters
}

// Param represents a request parameter extracted from struct tags.
type Param struct {
	Name     string // Tag value (URL param name, query key, header name)
	GoName   string // Go struct field name
	GoType   string // Go type as string ("string", "int", etc.)
	Location string // "path", "query", or "header"
	Required bool
	OAPIType string // OpenAPI type
	OAPIFmt  string // OpenAPI format, empty if not applicable
}

// Response describes the handler's return type.
type Response struct {
	StatusCode  int
	GoType      string // empty if no response body (e.g. error-only return)
	IsPtr       bool
	OAPIType    string
	OAPIFmt     string
	ContentType string // "text/plain" for string, "application/json" for structs
}
