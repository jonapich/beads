package workapi

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/issueops"
)

func TestParseLeaseTTLSettingAcceptsPositiveDurations(t *testing.T) {
	for raw, want := range map[string]time.Duration{
		"90s":    90 * time.Second,
		"60m":    60 * time.Minute,
		"2h":     2 * time.Hour,
		" 15m ":  15 * time.Minute,
		"1h30m":  90 * time.Minute,
		"1500ms": 1500 * time.Millisecond,
	} {
		got, err := ParseLeaseTTLSetting(raw)
		if err != nil {
			t.Errorf("ParseLeaseTTLSetting(%q) = %v, want it accepted", raw, err)
			continue
		}
		if got != want {
			t.Errorf("ParseLeaseTTLSetting(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestParseLeaseTTLSettingTreatsEmptyAsUnset(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		got, err := ParseLeaseTTLSetting(raw)
		if err != nil || got != 0 {
			t.Errorf("ParseLeaseTTLSetting(%q) = (%v, %v), want (0, nil): unset means the default", raw, got, err)
		}
	}
}

func TestParseLeaseTTLSettingRefusesWhatIsNotAPositiveDuration(t *testing.T) {
	for _, raw := range []string{"soon", "60", "0", "0s", "-5m", "5 minutes", "1d"} {
		_, err := ParseLeaseTTLSetting(raw)
		if !errors.Is(err, issueops.ErrValidation) {
			t.Errorf("ParseLeaseTTLSetting(%q) error = %v, want ErrValidation", raw, err)
			continue
		}
		if !strings.Contains(err.Error(), SettingKeyLeaseTTL) {
			t.Errorf("ParseLeaseTTLSetting(%q) error %q does not name %q", raw, err, SettingKeyLeaseTTL)
		}
	}
}

func TestValidateSettingWriteChecksTheLeaseTTL(t *testing.T) {
	for _, value := range []string{"60m", "90s", ""} {
		got, err := ValidateSettingWrite(SettingKeyLeaseTTL, value)
		if err != nil {
			t.Errorf("ValidateSettingWrite(lease.ttl, %q) = %v, want it accepted", value, err)
		}
		if got != value {
			t.Errorf("ValidateSettingWrite(lease.ttl, %q) returned %q, want the value unchanged", value, got)
		}
	}
	for _, value := range []string{"soon", "0", "-1h", "60"} {
		if _, err := ValidateSettingWrite(SettingKeyLeaseTTL, value); !errors.Is(err, issueops.ErrValidation) {
			t.Errorf("ValidateSettingWrite(lease.ttl, %q) error = %v, want ErrValidation", value, err)
		}
	}
}
