package workapi

import (
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/issueops"
)

// SettingKeyLeaseTTL is the workspace setting that sets how long a claim's
// lease stays valid without a heartbeat. It is a Go duration string ("90s",
// "60m", "2h"). Unset or empty means the built-in default (60m).
//
// It lives in the workspace database, like claim.pools: claims and heartbeats
// read it through the settings plane, so every worker on the workspace agrees
// on one lease length and nobody has to set an environment variable.
const SettingKeyLeaseTTL = "lease.ttl"

// ParseLeaseTTLSetting parses a stored lease.ttl value. An empty (or all-space)
// value means "not set" and returns 0 with no error: the caller falls back to
// the default lease TTL. Anything else must be a Go duration greater than zero;
// the error names the key and the offending value.
//
// One parser serves the write check (ValidateSettingWrite) and the read at
// claim/heartbeat/reclaim time, so a value the write accepted can never be
// refused by a reader, and one a reader refuses (a row written by a bd that
// predates the check) is reported with the same text.
func ParseLeaseTTLSetting(raw string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	ttl, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid %s value %q: want a duration such as 90s, 60m or 2h",
			issueops.ErrValidation, SettingKeyLeaseTTL, raw)
	}
	if ttl <= 0 {
		return 0, fmt.Errorf("%w: invalid %s value %q: the lease length must be greater than zero",
			issueops.ErrValidation, SettingKeyLeaseTTL, raw)
	}
	return ttl, nil
}
