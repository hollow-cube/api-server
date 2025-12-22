package model

type RecapData map[string]interface{}

type Recap struct {
	Id       string
	PlayerId string
	Username string
	Year     int
	Data     RecapData
}
