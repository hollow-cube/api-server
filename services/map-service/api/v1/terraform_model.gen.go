// Code generated with openapi-go DO NOT EDIT.
package v1

type TFPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type SchematicHeader struct {
	Name       string   `json:"name"`
	Size       float64  `json:"size"`
	Dimensions *TFPoint `json:"dimensions"`
	FileType   *string  `json:"fileType"`
}

type ListPlayerSchematicsResponse []*SchematicHeader

type UpdateSchematicHeaderRequest struct {
	Dimensions *TFPoint `json:"dimensions"`
	FileType   *string  `json:"fileType"`
}
