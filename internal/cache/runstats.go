package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// RunStats is <root>/v1/stats.json (spec-cache §3.2), written at the end of
// every check or warm.
type RunStats struct {
	Command      string    `json:"command"`
	At           time.Time `json:"at"`
	Hits         int64     `json:"hits"`
	Misses       int64     `json:"misses"`
	Stale        int64     `json:"stale"`
	APICalls     int64     `json:"api_calls"`
	FetchedBytes int64     `json:"fetched_bytes"`
}

// StatsFile is the run-stats file name under the schema directory.
const StatsFile = "stats.json"

// WriteRunStats stores the run summary best-effort; failures are silent.
func WriteRunStats(c *Cache, rs RunStats) {
	if !active(c) {
		return
	}
	data, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return
	}
	_ = writeAtomic(filepath.Join(c.Root, SchemaVersion, StatsFile), append(data, '\n'), now(c))
}

// ReadRunStats loads the last run summary; ok is false when absent or
// corrupt.
func ReadRunStats(c *Cache) (RunStats, bool) {
	if c == nil || c.Root == "" {
		return RunStats{}, false
	}
	data, err := os.ReadFile(filepath.Join(c.Root, SchemaVersion, StatsFile))
	if err != nil {
		return RunStats{}, false
	}
	var rs RunStats
	if err := json.Unmarshal(data, &rs); err != nil || rs.Command == "" {
		return RunStats{}, false
	}
	return rs, true
}
