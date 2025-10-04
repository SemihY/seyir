package tail

import (
	"encoding/json"
	"fmt"
	"logspot/internal/db"
	"net/http"
)

var clients = map[chan string]bool{}

func BroadcastLog(e *db.LogEntry) {
	msg, _ := json.Marshal(e)
	for ch := range clients {
		select { case ch <- string(msg): default: }
	}
}

func SSEHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type","text/event-stream")
	w.Header().Set("Cache-Control","no-cache")
	w.Header().Set("Connection","keep-alive")

	ch := make(chan string)
	clients[ch] = true
	defer func(){ delete(clients,ch); close(ch) }()

	notify := w.(http.CloseNotifier).CloseNotify()
	for {
		select {
		case <-notify: return
		case msg := <-ch:
			fmt.Fprintf(w,"data: %s\n\n", msg)
			if f, ok := w.(http.Flusher); ok { f.Flush() }
		}
	}
}
