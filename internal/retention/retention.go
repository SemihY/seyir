package retention

import (
	"log"
	"seyir/internal/db"
	"sync"
	"time"
)

var (
	retentionDays = 7 // default retention
	retentionMu   sync.RWMutex
)

// RetentionCleaner runs periodically to remove old logs from DuckDB
func RetentionCleaner(database *db.DB, defaultDays int) {
	SetRetentionDays(defaultDays)
	for {
		retentionMu.RLock()
		days := retentionDays
		retentionMu.RUnlock()

		cutoff := time.Now().AddDate(0, 0, -days)
		_, err := database.Exec(`DELETE FROM logs WHERE ts < ?`, cutoff)
		if err != nil {
			log.Printf("[ERROR] RetentionCleaner failed: %v", err)
		}
		time.Sleep(24 * time.Hour)
	}
}

// SetRetentionDays safely updates the retention policy
func SetRetentionDays(days int) {
	retentionMu.Lock()
	retentionDays = days
	retentionMu.Unlock()
}

// GetRetentionDays safely returns the current retention policy
func GetRetentionDays() int {
	retentionMu.RLock()
	defer retentionMu.RUnlock()
	return retentionDays
}
