package util

import "github.com/google/uuid"

func RemapUUID(str string) string {
	value, err := uuid.Parse(str)
	if err != nil {
		panic(err)
	}
	return value.String()
}
