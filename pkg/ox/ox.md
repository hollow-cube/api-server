# ox — Go API Code Generator

## Overview

`ox` is a Go code generator that analyzes Go source code and emits:
1. An OpenAPI 3 specification
2. A `server.gen.go` file containing glue code for `net/http` ServeMux

**IMPORTANT**: A user-facing `README.md` exists in `/pkg/ox/README.md` that documents how to use the library. This `ox.md` file is implementation guidance for code generation. The README.md must be kept up-to-date whenever this file changes or new features are added.

## Project Layout

- `/pkg/ox` — the ox library (marker types, error types, runtime helpers)
  - `README.md` — user-facing documentation (MUST be kept in sync with this file)
- `/cmd/ox` — the CLI tool (`ox generate ./path/to/package.MyServer`)

## CLI Usage

```
ox generate ./path/to/package.MyServer
```

Takes a package path and struct name (similar to `go build` patterns). The generator loads the package using `golang.org/x/tools/go/packages`, finds the named struct, analyzes its methods, and emits output.

## Code Analysis

Use the Go analysis toolchain:
- `golang.org/x/tools/go/packages` — load packages with `NeedTypes | NeedTypesInfo | NeedSyntax | NeedName`
- `go/types` — resolve types, inspect method signatures, struct fields, struct tags, embedded types
- `go/ast` — access doc comments on methods and types

### Discovery Flow
1. Load the target package
2. Look up the named struct in the package scope via `pkg.Types.Scope().Lookup(name)`
3. Get the `*types.Named` type
4. Iterate methods via `named.NumMethods()` / `named.Method(i)`
5. For each method, inspect the signature's params and return types
6. Cross-reference back to AST for doc comments

## OpenAPI Spec Generation

Use `github.com/pb33f/libopenapi` for building and serializing the OpenAPI 3 spec.

## Design Decisions

### Defining Endpoints

Handlers are methods on a user-defined struct (the "server"). Each method represents one API endpoint.

```go
type UserService struct{}

// GET /users/{id}
func (s *UserService) GetUser(ctx context.Context, req GetUserRequest) (*User, error) {}
```

### Route Declaration

Routes are declared as the first line of the doc comment in the format `METHOD /path`.
Any path parameters are declared as `{name}`, and may be present in the request struct.

### Metadata Directives

Additional metadata uses `//ox:` prefixed directives in doc comments:
- `//ox:produces type1, type2` — content types this endpoint can produce (required for `*ox.Stream` responses)
- `//ox:consumes type1, type2` — content types this endpoint accepts (required for `*ox.Stream` request bodies)
- `//ox:stability stable` — stability marker
- `//ox:deprecated Use GetUserV2 instead` — deprecation notice
- `//ox:tags users,admin` — OpenAPI tags

The description is the regular godoc prose between the first line and the directives.

### Request Parameters

Use struct tags on the request struct to bind parameters:

```go
type GetUserRequest struct {
    ID     string `path:"id"`
    Expand string `query:"expand,optional"`
    Token  string `header:"Authorization"`
}
```

- `path:` — path parameter. it is invalid to declare a path parameter without a corresponding path segment in the route
- `query:` — query parameter
- `header:` — header parameter

#### Request Body

The request body can be specified in three ways:

1. **Named `Body` field** — A field named `Body` without parameter tags
2. **Embedded struct** — An embedded struct without parameter tags (e.g., `UpdatePlayerDataRequestBody`)
3. **Separate `body` parameter** — A handler parameter named `body`

```go
// Example 1: Body field
type UpdateRequest struct {
    ID   string `path:"id"`
    Body UpdateBody
}

// Example 2: Embedded struct (recommended)
type UpdateRequest struct {
    ID string `path:"id"`
    UpdateRequestBody
}

// Example 3: Separate parameter
func (s *Server) Create(ctx context.Context, body CreateRequest) error
```

The generator reads struct fields and tags via `go/types` and `go/ast` to build OpenAPI parameter and request body schemas.

### Response Types

The return type of a handler is `(*ResponseType, error)`.

#### Content Types

The content type is determined by the response type:
- **`string`** — Returns `text/plain` content type
- **All other types (structs, etc.)** — Returns `application/json` content type

The generator inspects the underlying type to make this determination. This affects both the OpenAPI spec generation and the generated handler code.

#### Success Status Codes

Inferred by convention from the method name prefix:
- `Get` → 200
- `Create` → 201
- `Delete` → 204
- `Update` → 200
- `List` → 200

For non-standard status codes, embed a marker type from the `ox` package (inspired by `fx.In`/`fx.Out` pattern):

```go
package ox

type OK struct{}        // 200
type Created struct{}   // 201
type Accepted struct{}  // 202
type NoContent struct{} // 204
```

```go
type ExportResult struct {
	ox.Accepted
    Export
}
```

The generator checks if the return type has an embedded status type and uses it; otherwise falls back to method name convention.

#### Response Descriptions

The OpenAPI description for a response type comes from the godoc comment on the response struct.

### Error Handling

#### Global Error Types

Errors are declared globally by defining structs that embed base error types from the `ox` package. All registered error types are documented as possible responses on every endpoint.

```go
package ox

// Base error types — use value receivers so both value and pointer returns work
type NotFound struct{}
func (NotFound) StatusCode() int { return 404 }
func (NotFound) Error() string   { return "not found" }

type BadRequest struct{}
func (BadRequest) StatusCode() int { return 400 }
func (BadRequest) Error() string   { return "bad request" }

type Conflict struct{}
func (Conflict) StatusCode() int { return 409 }
func (Conflict) Error() string   { return "conflict" }

type Unauthorized struct{}
func (Unauthorized) StatusCode() int { return 401 }
func (Unauthorized) Error() string   { return "unauthorized" }

type Forbidden struct{}
func (Forbidden) StatusCode() int { return 403 }
func (Forbidden) Error() string   { return "forbidden" }

type ValidationError struct{}
func (ValidationError) StatusCode() int { return 422 }
func (ValidationError) Error() string   { return "validation error" }

// NotModified — for conditional GETs; the runtime writes a bare 304 (no body)
type NotModified struct{}
func (NotModified) StatusCode() int { return 304 }
func (NotModified) Error() string   { return "not modified" }

// PreconditionFailed — for failed If-Match / If-None-Match preconditions
type PreconditionFailed struct{}
func (PreconditionFailed) StatusCode() int { return 412 }
func (PreconditionFailed) Error() string   { return "precondition failed" }
```

`ox.Stream` has an `ETag` field written verbatim to the `ETag` response header
when non-empty. Conditional-request headers (`If-Match`, `If-None-Match`) are
read with ordinary `header:` request params; handlers return `ox.NotModified`
or `ox.PreconditionFailed` to short-circuit.

#### User-Defined Errors

Users extend base errors by embedding them. Methods are promoted via Go embedding, so they satisfy the `HTTPError` interface automatically. All methods must be **value receivers** so users can return values without pointers.

```go
type UserNotFound struct {
    ox.NotFound
    UserID string `json:"userId"`
}

// Usage in a handler:
return UserNotFound{UserID: "abc"} // no pointer needed
```

#### HTTPError Interface

The runtime uses this interface to determine status codes:

```go
type HTTPError interface {
    error
    StatusCode() int
}
```

#### Error Discovery

The generator finds all types in the user's package that embed an `ox.*` error type (transitively implementing `HTTPError`). These are all documented as possible error responses on every endpoint in the OpenAPI spec. No per-handler error declaration is needed.

### Generated Glue Code (`server.gen.go`)

The generated file provides:

#### `RegisterParams` struct
Configuration for route registration:
```go
type RegisterParams struct {
    Mux     *http.ServeMux
    BaseURL string // Optional prefix for all routes
}
```

#### `RegisterRoutes` function
Registers all routes:
```go
func RegisterRoutes(s *Server, params RegisterParams)
```

Usage:
```go
mux := http.NewServeMux()
server := NewServer(...)
RegisterRoutes(server, RegisterParams{
    Mux:     mux,
    BaseURL: "/api/v1", // optional prefix for all routes
})

// Routes are registered as: "METHOD "+BaseURL+"/path"
// Example: "GET /api/v1/users/{id}"
```

#### `handlers` struct (internal)
A private struct that wraps the server and provides HTTP handler methods. This keeps handler implementations out of the package namespace.

#### Handler flow
Each generated handler method:
1. Decodes path/query/header params from the request based on struct tags
2. Decodes JSON body if applicable (Body field, embedded struct, or body parameter)
3. Calls the server method
4. Handles errors:
    - If `nil`, marshal the response with the appropriate success status code
    - If implements `HTTPError`, marshal the error with its status code
    - Otherwise, return 500
5. Marshals the response:
    - For `string` responses: Use `runtime.WriteText()` with `text/plain` content type
    - For all other responses: Use `runtime.WriteJSON()` with `application/json` content type

### Key Principles

- **Types do the heavy lifting** — request/response shapes, parameter binding, error status codes are all expressed in Go's type system
- **Comments are minimal** — only used for route path, description, and light metadata directives. Never a parallel spec language
- **Convention over configuration**
- **Explicit overrides exist** — embedded marker types for status codes, `//ox:` directives for metadata
- **Global error registration** — all error types are possible on all endpoints, no per-handler boilerplate

