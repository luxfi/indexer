package explorer

import (
	"strconv"
)

// Coercion for values scanned out of SQLite, whose driver returns int64,
// float64 or string for the same column depending on declared affinity.

func toBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case int64:
		return b != 0
	case float64:
		return b != 0
	case int:
		return b != 0
	default:
		return false
	}
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}
