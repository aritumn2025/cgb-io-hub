package persona

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxResponseBody = 1 << 20 // 1 MiB

// Config collects parameters used to initialise the PersonaGo API client.
type Config struct {
	BaseURL    string
	GameName   string
	Attraction string
	Staff      string
	Timeout    time.Duration
	HTTPClient *http.Client
}

// Client wraps PersonaGo backend HTTP calls needed by the hub.
type Client struct {
	baseURL    string
	gameName   string
	attraction string
	staff      string
	httpClient *http.Client
}

// Lobby represents the current lobby occupants for a Persona game.
type Lobby struct {
	GameID string
	Slots  []Slot
}

// Slot describes a single lobby entry.
type Slot struct {
	Index       int
	SlotID      string
	UserID      string
	Name        string
	Personality string
}

// GameResult holds the score achieved by a player for a finished game.
type GameResult struct {
	Slot   int
	UserID string
	Name   string
	Score  int
}

// GameResultResponse describes the Persona API reply after submitting results.
type GameResultResponse struct {
	GameID string
	PlayID int
}

// ErrUserNotFound indicates that the requested user did not appear in the lobby.
var ErrUserNotFound = errors.New("persona: user not found in lobby")

// APIError provides access to Persona API error payloads.
type APIError struct {
	Operation string
	Status    int
	Detail    string
}

func (e *APIError) Error() string {
	op := strings.TrimSpace(e.Operation)
	if op == "" {
		op = "persona API call"
	}
	if e.Status > 0 {
		return fmt.Sprintf("persona: %s failed (status %d): %s", op, e.Status, e.Detail)
	}
	return fmt.Sprintf("persona: %s failed: %s", op, e.Detail)
}

// New constructs a PersonaGo API client from the provided configuration.
func New(cfg Config) (*Client, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		return nil, errors.New("persona: base URL required")
	}

	if _, err := url.Parse(base); err != nil {
		return nil, fmt.Errorf("persona: invalid base URL: %w", err)
	}

	gameName := strings.TrimSpace(cfg.GameName)
	if gameName == "" {
		return nil, errors.New("persona: game name required")
	}

	attraction := strings.TrimSpace(cfg.Attraction)
	if attraction == "" {
		return nil, errors.New("persona: attraction name required")
	}

	staff := strings.TrimSpace(cfg.Staff)
	if staff == "" {
		return nil, errors.New("persona: staff identifier required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	} else if timeout > 0 {
		httpClient.Timeout = timeout
	}

	return &Client{
		baseURL:    strings.TrimRight(base, "/"),
		gameName:   gameName,
		attraction: attraction,
		staff:      staff,
		httpClient: httpClient,
	}, nil
}

// FetchLobby retrieves the current lobby state from PersonaGo.
func (c *Client) FetchLobby(ctx context.Context) (*Lobby, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.buildURL("api", "games", "lobby", c.gameName), nil)
	if err != nil {
		return nil, fmt.Errorf("persona: create lobby request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("persona: lobby request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("persona: read lobby response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(string(rawBody))
		if detail == "" {
			detail = resp.Status
		}
		return nil, &APIError{
			Operation: "lobby request",
			Status:    resp.StatusCode,
			Detail:    detail,
		}
	}

	var decoded lobbyResponse
	if err := json.Unmarshal(rawBody, &decoded); err != nil {
		return nil, fmt.Errorf("persona: decode lobby response: %w", err)
	}

	return decoded.toLobby(), nil
}

// FindSlotForUser locates the slot assignment for the given user ID.
func (c *Client) FindSlotForUser(ctx context.Context, userID string) (*Slot, error) {
	lobby, err := c.FetchLobby(ctx)
	if err != nil {
		return nil, err
	}
	for _, slot := range lobby.Slots {
		if slot.UserID == userID {
			copy := slot
			return &copy, nil
		}
	}
	return nil, ErrUserNotFound
}

// RecordVisit marks that the specified user visited the configured attraction.
func (c *Client) RecordVisit(ctx context.Context, userID string) error {
	payload := struct {
		UserID string `json:"userId"`
		Staff  string `json:"staff"`
	}{
		UserID: userID,
		Staff:  c.staff,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("persona: encode visit payload: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.buildURL("api", "entry", "attraction", c.attraction, "visit"),
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("persona: create visit request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("persona: visit request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return fmt.Errorf("persona: read visit response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(string(rawBody))
		if detail == "" {
			detail = resp.Status
		}
		return &APIError{
			Operation: "visit request",
			Status:    resp.StatusCode,
			Detail:    detail,
		}
	}

	return nil
}

// ClearLobby removes the current lobby assignment for the configured game.
func (c *Client) ClearLobby(ctx context.Context) (*Lobby, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.buildURL("api", "games", "lobby", c.gameName), nil)
	if err != nil {
		return nil, fmt.Errorf("persona: create lobby delete request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("persona: lobby delete request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("persona: read lobby delete response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(string(rawBody))
		if detail == "" {
			detail = resp.Status
		}
		return nil, &APIError{
			Operation: "lobby delete request",
			Status:    resp.StatusCode,
			Detail:    detail,
		}
	}

	var decoded lobbyResponse
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &decoded); err != nil {
			return nil, fmt.Errorf("persona: decode lobby delete response: %w", err)
		}
	}

	return decoded.toLobby(), nil
}

// UpdateLobby replaces lobby entries with the provided slot assignments.
func (c *Client) UpdateLobby(ctx context.Context, slots map[int]string) (*Lobby, error) {
	payload := lobbyUpdateRequest{
		GameID: c.gameName,
		Lobby: map[string]*string{
			"1": nil,
			"2": nil,
			"3": nil,
			"4": nil,
		},
	}

	for slot, userID := range slots {
		if slot < 1 || slot > 4 {
			continue
		}
		trimmed := strings.TrimSpace(userID)
		if trimmed == "" {
			continue
		}
		value := trimmed
		payload.Lobby[strconv.Itoa(slot)] = &value
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("persona: encode lobby update payload: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.buildURL("api", "games", "lobby", c.gameName),
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("persona: create lobby update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("persona: lobby update request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("persona: read lobby update response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		detail := strings.TrimSpace(string(rawBody))
		if detail == "" {
			detail = resp.Status
		}
		return nil, &APIError{
			Operation: "lobby update request",
			Status:    resp.StatusCode,
			Detail:    detail,
		}
	}

	var decoded lobbyResponse
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &decoded); err != nil {
			return nil, fmt.Errorf("persona: decode lobby update response: %w", err)
		}
	}

	return decoded.toLobby(), nil
}

// SubmitGameResult uploads the scores for a completed match to the Persona API.
func (c *Client) SubmitGameResult(ctx context.Context, startTime time.Time, results []GameResult) (*GameResultResponse, error) {
	if len(results) == 0 {
		return nil, errors.New("persona: at least one game result required")
	}

	payload := gameResultRequest{
		Results: map[string]*gameResultSlot{
			"1": nil,
			"2": nil,
			"3": nil,
			"4": nil,
		},
	}

	if !startTime.IsZero() {
		payload.StartTime = startTime.UTC().Format(time.RFC3339)
	}

	seenSlots := make(map[int]struct{}, len(results))
	for _, res := range results {
		if res.Slot < 1 || res.Slot > 4 {
			return nil, fmt.Errorf("persona: invalid slot %d", res.Slot)
		}
		if res.UserID == "" {
			return nil, fmt.Errorf("persona: user id required for slot %d", res.Slot)
		}
		if _, exists := seenSlots[res.Slot]; exists {
			return nil, fmt.Errorf("persona: duplicate slot %d", res.Slot)
		}
		seenSlots[res.Slot] = struct{}{}
		payload.Results[strconv.Itoa(res.Slot)] = &gameResultSlot{
			UserID: res.UserID,
			Name:   res.Name,
			Score:  res.Score,
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("persona: encode game result payload: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.buildURL("api", "games", "result", c.gameName),
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("persona: create game result request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("persona: game result request: %w", err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("persona: read game result response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(string(rawBody))
		if detail == "" {
			detail = resp.Status
		}
		return nil, &APIError{
			Operation: "game result request",
			Status:    resp.StatusCode,
			Detail:    detail,
		}
	}

	var decoded gameResultResponse
	if len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, &decoded); err != nil {
			return nil, fmt.Errorf("persona: decode game result response: %w", err)
		}
	}

	return &GameResultResponse{
		GameID: decoded.GameID,
		PlayID: decoded.PlayID,
	}, nil
}

func (c *Client) buildURL(segments ...string) string {
	base := c.baseURL
	escaped := make([]string, 0, len(segments))
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(segment))
	}
	return base + "/" + strings.Join(escaped, "/")
}

type lobbyResponse struct {
	GameID string        `json:"gameId"`
	Lobby  lobbySlotsRaw `json:"lobby"`
}

type lobbySlotsRaw struct {
	Slot1 *lobbySlot `json:"1"`
	Slot2 *lobbySlot `json:"2"`
	Slot3 *lobbySlot `json:"3"`
	Slot4 *lobbySlot `json:"4"`
}

type lobbySlot struct {
	UserID      string `json:"id"`
	Name        string `json:"name"`
	Personality string `json:"personality"`
}

func (resp lobbyResponse) toLobby() *Lobby {
	slots := make([]Slot, 0, 4)

	appendSlot := func(index int, raw *lobbySlot) {
		if raw == nil {
			return
		}
		slotID := fmt.Sprintf("p%d", index)
		slots = append(slots, Slot{
			Index:       index,
			SlotID:      slotID,
			UserID:      raw.UserID,
			Name:        raw.Name,
			Personality: raw.Personality,
		})
	}

	appendSlot(1, resp.Lobby.Slot1)
	appendSlot(2, resp.Lobby.Slot2)
	appendSlot(3, resp.Lobby.Slot3)
	appendSlot(4, resp.Lobby.Slot4)

	return &Lobby{
		GameID: resp.GameID,
		Slots:  slots,
	}
}

type gameResultRequest struct {
	StartTime string                     `json:"startTime,omitempty"`
	Results   map[string]*gameResultSlot `json:"results"`
}

type gameResultSlot struct {
	UserID string `json:"id"`
	Name   string `json:"name"`
	Score  int    `json:"score"`
}

type gameResultResponse struct {
	GameID string `json:"gameId"`
	PlayID int    `json:"playId"`
}

type lobbyUpdateRequest struct {
	GameID string             `json:"gameId"`
	Lobby  map[string]*string `json:"lobby"`
}
