// Package config loads runtime configuration from environment variables and
// resolves the on-disk paths used for the Telegram session and (later) the
// SQLite cache.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds everything the binary needs to talk to Telegram.
type Config struct {
	AppID    int    // api_id from my.telegram.org
	AppHash  string // api_hash from my.telegram.org
	Home     string // base dir for session/db (~/.config/tg-mcp by default)
	Phone    string // optional, only used to pre-fill the login flow
	Password string // optional 2FA password, only used during login

	EnableWrite bool // when true, the MCP server also exposes write tools (posting)
}

// SessionPath returns the path to the gotd session file.
func (c Config) SessionPath() string { return filepath.Join(c.Home, "session.json") }

// DBPath returns the path to the SQLite cache (used from phase 2 onwards).
func (c Config) DBPath() string { return filepath.Join(c.Home, "cache.db") }

// Load reads configuration from the environment.
//
// Required: TG_APP_ID, TG_APP_HASH.
// Optional: TGMCP_HOME (default ~/.config/tg-mcp), TG_PHONE, TG_PASSWORD,
// TGMCP_ENABLE_WRITE.
func Load() (Config, error) {
	var c Config

	rawID := os.Getenv("TG_APP_ID")
	if rawID == "" {
		return c, fmt.Errorf("TG_APP_ID is not set (get api_id/api_hash at https://my.telegram.org)")
	}
	id, err := strconv.Atoi(rawID)
	if err != nil {
		return c, fmt.Errorf("TG_APP_ID is not a number: %w", err)
	}
	c.AppID = id

	c.AppHash = os.Getenv("TG_APP_HASH")
	if c.AppHash == "" {
		return c, fmt.Errorf("TG_APP_HASH is not set")
	}

	c.Home, err = ResolveHome()
	if err != nil {
		return c, err
	}

	c.Phone = os.Getenv("TG_PHONE")
	c.Password = os.Getenv("TG_PASSWORD")
	c.EnableWrite = parseEnableWrite(os.Getenv("TGMCP_ENABLE_WRITE"))

	return c, nil
}

// parseEnableWrite treats 1/true/yes (case-insensitive) as enabling write tools.
func parseEnableWrite(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// ResolveHome returns the data directory (TGMCP_HOME or the default under the
// user config dir), creating it if needed. Requires no Telegram credentials, so
// it is usable by credential-free commands like `tgmcp pref`.
func ResolveHome() (string, error) {
	home := os.Getenv("TGMCP_HOME")
	if home == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve config dir: %w", err)
		}
		home = filepath.Join(base, "tg-mcp")
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", fmt.Errorf("create home dir %q: %w", home, err)
	}
	return home, nil
}
