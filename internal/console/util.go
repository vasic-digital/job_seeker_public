package console

import (
	"database/sql"
	"encoding/json"
	"sort"
)

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
func jsonMarshal(v any) ([]byte, error)   { return json.Marshal(v) }

func nullable(n sql.NullInt64) any {
	if n.Valid {
		return n.Int64
	}
	return nil
}

func sortedKeys(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
