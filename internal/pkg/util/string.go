package util

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func NilToEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func EmptyToNil(s string) *string {
	if s == "" {
		return nil
	}
	return s
}

var idPattern = regexp.MustCompile(`^([0-9]{3})-([0-9]{3})-([0-9]{3})$`)

func ParseMapPublishedID(publishedID string) (int64, error) {
	if !idPattern.MatchString(publishedID) {
		return 0, fmt.Errorf("invalid published ID format")
	}
	return strconv.ParseInt(strings.ReplaceAll(publishedID, "-", ""), 10, 64)
}
