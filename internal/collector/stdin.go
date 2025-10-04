package collector

import (
	"bufio"
	"logspot/internal/db"
	"logspot/internal/tail"
	"os"
	"strings"
)

func CaptureStdin(database *db.DB, source string) {
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		if source == "" { source = "stdin" }
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := scanner.Text()
			level := db.INFO
			switch {
			case strings.HasPrefix(line,"[ERROR]"): level = db.ERROR
			case strings.HasPrefix(line,"[WARN]"):  level = db.WARN
			case strings.HasPrefix(line,"[DEBUG]"): level = db.DEBUG
			}
			entry := db.NewLogEntry(source, level, line)
			db.SaveLog(database, entry)
			tail.BroadcastLog(entry)
		}
	}
}
