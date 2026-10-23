package web

import (
	"net/http"
	"strconv"
)

// queryFloat parses a float query parameter, clamped to [min, max], falling
// back to def when absent or unparseable.
func queryFloat(r *http.Request, name string, def, min, max float64) float64 {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return clamp(f, min, max)
}

// queryInt parses an int query parameter, clamped to [min, max], falling back
// to def when absent or unparseable.
func queryInt(r *http.Request, name string, def, min, max int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
