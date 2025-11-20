package model

type SchematicHeader struct {
	Name       string
	Size       int
	Dimensions int64 // XYZ encoded in int
	FileType   string
}
