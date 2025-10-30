package config

import "time"

const (
	defaultAddr              = ":8765"
	defaultOrigins           = "*"
	defaultMaxControllers    = 4
	defaultRateHz            = 60
	defaultRegisterTimeout   = 5 * time.Second
	defaultWriteTimeout      = 2 * time.Second
	defaultShutdownTimeout   = 10 * time.Second
	defaultPersonaTimeout    = 3 * time.Second
	defaultSessionTokenTTL   = 60 * time.Second
	defaultPersonaGame       = "Game_1"
	defaultPersonaAttraction = "Game_1"
	defaultPersonaStaff      = "hub"
)

// Config holds application level configuration.
type Config struct {
	Addr              string
	Origins           []string
	MaxControllers    int
	RateHz            int
	RegisterTimeout   time.Duration
	WriteTimeout      time.Duration
	ShutdownTimeout   time.Duration
	PersonaBaseURL    string
	PersonaGameName   string
	PersonaAttraction string
	PersonaStaff      string
	PersonaTimeout    time.Duration
	SessionTokenTTL   time.Duration
}
