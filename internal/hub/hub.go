package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

const (
	roleGame       = "game"
	roleController = "controller"
)

var controllerIDPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// Config collects tunable parameters for Hub behaviour.
type Config struct {
	AllowedOrigins  []string
	MaxControllers  int
	RelayQueueSize  int
	RegisterTimeout time.Duration
	WriteTimeout    time.Duration
}

// Hub coordinator for controller and game WebSocket connections.
type Hub struct {
	cfg Config
	log *slog.Logger

	mu          sync.Mutex
	controllers map[string]*controllerSession
	game        *gameSession
}

// New creates a Hub with sane defaults applied to the provided Config.
func New(cfg Config, logger *slog.Logger) *Hub {
	if cfg.MaxControllers <= 0 {
		cfg.MaxControllers = 4
	}
	if cfg.RelayQueueSize <= 0 {
		cfg.RelayQueueSize = 128
	}
	if cfg.RegisterTimeout <= 0 {
		cfg.RegisterTimeout = 5 * time.Second
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 2 * time.Second
	}
	if len(cfg.AllowedOrigins) == 1 && cfg.AllowedOrigins[0] == "*" {
		cfg.AllowedOrigins = nil
	}

	return &Hub{
		cfg:         cfg,
		log:         logger,
		controllers: make(map[string]*controllerSession),
	}
}

// HandleWS upgrades HTTP connections to WebSocket and manages session lifecycles.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	remote := remoteAddr(r)

	opts := &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionDisabled,
	}
	if len(h.cfg.AllowedOrigins) > 0 {
		opts.OriginPatterns = h.cfg.AllowedOrigins
	}

	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		h.log.Error("ws_accept_failed", "role", "", "id", "", "remote_ip", remote, "err", err.Error())
		return
	}

	status := websocket.StatusNormalClosure
	reason := statusText(status)
	defer func() {
		_ = conn.Close(status, reason)
	}()

	ctx := r.Context()
	reg, regErrStatus, regErrReason := h.readRegister(ctx, conn, remote)
	if regErrStatus != 0 {
		status = regErrStatus
		reason = regErrReason
		return
	}

	switch reg.Role {
	case roleGame:
		status, reason = h.handleGame(ctx, conn, remote)
	case roleController:
		status, reason = h.handleController(ctx, conn, remote, reg.ID)
	default:
		status = websocket.StatusPolicyViolation
		reason = "invalid role"
		h.log.Warn("register_invalid_role", "role", reg.Role, "id", reg.ID, "remote_ip", remote)
	}

	if reason == "" {
		reason = statusText(status)
	}
}

// Shutdown requests a graceful close of active sessions.
func (h *Hub) Shutdown(ctx context.Context) {
	h.mu.Lock()
	game := h.game
	controllers := make([]*controllerSession, 0, len(h.controllers))
	for _, c := range h.controllers {
		controllers = append(controllers, c)
	}
	h.game = nil
	h.controllers = make(map[string]*controllerSession)
	h.mu.Unlock()

	if game != nil {
		game.close(websocket.StatusNormalClosure, "server shutdown")
	}
	for _, c := range controllers {
		_ = c.conn.Close(websocket.StatusNormalClosure, "server shutdown")
	}

	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
	}
}

type registerPayload struct {
	Role string `json:"role"`
	ID   string `json:"id,omitempty"`
}

func (h *Hub) readRegister(ctx context.Context, conn *websocket.Conn, remote string) (registerPayload, websocket.StatusCode, string) {
	ctx, cancel := context.WithTimeout(ctx, h.cfg.RegisterTimeout)
	defer cancel()

	msgType, data, err := conn.Read(ctx)
	if err != nil {
		status, reason := closeStatusFromError(err, websocket.StatusPolicyViolation)
		h.log.Warn("register_read_failed", "role", "", "id", "", "remote_ip", remote, "err", err.Error())
		return registerPayload{}, status, reason
	}

	if msgType != websocket.MessageText {
		h.log.Warn("register_invalid_type", "role", "", "id", "", "remote_ip", remote)
		return registerPayload{}, websocket.StatusUnsupportedData, "text frame required"
	}

	var payload registerPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		h.log.Warn("register_invalid_json", "role", "", "id", "", "remote_ip", remote, "err", err.Error())
		return registerPayload{}, websocket.StatusPolicyViolation, "invalid register payload"
	}

	payload.Role = strings.ToLower(strings.TrimSpace(payload.Role))
	payload.ID = strings.ToLower(strings.TrimSpace(payload.ID))

	if payload.Role == roleController {
		if payload.ID == "" {
			h.log.Warn("register_missing_id", "role", roleController, "id", "", "remote_ip", remote)
			return registerPayload{}, websocket.StatusPolicyViolation, "controller id required"
		}
		if !controllerIDPattern.MatchString(payload.ID) {
			h.log.Warn("register_invalid_id", "role", roleController, "id", payload.ID, "remote_ip", remote)
			return registerPayload{}, websocket.StatusPolicyViolation, "invalid controller id"
		}
	}

	return payload, 0, ""
}

func (h *Hub) handleGame(ctx context.Context, conn *websocket.Conn, remote string) (websocket.StatusCode, string) {
	session := newGameSession(ctx, conn, remote, h.cfg.RelayQueueSize, h.cfg.WriteTimeout, h.log)

	h.mu.Lock()
	previous := h.game
	h.game = session
	h.mu.Unlock()

	if previous != nil {
		previous.close(websocket.StatusPolicyViolation, "game replaced")
	}

	session.logger.Info("connected")
	session.startWriter()

	status := websocket.StatusNormalClosure
	reason := statusText(status)

	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			status, reason = closeStatusFromError(err, websocket.StatusNormalClosure)
			if !errors.Is(err, context.Canceled) {
				session.logger.Info("disconnected", "status", status, "reason", reason, "err", err.Error())
			} else {
				session.logger.Info("disconnected", "status", status, "reason", reason)
			}
			break
		}
	}

	h.mu.Lock()
	if h.game == session {
		h.game = nil
	}
	h.mu.Unlock()

	session.close(status, reason)

	return status, reason
}

func (h *Hub) handleController(ctx context.Context, conn *websocket.Conn, remote, controllerID string) (websocket.StatusCode, string) {
	session := newControllerSession(conn, controllerID, remote, h.log)

	replaced, err := h.addController(session)
	if err != nil {
		session.logger.Warn("rejected", "reason", err.Error())
		return websocket.StatusPolicyViolation, err.Error()
	}

	if replaced != nil {
		_ = replaced.conn.Close(websocket.StatusPolicyViolation, "controller replaced")
	}

	session.logger.Info("connected")

	status := websocket.StatusNormalClosure
	reason := statusText(status)

	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			status, reason = closeStatusFromError(err, websocket.StatusNormalClosure)
			break
		}
		if msgType != websocket.MessageText {
			status = websocket.StatusUnsupportedData
			reason = "text frame required"
			break
		}

		if err := h.processControllerMessage(session, data); err != nil {
			session.logger.Warn("payload_invalid", "err", err.Error())
			status = websocket.StatusPolicyViolation
			reason = err.Error()
			break
		}
	}

	h.removeController(controllerID, session)
	session.logger.Info("disconnected", "status", status, "reason", reason)

	return status, reason
}

func (h *Hub) processControllerMessage(session *controllerSession, payload []byte) error {
	var brief struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &brief); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}
	if brief.ID != "" && brief.ID != session.id {
		return fmt.Errorf("id mismatch")
	}

	session.touch()
	h.forwardToGame(payload, session)
	return nil
}

func (h *Hub) forwardToGame(payload []byte, controller *controllerSession) {
	h.mu.Lock()
	game := h.game
	h.mu.Unlock()

	if game == nil {
		return
	}

	game.enqueue(payload, controller.id)
}

func (h *Hub) addController(session *controllerSession) (*controllerSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existing := h.controllers[session.id]; existing != nil {
		h.controllers[session.id] = session
		return existing, nil
	}

	if len(h.controllers) >= h.cfg.MaxControllers {
		return nil, fmt.Errorf("controller limit reached")
	}

	h.controllers[session.id] = session
	return nil, nil
}

func (h *Hub) removeController(id string, session *controllerSession) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if current, ok := h.controllers[id]; ok && current == session {
		delete(h.controllers, id)
	}
}

type controllerSession struct {
	id        string
	conn      *websocket.Conn
	remoteIP  string
	lastSeen  time.Time
	logger    *slog.Logger
	lastSeenM sync.Mutex
}

func newControllerSession(conn *websocket.Conn, id, remote string, logger *slog.Logger) *controllerSession {
	return &controllerSession{
		id:       id,
		conn:     conn,
		remoteIP: remote,
		lastSeen: time.Now(),
		logger:   logger.With("role", roleController, "id", id, "remote_ip", remote),
	}
}

func (c *controllerSession) touch() {
	c.lastSeenM.Lock()
	c.lastSeen = time.Now()
	c.lastSeenM.Unlock()
}

type gameSession struct {
	conn         *websocket.Conn
	remoteIP     string
	send         chan []byte
	ctx          context.Context
	cancel       context.CancelFunc
	writeTimeout time.Duration
	logger       *slog.Logger
	closeOnce    sync.Once
}

func newGameSession(ctx context.Context, conn *websocket.Conn, remote string, queueSize int, writeTimeout time.Duration, logger *slog.Logger) *gameSession {
	if queueSize <= 0 {
		queueSize = 32
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	return &gameSession{
		conn:         conn,
		remoteIP:     remote,
		send:         make(chan []byte, queueSize),
		ctx:          sessionCtx,
		cancel:       cancel,
		writeTimeout: writeTimeout,
		logger:       logger.With("role", roleGame, "id", "", "remote_ip", remote),
	}
}

func (g *gameSession) startWriter() {
	go func() {
		for {
			select {
			case <-g.ctx.Done():
				return
			case msg, ok := <-g.send:
				if !ok {
					return
				}
				writeCtx, cancel := context.WithTimeout(g.ctx, g.writeTimeout)
				err := g.conn.Write(writeCtx, websocket.MessageText, msg)
				cancel()
				if err != nil {
					g.logger.Error("write_failed", "err", err.Error())
					g.close(websocket.StatusInternalError, "relay failed")
					return
				}
			}
		}
	}()
}

func (g *gameSession) enqueue(payload []byte, controllerID string) {
	data := cloneBytes(payload)
	select {
	case g.send <- data:
		return
	default:
	}

	select {
	case <-g.send:
		g.logger.Warn("queue_drop_oldest", "controller_id", controllerID)
	default:
	}

	select {
	case g.send <- data:
	default:
		g.logger.Warn("queue_drop_latest", "controller_id", controllerID)
	}
}

func (g *gameSession) close(status websocket.StatusCode, reason string) {
	g.closeOnce.Do(func() {
		g.cancel()
		close(g.send)
		_ = g.conn.Close(status, reason)
	})
}

func remoteAddr(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, p := range strings.Split(xff, ",") {
			candidate := strings.TrimSpace(p)
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

func closeStatusFromError(err error, fallback websocket.StatusCode) (websocket.StatusCode, string) {
	if err == nil {
		status := websocket.StatusNormalClosure
		return status, statusText(status)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		status := websocket.StatusNormalClosure
		reason := statusText(status)
		if reason == "" {
			reason = "context canceled"
		}
		return status, reason
	}
	status := websocket.CloseStatus(err)
	if status == -1 {
		status = fallback
	}
	reason := statusText(status)
	if reason == "" {
		reason = "closing"
	}
	return status, reason
}

func cloneBytes(src []byte) []byte {
	dup := make([]byte, len(src))
	copy(dup, src)
	return dup
}

func statusText(code websocket.StatusCode) string {
	switch code {
	case websocket.StatusNormalClosure:
		return "normal closure"
	case websocket.StatusGoingAway:
		return "going away"
	case websocket.StatusProtocolError:
		return "protocol error"
	case websocket.StatusUnsupportedData:
		return "unsupported data"
	case websocket.StatusPolicyViolation:
		return "policy violation"
	case websocket.StatusInternalError:
		return "internal error"
	case websocket.StatusMessageTooBig:
		return "message too big"
	case websocket.StatusMandatoryExtension:
		return "mandatory extension"
	case websocket.StatusBadGateway:
		return "bad gateway"
	case websocket.StatusTLSHandshake:
		return "tls handshake failure"
	default:
		if code >= 0 {
			return fmt.Sprintf("status %d", code)
		}
		return ""
	}
}
