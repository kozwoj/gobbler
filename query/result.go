package query

import (
	"encoding/json"
	"time"

	"github.com/kozwoj/gobbler-query/api"
)

const resultDatetimeFmt = "2006-01-02 15:04:05.000"

// SerializeResult converts an api.Result into a JSON array of row objects.
// Each row becomes a JSON object keyed by column name. Null cells are
// represented as JSON null. Returns []byte("[]") for an empty result.
func SerializeResult(r *api.Result) ([]byte, error) {
	rows := make([]map[string]any, len(r.Rows))
	for i, row := range r.Rows {
		obj := make(map[string]any, len(r.Schema))
		for j, meta := range r.Schema {
			null := false
			if i < len(r.Nulls) && j < len(r.Nulls[i]) {
				null = r.Nulls[i][j]
			} else if row[j] == nil {
				null = true
			}
			if null {
				obj[meta.Name] = nil
			} else {
				obj[meta.Name] = formatResultValue(row[j])
			}
		}
		rows[i] = obj
	}
	return json.Marshal(rows)
}

// formatResultValue converts Go values to JSON-friendly representations.
// time.Time  → Gobbler datetime string ("2006-01-02 15:04:05.000")
// time.Duration → Go duration string (e.g. "1h30m")
// All other types pass through unchanged (int32, int64, float64, string, bool
// are natively JSON-serialisable).
func formatResultValue(v any) any {
	switch t := v.(type) {
	case time.Time:
		return t.UTC().Format(resultDatetimeFmt)
	case time.Duration:
		return t.String()
	default:
		return v
	}
}
