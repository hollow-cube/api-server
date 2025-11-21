package legacy

import (
	_ "embed"
	"encoding/json"
)

//go:embed items.json
var rawLegacyItemMap []byte
var legacyItemMap map[string]string

func init() {
	err := json.Unmarshal(rawLegacyItemMap, &legacyItemMap)
	if err != nil {
		panic(err)
	}
}

func ConvertItem(legacyItemId string) (string, bool) {
	itemId, ok := legacyItemMap[legacyItemId]
	return itemId, ok
}
