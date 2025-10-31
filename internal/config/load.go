package config

import (
	"flag"
	"os"
	"strconv"
	"strings"
	"time"
)

// Load parses CLI flags and environment variables to construct Config.
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("hub", flag.ContinueOnError)
	addrFlag := fs.String("addr", "", "listen address (ADDR)")
	originsFlag := fs.String("origins", "", "allowed origins, comma separated (ORIGINS)")
	maxControllersFlag := fs.Int("max-clients", 0, "max controller connections (MAX_CLIENTS)")
	rateHzFlag := fs.Int("rate-hz", 0, "relay rate limit in Hz (RATE_HZ)")
	registerTimeoutFlag := fs.Duration("register-timeout", 0, "controller register timeout (REGISTER_TIMEOUT)")
	writeTimeoutFlag := fs.Duration("write-timeout", 0, "game write timeout (WRITE_TIMEOUT)")
	shutdownTimeoutFlag := fs.Duration("shutdown-timeout", 0, "graceful shutdown timeout (SHUTDOWN_TIMEOUT)")
	dbBaseURLFlag := fs.String("db-base-url", "", "PersonaGo API base URL (DB_BASE_URL)")
	personaBaseURLFlag := fs.String("persona-base-url", "", "PersonaGo API base URL (deprecated: PERSONA_BASE_URL)")
	gameIDFlag := fs.String("game-id", "", "PersonaGo game identifier (GAME_ID)")
	personaGameFlag := fs.String("persona-game", "", "PersonaGo game name (deprecated: PERSONA_GAME)")
	attractionIDFlag := fs.String("attraction-id", "", "PersonaGo attraction identifier (ATTRACTION_ID)")
	personaAttractionFlag := fs.String("persona-attraction", "", "PersonaGo attraction name (deprecated: PERSONA_ATTRACTION)")
	staffNameFlag := fs.String("staff-name", "", "PersonaGo staff identifier (STAFF_NAME)")
	personaStaffFlag := fs.String("persona-staff", "", "PersonaGo staff identifier (deprecated: PERSONA_STAFF)")
	dbAPITimeoutFlag := fs.Duration("db-api-timeout", 0, "PersonaGo API client timeout (DB_API_TIMEOUT)")
	personaTimeoutFlag := fs.Duration("persona-timeout", 0, "PersonaGo API client timeout (deprecated: PERSONA_TIMEOUT)")
	sessionTokenTTLFlag := fs.Duration("session-token-ttl", 0, "controller session token TTL (SESSION_TOKEN_TTL)")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg := Config{
		Addr:            firstNonEmpty(*addrFlag, os.Getenv("ADDR"), defaultAddr),
		Origins:         parseOrigins(firstNonEmpty(*originsFlag, os.Getenv("ORIGINS"), defaultOrigins)),
		MaxControllers:  firstPositiveInt(*maxControllersFlag, envToInt("MAX_CLIENTS"), defaultMaxControllers),
		RateHz:          firstPositiveInt(*rateHzFlag, envToInt("RATE_HZ"), defaultRateHz),
		RegisterTimeout: firstPositiveDuration(*registerTimeoutFlag, envToDuration("REGISTER_TIMEOUT"), defaultRegisterTimeout),
		WriteTimeout:    firstPositiveDuration(*writeTimeoutFlag, envToDuration("WRITE_TIMEOUT"), defaultWriteTimeout),
		ShutdownTimeout: firstPositiveDuration(*shutdownTimeoutFlag, envToDuration("SHUTDOWN_TIMEOUT"), defaultShutdownTimeout),
		DBBaseURL: strings.TrimSpace(firstNonEmpty(
			*dbBaseURLFlag,
			*personaBaseURLFlag,
			os.Getenv("DB_BASE_URL"),
			os.Getenv("PERSONA_BASE_URL"),
		)),
		GameID: firstNonEmpty(
			*gameIDFlag,
			*personaGameFlag,
			os.Getenv("GAME_ID"),
			os.Getenv("PERSONA_GAME"),
			defaultGameID,
		),
		AttractionID: firstNonEmpty(
			*attractionIDFlag,
			*personaAttractionFlag,
			os.Getenv("ATTRACTION_ID"),
			os.Getenv("PERSONA_ATTRACTION"),
			defaultAttractionID,
		),
		StaffName: firstNonEmpty(
			*staffNameFlag,
			*personaStaffFlag,
			os.Getenv("STAFF_NAME"),
			os.Getenv("PERSONA_STAFF"),
			defaultStaffName,
		),
		DBAPITimeout: firstPositiveDuration(
			*dbAPITimeoutFlag,
			*personaTimeoutFlag,
			envToDuration("DB_API_TIMEOUT"),
			envToDuration("PERSONA_TIMEOUT"),
			defaultDBAPITimeout,
		),
		SessionTokenTTL: firstPositiveDuration(*sessionTokenTTLFlag, envToDuration("SESSION_TOKEN_TTL"), defaultSessionTokenTTL),
	}

	if cfg.SessionTokenTTL <= 0 {
		cfg.SessionTokenTTL = defaultSessionTokenTTL
	}

	return cfg, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parseOrigins(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "*" {
		return nil
	}
	parts := strings.Split(trimmed, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		candidate := strings.TrimSpace(p)
		if candidate != "" {
			origins = append(origins, candidate)
		}
	}
	return origins
}

func firstPositiveInt(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func firstPositiveDuration(values ...time.Duration) time.Duration {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func envToInt(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return v
}

func envToDuration(key string) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0
	}
	return d
}
