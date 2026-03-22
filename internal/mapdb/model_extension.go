package mapdb

type Pos struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`

	Yaw   float64 `json:"yaw"`
	Pitch float64 `json:"pitch"`
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type MapExt struct {
	Objects map[string]*ObjectData `json:"objects"` // Objects by ID
}

type ObjectData struct {
	Id   string                 `json:"id"`
	Type string                 `json:"type"`
	Pos  Point                  `json:"pos"`
	Data map[string]interface{} `json:"data"`
}

type Leaderboard struct {
	Asc    bool   `json:"asc"`
	Format string `json:"format"` // 'number', 'percent', 'time' (default)
	Score  string `json:"score"`  // molang expression, default 'q.playtime'
}
