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

// RequestBody represents a request body parameter.
type RequestBody struct {
	GoName   string // Parameter name or struct field name (e.g. "body" or "Body")
	GoType   string // Go type as string
	Required bool   // Always true for body parameters

	// IsStream is true when the body is *ox.Stream (raw bytes). The runtime
	// populates ContentType/Body/ContentLength from the request rather than
	// JSON-decoding. Consumes enumerates the MIME types the endpoint accepts.
	IsStream bool
	Consumes []string

	// IsRawBytes is true when the body is a plain []byte field. The runtime
	// reads the request body in full with io.ReadAll. Treated as
	// application/octet-stream in the OpenAPI spec by default; Consumes may
	// override the accepted MIME types.
	IsRawBytes bool

	// IsReader is true when the body is an io.Reader / io.ReadCloser field.
	// The runtime passes r.Body through verbatim without buffering it, so the
	// handler can impose its own bounded read (e.g. io.LimitReader) instead of
	// reading an unbounded body into memory. Treated as
	// application/octet-stream in the OpenAPI spec by default; Consumes may
	// override the accepted MIME types.
	IsReader bool
}

// Param represents a request parameter extracted from struct tags.
type Param struct {
	Name     string // Tag value (URL param name, query key, header name)
	GoName   string // Go struct field name
	GoType   string // Underlying basic kind for parsing ("string", "int", "int64", "bool")
	ElemType string // Concrete (possibly named) Go type to assign to the field. Equals GoType for unnamed basics.
	Location string // "path", "query", or "header"

	// IsPointer is true when the field is a pointer (*T). Pointer params are
	// implicitly optional — Required is forced to false. The generated
	// decoder allocates a value and assigns the pointer only when the
	// query/header value is non-empty.
	IsPointer bool
	Required  bool
	OAPIType  string // OpenAPI type
	OAPIFmt   string // OpenAPI format, empty if not applicable

	// IsWildcard is true for path parameters declared as {*name} — these
	// capture the remainder of the URL (one or more path segments). Only
	// valid for path params; must be the last segment in the route.
	IsWildcard bool
}

// Response describes the handler's return type.
type Response struct {
	StatusCode  int
	GoType      string // empty if no response body (e.g. error-only return)
	IsPtr       bool
	OAPIType    string
	OAPIFmt     string
	ContentType string // "text/plain" for string, "application/json" for structs

	// IsStream is true when the handler returns *ox.Stream. In that case the
	// response is a binary/streaming body and Produces enumerates the MIME
	// types the endpoint may emit at runtime.
	IsStream bool
	Produces []string

	// IsSSE is true when the handler returns iter.Seq2[ox.Event[T], error].
	// SSEPayloadGoType holds the Go source string for T, used to instantiate
	// the generic runtime.WriteSSE call.
	IsSSE            bool
	SSEPayloadGoType string
}
