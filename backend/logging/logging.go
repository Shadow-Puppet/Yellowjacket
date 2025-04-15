package logging

import (
	"encoding/json"
)

// TODO check that error
func PrettyJSON(obj interface{}) string {
	bytes, _ := json.MarshalIndent(obj, "\t", "\t")
	return string(bytes)
}
