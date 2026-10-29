package v3

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if s.stream == nil {
		writeError(w, http.StatusServiceUnavailable, "stream_unavailable", "event stream is unavailable")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream_unavailable", "response writer cannot stream")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events, streamErrors := s.stream.Subscribe(r.Context(), r.PathValue("id"), r.Header.Get("Last-Event-ID"))
	heartbeats := time.NewTicker(s.heartbeat)
	defer heartbeats.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case err, ok := <-streamErrors:
			if !ok {
				streamErrors = nil
				continue
			}
			if err != nil {
				s.writeSSE(w, flusher, "event: error\ndata: {\"code\":\"stream_unavailable\"}\n\n")
				return
			}
		case event, ok := <-events:
			if !ok {
				return
			}
			var encoded strings.Builder
			if event.ID != "" {
				_, _ = fmt.Fprintf(&encoded, "id: %s\n", event.ID)
			}
			if event.Type != "" {
				_, _ = fmt.Fprintf(&encoded, "event: %s\n", event.Type)
			}
			_, _ = fmt.Fprintf(&encoded, "data: %s\n\n", redactTraceJSON(event.Data))
			if !s.writeSSE(w, flusher, "%s", encoded.String()) {
				return
			}
		case <-heartbeats.C:
			if !s.writeSSE(w, flusher, ": heartbeat\n\n") {
				return
			}
		}
	}
}

func (s *Server) writeSSE(w http.ResponseWriter, flusher http.Flusher, format string, values ...any) bool {
	// A per-event deadline prevents an unread TCP peer from retaining a handler forever.
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(s.writeWait))
	_, err := fmt.Fprintf(w, format, values...)
	if err != nil {
		return false
	}
	flusher.Flush()
	return true
}
