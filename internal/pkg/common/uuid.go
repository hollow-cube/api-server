package common

import (
	"regexp"

	"github.com/google/uuid"
)

// Expression from https://ihateregex.io/expr/uuid/
var uuidRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}\b-[0-9a-fA-F]{4}\b-[0-9a-fA-F]{4}\b-[0-9a-fA-F]{4}\b-[0-9a-fA-F]{12}$`)

func IsUUID(s string) bool {
	return uuidRegex.MatchString(s)
}

func NewUUID() string {
	return uuid.NewString()
}

func UUIDToBin(s string) []byte {
	// There is no case marshalbinary will fail
	b, _ := uuid.MustParse(s).MarshalBinary()
	return b
}

func UUIDFromBin(b []byte) string {
	u := uuid.UUID{}
	// There is no case unmarshalbinary will fail
	if err := u.UnmarshalBinary(b); err != nil {
		panic(err)
	}
	return u.String()
}
