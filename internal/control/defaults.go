package control

import (
	"crypto/sha256"
	"encoding/hex"
)

// Where a setting's current value comes from.
const (
	SourceDatabase    = "database"    // saved from the admin console
	SourceEnvironment = "environment" // the deployment's environment variables
	SourceDefault     = "default"
)

// Defaults are settings the deployment gives in environment variables. A
// value saved from the admin console is stored in the database and wins over
// them; removing it falls back to the environment again.
type Defaults struct {
	SSO      *SSO
	Telegram *Telegram
	// Domain and AdminLabel name the primary domain and the management host
	// under it. Unlike the other settings, the environment owns them: the
	// console can add domains and change this one's DNS mode, but can't
	// replace it. An empty AdminLabel keeps a generated one.
	Domain     string
	AdminLabel string
}

// SetDefaults installs the environment's settings. Call it once, before the
// database serves requests.
func (d *Database) SetDefaults(defaults Defaults) { d.defaults.Store(&defaults) }

// EnvironmentSets reports which settings the environment gives.
func (d *Database) EnvironmentSets() (sso, telegram bool) {
	env := d.environment()
	return env.SSO != nil, env.Telegram != nil
}

func (d *Database) environment() Defaults {
	if defaults := d.defaults.Load(); defaults != nil {
		return *defaults
	}
	return Defaults{}
}

// telegramKey identifies the bot and chat a test message went to, so a
// verification only counts while the environment still names them.
func telegramKey(token, chatID string) string {
	sum := sha256.Sum256([]byte(token + "\x00" + chatID))
	return hex.EncodeToString(sum[:])
}
