package model

import "time"

type BoxShape string

type Box struct {
	Id             string
	PlayerId       *string
	CreatedAt      time.Time
	Name           *string
	Shape          BoxShape
	SchematicData  []byte
	LegacyUsername *string
}
