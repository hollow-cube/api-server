package util

func EmptyToNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func Ptr[T any](v T) *T {
	return &v
}
