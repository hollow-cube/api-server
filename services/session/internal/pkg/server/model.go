package server

import "time"

type StatusV2 string

const ( // Names are max 10 chars long
	Starting StatusV2 = "starting"
	Active   StatusV2 = "active"
	Draining StatusV2 = "draining"
)

type Status int

const (
	NotReady Status = iota
	Ready
)

type State struct {
	ID          string
	Role        string
	StartTime   time.Time
	StatusV2    StatusV2
	StatusSince time.Time
	ClusterIP   string
}
