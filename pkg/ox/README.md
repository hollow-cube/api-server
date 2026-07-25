# ox — Go API Code Generator

`ox` is a type-driven API code generator for Go that produces OpenAPI specs and HTTP handler glue code from annotated Go methods.

## Quick Start

1. Define a server struct with handler methods:

```go
package myapi

import (
    "context"
    "github.com/hollow-cube/api-server/pkg/ox"
)

type UserService struct {
    // your dependencies here
}

type GetUserRequest struct {
    ID string `path:"id"`
}

type User struct {
    ID    string `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

// GET /users/{id}
//
// Retrieves a user by their ID.
func (s *UserService) GetUser(ctx context.Context, req GetUserRequest) (*User, error) {
    // your implementation
}
```

2. Generate the code:

```bash
ox generate ./path/to/myapi.UserService
```

3. Use the generated code:

```go
package main

import (
    "net/http"
    "myapi"
)

func main() {
    service := &myapi.UserService{}

    mux := http.NewServeMux()
    myapi.RegisterRoutes(mux, service)

    http.ListenAndServe(":8080", mux)
}
```

## Handler Method Signature

```go
func (s *ServerStruct) MethodName(ctx context.Context, req RequestStruct) (ResponseType, error)
```

- **First parameter**: Always `context.Context`
- **Second parameter** (optional): Request struct containing parameters
- **Return values**: Response value (or pointer) and error

## Declaring Routes

Routes are declared on the first line of the method's doc comment as `METHOD /path`:

```go
// GET /users/{id}
func (s *UserService) GetUser(ctx context.Context, req GetUserRequest) (*User, error)
```

Description prose follows on the next lines. Lines starting with `ox:` are treated as directives.

Supported methods: `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`

## Request Parameters

Bind HTTP parameters using struct tags on the request struct:

```go
type GetUserRequest struct {
    ID       string `path:"id"`              // path parameter
    Expand   string `query:"expand,optional"` // optional query parameter
    APIKey   string `header:"X-API-Key"`      // header parameter
}
```

### Parameter Locations

- **`path:"name"`** — Path parameter (must match `{name}` in route)
- **`query:"name"`** — Query parameter
  - Add `,optional` for optional query params
- **`header:"Name"`** — Header parameter

### Supported Parameter Types

- `string`
- `int`, `int64`
- `bool`

## Response Types

### Content Types

- **`string`** — Returns `text/plain`
- **Struct types** — Returns `application/json`
- **`*ox.Stream`** — Returns raw bytes with a content type chosen at runtime (see [Streaming Responses](#streaming-responses))

```go
// Returns plain text
func (s *Service) GetStatus(ctx context.Context) (string, error) {
    return "OK", nil
}

// Returns JSON
func (s *Service) GetUser(ctx context.Context, req GetUserRequest) (*User, error) {
    return &User{ID: "123", Name: "Alice"}, nil
}
```

### Streaming Responses

Return `*ox.Stream` to write raw bytes with a runtime-chosen content type. The
endpoint must declare every MIME type it can produce via `ox:produces`; the
handler picks one from that set (typically based on the `Accept` header) and
sets it on the returned stream.

```go
type GetMapWorldRequest struct {
    MapID  string `path:"mapId"`
    Accept string `header:"Accept"`
}

// GET /maps/{mapId}/world
// ox:produces application/vnd.hollowcube.polar, application/vnd.hollowcube.anvil
func (s *Service) GetMapWorld(ctx context.Context, req GetMapWorldRequest) (*ox.Stream, error) {
    data, ct, err := s.fetchWorld(ctx, req.MapID, req.Accept)
    if err != nil {
        return nil, err
    }
    return &ox.Stream{
        ContentType:   ct,
        Body:          bytes.NewReader(data),
        ContentLength: int64(len(data)),
    }, nil
}
```

If `Body` implements `io.Closer` it is closed after the response is written.
`ContentLength` is optional — pass `0` if the length is unknown.

Set `ETag` to emit an `ETag` response header (the value must already be a valid
header token, i.e. a quoted string, optionally `W/`-prefixed).

### Conditional Requests

Read the conditional headers with ordinary `header:` params and return the
matching sentinel error to short-circuit:

- On a GET, return `ox.NotModified` when `If-None-Match` matches the current
  representation. The runtime writes a bare `304` with no body.
- On a mutating request, return `ox.PreconditionFailed` (`412`) when an
  `If-Match` / `If-None-Match` precondition is not satisfied.

```go
type GetThingRequest struct {
    ID          string `path:"id"`
    IfNoneMatch string `header:"If-None-Match,optional"`
}

func (s *Service) GetThing(ctx context.Context, req GetThingRequest) (*ox.Stream, error) {
    t, err := s.load(ctx, req.ID)
    if err != nil {
        return nil, err
    }
    etag := fmt.Sprintf("%q", t.Hash)
    if req.IfNoneMatch != "" && req.IfNoneMatch == etag {
        return nil, ox.NotModified{}
    }
    return &ox.Stream{Body: t.Reader(), ETag: etag}, nil
}
```

### Streaming / Binary Request Bodies

Add an `*ox.Stream` field on the request struct (or use `body *ox.Stream` as a
parameter) to receive a raw request body. Declare the accepted MIME types with
`ox:consumes`. The runtime sets `ContentType`, `Body` (the unread `r.Body`),
and `ContentLength` on the stream.

```go
type UpdateMapWorldRequest struct {
    MapID string `path:"mapId"`
    Body  *ox.Stream
}

// PUT /maps/{mapId}/world
// ox:consumes application/vnd.hollowcube.polar, application/vnd.hollowcube.anvil
func (s *Service) UpdateMapWorld(ctx context.Context, req UpdateMapWorldRequest) error {
    return s.upload(ctx, req.MapID, req.Body.ContentType, req.Body.Body)
}
```

### Status Codes

Status codes are inferred from the method name:

- `Get*` → 200 OK
- `Create*` → 201 Created
- `Delete*` → 204 No Content
- Everything else → 200 OK

To override, embed a status marker type:

```go
type ExportResult struct {
    ox.Accepted  // 202 Accepted
    Export
}

func (s *Service) CreateExport(ctx context.Context) (*ExportResult, error) {
    return &ExportResult{Export: Export{ID: "123"}}, nil
}
```

Available status markers in `pkg/ox`:
- `ox.OK` (200)
- `ox.Created` (201)
- `ox.Accepted` (202)
- `ox.NoContent` (204)

## Error Handling

Define custom errors by embedding base error types from `pkg/ox`:

```go
type UserNotFound struct {
    ox.NotFound
    UserID string `json:"userId"`
}

func (s *UserService) GetUser(ctx context.Context, req GetUserRequest) (*User, error) {
    user, err := s.db.Find(req.ID)
    if err != nil {
        return nil, UserNotFound{UserID: req.ID}
    }
    return user, nil
}
```

Base error types:
- `ox.NotFound` (404)
- `ox.BadRequest` (400)
- `ox.Conflict` (409)
- `ox.Unauthorized` (401)
- `ox.Forbidden` (403)
- `ox.ValidationError` (422)
- `ox.NotModified` (304) — bare response, no body; for conditional GETs
- `ox.PreconditionFailed` (412) — for failed `If-Match` / `If-None-Match`

**Important**: All error types use **value receivers**, so you can return them as values (no pointer needed).

All custom error types in your package are automatically documented in the OpenAPI spec as possible responses on every endpoint.

## Additional Metadata

Use `ox:` directives in doc comments for additional metadata:

```go
// GET /users/{id}
//
// Retrieves a user by their unique identifier.
//
// ox:tags users,admin
// ox:stability stable
// ox:deprecated Use GetUserV2 instead
func (s *UserService) GetUser(ctx context.Context, req GetUserRequest) (*User, error)
```

Available directives:
- `ox:produces type1, type2` — Content types this endpoint can return (required for `*ox.Stream` responses)
- `ox:consumes type1, type2` — Content types this endpoint accepts (required for `*ox.Stream` request bodies)
- `ox:tags tag1,tag2` — OpenAPI tags
- `ox:stability stable|beta|alpha` — Stability marker
- `ox:deprecated message` — Deprecation notice

## Generated Files

`ox generate` produces two files in the same directory as your server struct:

### `openapi.yaml`

OpenAPI 3.0.3 specification describing your API.

### `server.gen.go`

Go code containing:
- `RegisterRoutes(mux *http.ServeMux, s *YourServer)` — Registers all routes
- Handler functions that:
  - Parse path/query/header parameters
  - Validate required parameters
  - Call your handler methods
  - Marshal responses with correct content types
  - Handle errors with appropriate status codes

## Example

See a complete example in `api/v4/`:
- `server.go` — Handler implementation
- `server.gen.go` — Generated glue code
- `openapi.yaml` — Generated OpenAPI spec

## CLI Usage

```bash
# Generate from a package.StructName
ox generate ./api/v4.Server

# Generate from current directory
ox generate .MyServer
```

The package path follows Go's package patterns (same as `go build`).

## Design Philosophy

- **Types over comments** — Request/response shapes, parameter binding, and error codes are expressed in Go's type system
- **Convention over configuration** — Sensible defaults with explicit overrides when needed
- **Minimal annotations** — Only route paths and light metadata in comments
- **Standard library** — Uses `net/http` ServeMux (Go 1.22+ routing)
