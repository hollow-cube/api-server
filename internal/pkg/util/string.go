package util

import (
	"errors"
	"fmt"
	"path"
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
	return &s
}

var idPattern = regexp.MustCompile(`^([0-9]{3})-([0-9]{3})-([0-9]{3})$`)

func ParseMapPublishedID(publishedID string) (int64, error) {
	if !idPattern.MatchString(publishedID) {
		return 0, fmt.Errorf("invalid published ID format")
	}
	return strconv.ParseInt(strings.ReplaceAll(publishedID, "-", ""), 10, 64)
}

func NormalizePath(p string) (string, error) {
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("invalid path")
	}
	cleaned = strings.TrimPrefix(cleaned, "/")
	if strings.ContainsRune(cleaned, '\\') {
		return "", errors.New("backslashes not allowed in path")
	}
	if len(cleaned) == 0 || len(cleaned) > 512 {
		return "", errors.New("path length out of range")
	}
	return cleaned, nil
}
