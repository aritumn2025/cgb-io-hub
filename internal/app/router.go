package app

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aritumn2025/cgb-io-hub/internal/hub"
	"github.com/aritumn2025/cgb-io-hub/internal/persona"
)

func (a *App) buildRouter(assets http.FileSystem) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.Handle("/ws", http.HandlerFunc(a.hub.HandleWS))
	mux.HandleFunc("/api/controller/session", a.controllerSessionHandler)
	mux.HandleFunc("/api/controller/assignments", a.controllerAssignmentsHandler)
	mux.HandleFunc("/api/game/lobby", a.gameLobbyHandler)
	mux.HandleFunc("/api/game/start", a.gameStartHandler)
	mux.HandleFunc("/api/game/result", a.gameResultHandler)
	mux.Handle("/", http.FileServer(assets))
	return mux
}

func (a *App) controllerSessionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.persona == nil {
		a.respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "persona integration disabled",
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req struct {
		UserID string `json:"userId"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "request body required"})
			return
		}
		a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}
	if err := decoder.Decode(new(struct{})); err != io.EOF {
		a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "unexpected trailing content"})
		return
	}

	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "userId is required"})
		return
	}

	slot, err := a.persona.FindSlotForUser(r.Context(), userID)
	if err != nil {
		if errors.Is(err, persona.ErrUserNotFound) {
			a.respondJSON(w, http.StatusNotFound, map[string]string{"error": "user not present in lobby"})
			return
		}
		var apiErr *persona.APIError
		if errors.As(err, &apiErr) {
			a.logErrorWithStack(
				"persona_lookup_failed",
				"user_id", userID,
				"status", apiErr.Status,
				"detail", apiErr.Detail,
				"err", err.Error(),
			)
		} else {
			a.logErrorWithStack("persona_lookup_failed", "user_id", userID, "err", err.Error())
		}
		a.respondJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to verify user lobby assignment"})
		return
	}

	token, expiresAt, err := a.hub.IssueControllerToken(
		slot.SlotID,
		slot.UserID,
		slot.Name,
		slot.Personality,
		a.cfg.SessionTokenTTL,
	)
	if err != nil {
		a.logErrorWithStack("token_issue_failed", "slot", slot.SlotID, "user_id", slot.UserID, "err", err.Error())
		a.respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to issue controller token"})
		return
	}

	ttlSeconds := int(time.Until(expiresAt).Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = int(a.cfg.SessionTokenTTL.Seconds())
		if ttlSeconds < 1 {
			ttlSeconds = 60
		}
	}

	a.respondJSON(w, http.StatusCreated, map[string]any{
		"slotId":    slot.SlotID,
		"token":     token,
		"ttl":       ttlSeconds,
		"expiresAt": expiresAt.UTC().Format(time.RFC3339),
		"user": map[string]string{
			"id":          slot.UserID,
			"name":        slot.Name,
			"personality": slot.Personality,
		},
		"gameId": a.cfg.GameID,
	})
}

func (a *App) controllerAssignmentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	assignments := a.hub.ControllerAssignments()
	type assignmentResponse struct {
		SlotID         string  `json:"slotId"`
		UserID         string  `json:"userId,omitempty"`
		Name           string  `json:"name,omitempty"`
		Personality    string  `json:"personality,omitempty"`
		Connected      bool    `json:"connected"`
		LastSeen       *string `json:"lastSeen,omitempty"`
		TokenExpiresAt *string `json:"tokenExpiresAt,omitempty"`
	}

	responses := make([]assignmentResponse, 0, len(assignments))
	for _, record := range assignments {
		resp := assignmentResponse{
			SlotID:      record.SlotID,
			UserID:      record.UserID,
			Name:        record.Name,
			Personality: record.Personality,
			Connected:   record.Connected,
		}
		if !record.LastSeen.IsZero() {
			lastSeen := record.LastSeen.UTC().Format(time.RFC3339)
			resp.LastSeen = &lastSeen
		}
		if !record.TokenExpiresAt.IsZero() {
			expires := record.TokenExpiresAt.UTC().Format(time.RFC3339)
			resp.TokenExpiresAt = &expires
		}
		responses = append(responses, resp)
	}

	a.respondJSON(w, http.StatusOK, map[string]any{
		"assignments": responses,
	})
}

func (a *App) gameStartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.persona == nil {
		a.respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "persona integration disabled",
		})
		return
	}

	var req struct {
		Slots []string `json:"slots"`
	}

	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		defer r.Body.Close()

		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			if !errors.Is(err, io.EOF) {
				a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
				return
			}
		} else if err := decoder.Decode(new(struct{})); err != io.EOF {
			a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "unexpected trailing content"})
			return
		}
	}

	assignments := a.hub.ControllerAssignments()
	index := make(map[string]hub.ControllerAssignment, len(assignments))
	for _, rec := range assignments {
		index[rec.SlotID] = rec
	}

	targetSlots := make([]string, 0)
	if len(req.Slots) > 0 {
		seen := make(map[string]struct{})
		for _, raw := range req.Slots {
			slotID := strings.ToLower(strings.TrimSpace(raw))
			if slotID == "" {
				continue
			}
			if _, exists := seen[slotID]; exists {
				continue
			}
			if _, ok := index[slotID]; !ok {
				a.respondJSON(w, http.StatusNotFound, map[string]string{"error": "slot not found: " + slotID})
				return
			}
			seen[slotID] = struct{}{}
			targetSlots = append(targetSlots, slotID)
		}
	} else {
		for slotID, rec := range index {
			if rec.Connected && rec.UserID != "" {
				targetSlots = append(targetSlots, slotID)
			}
		}
	}

	if len(targetSlots) == 0 {
		a.respondJSON(w, http.StatusOK, map[string]any{
			"gameId":  a.cfg.GameID,
			"marked":  []any{},
			"skipped": []any{},
			"message": "no eligible players to mark",
		})
		return
	}

	sort.Strings(targetSlots)

	type visitResult struct {
		SlotID string `json:"slotId"`
		UserID string `json:"userId"`
	}

	results := make([]visitResult, 0, len(targetSlots))
	skipped := make([]string, 0)
	for _, slotID := range targetSlots {
		rec := index[slotID]
		if rec.UserID == "" {
			skipped = append(skipped, slotID)
			continue
		}

		if err := a.persona.RecordVisit(r.Context(), rec.UserID); err != nil {
			a.logger.Error("persona_visit_failed", "slot", slotID, "user_id", rec.UserID, "err", err.Error())
			a.respondJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to mark visit for slot " + slotID})
			return
		}

		results = append(results, visitResult{
			SlotID: slotID,
			UserID: rec.UserID,
		})
	}

	a.respondJSON(w, http.StatusOK, map[string]any{
		"gameId":  a.cfg.GameID,
		"marked":  results,
		"count":   len(results),
		"slots":   targetSlots,
		"skipped": skipped,
	})
}

func (a *App) gameLobbyHandler(w http.ResponseWriter, r *http.Request) {
	if a.persona == nil {
		a.respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "persona integration disabled",
		})
		return
	}

	switch r.Method {
	case http.MethodGet:
		lobby, err := a.persona.FetchLobby(r.Context())
		if err != nil {
			a.logger.Error("persona_lobby_fetch_failed", "err", err.Error())
			a.respondJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to fetch lobby"})
			return
		}
		a.respondJSON(w, http.StatusOK, lobbyResponsePayload(lobby))

	case http.MethodPost:
		if r.Body == nil {
			a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "request body required"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		defer r.Body.Close()

		var req struct {
			GameID string             `json:"gameId"`
			Lobby  map[string]*string `json:"lobby"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "request body required"})
				return
			}
			a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
			return
		}
		if err := decoder.Decode(new(struct{})); err != io.EOF {
			a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "unexpected trailing content"})
			return
		}

		if len(req.Lobby) == 0 {
			a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "lobby mapping required"})
			return
		}

		slots := make(map[int]string, len(req.Lobby))
		for key, value := range req.Lobby {
			_, slotNum, ok := normalizeSlotID("p" + key)
			if !ok {
				a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid slot key: " + key})
				return
			}
			if value == nil {
				continue
			}
			slots[slotNum] = *value
		}

		lobby, err := a.persona.UpdateLobby(r.Context(), slots)
		if err != nil {
			a.logger.Error("persona_lobby_update_failed", "err", err.Error())
			a.respondJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to update lobby"})
			return
		}

		a.respondJSON(w, http.StatusOK, lobbyResponsePayload(lobby))

	case http.MethodDelete:
		lobby, err := a.persona.ClearLobby(r.Context())
		if err != nil {
			a.logger.Error("persona_lobby_delete_failed", "err", err.Error())
			a.respondJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to clear lobby"})
			return
		}
		a.respondJSON(w, http.StatusOK, lobbyResponsePayload(lobby))

	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost, http.MethodDelete}, ", "))
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *App) gameResultHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if a.persona == nil {
		a.respondJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "persona integration disabled",
		})
		return
	}

	if r.Body == nil {
		a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "request body required"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req struct {
		StartTime string `json:"startTime"`
		Results   []struct {
			SlotID string `json:"slotId"`
			Score  int    `json:"score"`
			Name   string `json:"name"`
		} `json:"results"`
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "request body required"})
			return
		}
		a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}
	if err := decoder.Decode(new(struct{})); err != io.EOF {
		a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "unexpected trailing content"})
		return
	}

	if len(req.Results) == 0 {
		a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "results array required"})
		return
	}

	assignments := a.hub.ControllerAssignments()
	index := make(map[string]hub.ControllerAssignment, len(assignments))
	for _, rec := range assignments {
		slot := strings.ToLower(strings.TrimSpace(rec.SlotID))
		if slot == "" {
			continue
		}
		index[slot] = rec
	}

	submissions := make([]persona.GameResult, 0, len(req.Results))
	seen := make(map[int]string, len(req.Results))

	for _, entry := range req.Results {
		slotRaw := strings.TrimSpace(entry.SlotID)
		if slotRaw == "" {
			a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "slotId is required"})
			return
		}

		slotKey, slotNum, ok := normalizeSlotID(slotRaw)
		if !ok {
			a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid slotId: " + slotRaw})
			return
		}
		if _, exists := seen[slotNum]; exists {
			a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "duplicate slotId: " + slotKey})
			return
		}
		seen[slotNum] = slotKey

		assign, ok := index[slotKey]
		if !ok || strings.TrimSpace(assign.UserID) == "" {
			a.respondJSON(w, http.StatusNotFound, map[string]string{"error": "slot not assigned to user: " + slotKey})
			return
		}

		if entry.Score < 0 {
			a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "score must be non-negative"})
			return
		}

		name := strings.TrimSpace(entry.Name)
		if name == "" {
			name = strings.TrimSpace(assign.Name)
		}

		submissions = append(submissions, persona.GameResult{
			Slot:   slotNum,
			UserID: assign.UserID,
			Name:   name,
			Score:  entry.Score,
		})
	}

	if len(submissions) == 0 {
		a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "no valid results provided"})
		return
	}

	startTime := time.Now().UTC()
	if raw := strings.TrimSpace(req.StartTime); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			a.respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid startTime"})
			return
		}
		startTime = parsed
	}

	resp, err := a.persona.SubmitGameResult(r.Context(), startTime, submissions)
	if err != nil {
		var apiErr *persona.APIError
		if errors.As(err, &apiErr) {
			a.logErrorWithStack(
				"persona_result_failed",
				"status", apiErr.Status,
				"detail", apiErr.Detail,
				"err", err.Error(),
			)
		} else {
			a.logErrorWithStack("persona_result_failed", "err", err.Error())
		}
		a.respondJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to submit game results"})
		return
	}

	a.respondJSON(w, http.StatusOK, map[string]any{
		"gameId":    resp.GameID,
		"playId":    resp.PlayID,
		"submitted": len(submissions),
		"startTime": startTime.UTC().Format(time.RFC3339),
	})
}

func normalizeSlotID(raw string) (string, int, bool) {
	slot := strings.ToLower(strings.TrimSpace(raw))
	if slot == "" {
		return "", 0, false
	}
	if strings.HasPrefix(slot, "p") {
		slot = strings.TrimPrefix(slot, "p")
	}
	num, err := strconv.Atoi(slot)
	if err != nil || num < 1 || num > 4 {
		return "", 0, false
	}
	return "p" + strconv.Itoa(num), num, true
}

func lobbyResponsePayload(lobby *persona.Lobby) map[string]any {
	gameID := ""
	if lobby != nil {
		gameID = lobby.GameID
	}

	response := map[string]any{
		"gameId": gameID,
		"lobby":  map[string]any{"1": nil, "2": nil, "3": nil, "4": nil},
	}

	if lobby == nil {
		return response
	}

	payloadLobby := response["lobby"].(map[string]any)
	for _, slot := range lobby.Slots {
		entry := map[string]string{
			"id":          slot.UserID,
			"name":        slot.Name,
			"personality": slot.Personality,
		}
		payloadLobby[strconv.Itoa(slot.Index)] = entry
	}

	return response
}

func (a *App) respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		a.logger.Error("http_response_encode_error", "status", status, "err", err.Error())
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &responseLogger{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lrw, r)
		duration := time.Since(start)
		logger.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", lrw.status,
			"duration_ms", duration.Milliseconds(),
			"remote_ip", requestIP(r),
		)
	})
}

type responseLogger struct {
	http.ResponseWriter
	status int
}

func (r *responseLogger) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseLogger) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("http.Hijacker not supported")
	}
	return hj.Hijack()
}

func requestIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			candidate := strings.TrimSpace(part)
			if candidate != "" {
				return candidate
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
