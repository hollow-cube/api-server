package v1

import (
	"net/http"

	commonV1 "github.com/hollow-cube/hc-services/libraries/common/pkg/api"
)

var (
	ErrSessionNotFound = &commonV1.Error{HTTP: http.StatusNotFound, Code: "session_not_found", Message: "Session not found"}

	ErrSchemNotFound = &commonV1.Error{HTTP: http.StatusNotFound, Code: "schem_not_found", Message: "Schematic not found"}
)
