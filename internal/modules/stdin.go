package modules

import (
	"bufio"
	"os"
	"strings"

	"logspot/internal/core"
)

func CaptureStdin(db *core.DB, source string) {
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		if source == "" { source = "stdin" }
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := scanner.Text()
			level := core.INFO
			switch {
			case strings.HasPrefix(line,"[ERROR]"): level = core.ERROR
			case strings.HasPrefix(line,"[WARN]"):  level = core.WARN
			case strings.HasPrefix(line,"[DEBUG]"): level = core.DEBUG
			}
			entry := core.NewLogEntry(source, level, line)
			core.SaveLog(db, entry)
			core.BroadcastLog(entry)
		}
	}
}
