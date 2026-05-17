package ox

// HTTPError is implemented by error types that map to HTTP status codes.
type HTTPError interface {
	error
	StatusCode() int
}

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

// NotModified signals a conditional GET whose If-None-Match matched the
// current representation. The runtime writes a bare 304 with no body.
type NotModified struct{}

func (NotModified) StatusCode() int { return 304 }
func (NotModified) Error() string   { return "not modified" }

// PreconditionFailed signals a failed If-Match / If-None-Match precondition
// on a mutating request.
type PreconditionFailed struct{}

func (PreconditionFailed) StatusCode() int { return 412 }
func (PreconditionFailed) Error() string   { return "precondition failed" }
