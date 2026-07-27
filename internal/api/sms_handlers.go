package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/austinkregel/backup-server/internal/ws"
)

// These handlers expose SMS history (read) and send (write) for phone-class
// agents. Thread/message listing reads straight from the local encrypted
// SMSStore — it's kept in sync by the relay as messages arrive/are sent, so
// there's no need to round-trip to the agent on every page load (unlike
// send, which has no local equivalent to fall back to).

func handleSMSThreads(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.SMS == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "sms history unavailable (no database configured)"})
			return
		}
		clientID := chi.URLParam(r, "clientId")
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}

		threads, err := deps.SMS.ListThreads(clientID, limit)
		if err != nil {
			deps.Log.Error("list sms threads failed", "clientId", clientID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to list threads"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
	}
}

func handleSMSMessages(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.SMS == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "sms history unavailable (no database configured)"})
			return
		}
		clientID := chi.URLParam(r, "clientId")
		threadIDStr := chi.URLParam(r, "threadId")
		threadID64, err := strconv.ParseUint(threadIDStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid threadId"})
			return
		}

		limit := 200
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}

		messages, err := deps.SMS.ListMessages(clientID, uint(threadID64), limit)
		if err != nil {
			deps.Log.Error("list sms messages failed", "clientId", clientID, "threadId", threadIDStr, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to list messages"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": messages})
	}
}

// handleSMSSend forwards a send request to the agent's companion app and, on
// success, persists the sent message so it's immediately visible in history
// (the relay's sms_send_result case only resolves the pending request — it
// doesn't have the message body, only this handler does).
func handleSMSSend(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientID := chi.URLParam(r, "clientId")
		if !deps.Store.HasClient(clientID) {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "client offline"})
			return
		}

		var body struct {
			To   string `json:"to"`
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.To == "" || body.Body == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "to and body are required"})
			return
		}

		token := fmt.Sprintf("sms-send-%s-%d", clientID, time.Now().UnixMilli())
		ch := deps.Relay.RegisterGenericPending(token)
		defer deps.Relay.UnregisterGenericPending(token)

		ws.SendSignedCommand(deps.Store, clientID, "sms_send", map[string]any{
			"token": token, "to": body.To, "body": body.Body,
		}, deps.Log)

		select {
		case result := <-ch:
			if errMsg, _ := result["error"].(string); errMsg != "" {
				writeJSON(w, http.StatusBadGateway, map[string]any{"error": errMsg})
				return
			}
			status, _ := result["status"].(string)
			if status == "" {
				status = "sent"
			}
			messageID, _ := result["messageId"].(string)

			resp := persistSentSMS(deps, clientID, body.To, body.Body, status, messageID)
			writeJSON(w, http.StatusOK, resp)
		case <-time.After(20 * time.Second):
			writeJSON(w, http.StatusGatewayTimeout, map[string]any{"error": "timeout waiting for sms send result"})
		case <-r.Context().Done():
			return
		}
	}
}

// persistSentSMS records a successfully-sent message in local history and
// returns the REST response body. Persistence failures are logged, not
// surfaced as an error — the send itself already succeeded on the phone, so
// this is best-effort bookkeeping, not something that should turn a
// successful send into an HTTP error.
func persistSentSMS(deps Deps, clientID, to, body, status, messageID string) map[string]any {
	resp := map[string]any{"to": to, "status": status}
	if deps.SMS == nil {
		return resp
	}

	threadID, err := deps.SMS.UpsertThread(clientID, to, body, time.Now().UTC(), false)
	if err != nil {
		deps.Log.Warn("failed to upsert sms thread after send", "clientId", clientID, "error", err)
		return resp
	}
	resp["threadId"] = threadID

	if messageID != "" {
		if _, err := deps.SMS.InsertMessage(clientID, threadID, messageID, to, "out", body, status, time.Now().UTC()); err != nil {
			deps.Log.Warn("failed to persist sent sms", "clientId", clientID, "error", err)
		} else {
			resp["messageId"] = messageID
		}
	}
	return resp
}
