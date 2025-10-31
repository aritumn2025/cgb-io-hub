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
