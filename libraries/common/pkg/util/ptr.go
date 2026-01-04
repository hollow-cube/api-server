package util

// Can be removed in go 1.26 :OOOO
func Ptr[T any](v T) *T {
	return &v
}
