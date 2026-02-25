package util

func NilToEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
