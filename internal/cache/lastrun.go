package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/najemi-software/jobsworth/internal/finding"
)

// RunRecord is what `check` leaves behind for `explain --last`
// (spec-explain §5): the entries, the findings in printed order, the config
// path, and the flags. Stored at <root>/v1/last/<hash of cwd>.json.
type RunRecord struct {
	At        time.Time           `json:"at"`
	Completed bool                `json:"completed"`
	Entries   []string            `json:"entries"`
	Findings  []RunFinding        `json:"findings"`
	Config    string              `json:"config,omitempty"`
	Flags     map[string][]string `json:"flags,omitempty"`
}

// RunFinding is one printed finding with the entry it belonged to.
type RunFinding struct {
	Entry   string          `json:"entry"`
	Finding finding.Finding `json:"finding"`
}

// LastDir is the directory under the schema version holding run records.
const LastDir = "last"

func lastPath(c *Cache, cwd string) string {
	sum := sha256.Sum256([]byte(cwd))
	return filepath.Join(c.Root, SchemaVersion, LastDir, hex.EncodeToString(sum[:8])+".json")
}

// WriteLastRun stores the record best-effort; failures are silent.
func WriteLastRun(c *Cache, cwd string, rec RunRecord) {
	if c == nil || c.Root == "" {
		return
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return
	}
	_ = writeAtomic(lastPath(c, cwd), append(data, '\n'), now(c))
}

// ReadLastRun loads the record for cwd; ok is false when absent or corrupt.
func ReadLastRun(c *Cache, cwd string) (RunRecord, bool) {
	if c == nil || c.Root == "" {
		return RunRecord{}, false
	}
	data, err := os.ReadFile(lastPath(c, cwd))
	if err != nil {
		return RunRecord{}, false
	}
	var rec RunRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return RunRecord{}, false
	}
	return rec, true
}
