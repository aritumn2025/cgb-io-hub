package config

import "time"

const (
	defaultAddr            = ":8765"
	defaultOrigins         = "*"
	defaultMaxControllers  = 4
	defaultRateHz          = 60
	defaultRegisterTimeout = 5 * time.Second
	defaultWriteTimeout    = 2 * time.Second
	defaultShutdownTimeout = 10 * time.Second
	defaultDBAPITimeout    = 3 * time.Second
	defaultSessionTokenTTL = 60 * time.Second
	defaultGameID          = "Game_1"
	defaultAttractionID    = "Game_1"
	defaultStaffName       = "hub"
)

// Config holds application level configuration.
type Config struct {
	Addr            string
	Origins         []string
	MaxControllers  int
	RateHz          int
	RegisterTimeout time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	DBBaseURL       string
	GameID          string
	AttractionID    string
	StaffName       string
	DBAPITimeout    time.Duration
	SessionTokenTTL time.Duration
}
