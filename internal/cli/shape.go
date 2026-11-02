package cli

import (
	"encoding/json"
	"fmt"

	"github.com/bilhasry/saringan/internal/source"
)

// shapeOf describes the structure of a fetched document, e.g.
// "array[3] of object{4}" or "object{2}".
func shapeOf(v any) string {
	switch t := v.(type) {
	case []any:
		if len(t) == 0 {
			return "array[0]"
		}
		return fmt.Sprintf("array[%d] of %s", len(t), shapeOf(t[0]))
	case map[string]any:
		return fmt.Sprintf("object{%d}", len(t))
	case string:
		return "string"
	case float64, int, int64:
		return "number"
	case bool:
		return "bool"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// previewOf returns a short printable preview of a document.
func previewOf(doc *source.Document) string {
	if doc.IsStructured() {
		b, err := json.Marshal(doc.Data)
		if err != nil {
			return ""
		}
		return string(b)
	}
	return doc.Raw
}
