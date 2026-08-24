package types

import (
	"errors"

	ipfs "go.lumeweb.com/ipfs-sdk"
)

// SiteState is the normalized, centralized classification of a website's
// serving state. All consumers (gateway, cache, server, watcher) should
// derive decisions from this type rather than comparing against the raw
// API status strings directly. This keeps broken/active/pending logic in
// one place (DRY) so it cannot drift between callers.
type SiteState string

const (
	StateActive   SiteState = "active"
	StatePending  SiteState = "pending_validation"
	StateBroken   SiteState = "broken"
	StateInactive SiteState = "inactive"
	StateUnknown  SiteState = "unknown"
)

// IsActive reports whether the site is fully serviceable and should be served.
func (s SiteState) IsActive() bool { return s == StateActive }

// IsPending reports whether the site is awaiting validation.
func (s SiteState) IsPending() bool { return s == StatePending }

// IsBroken reports whether the site is marked broken/removed (410).
func (s SiteState) IsBroken() bool { return s == StateBroken }

// IsInactive reports whether the site exists but is not currently serviceable.
func (s SiteState) IsInactive() bool { return s == StateInactive }

// IsUnknown reports whether the state could not be determined.
func (s SiteState) IsUnknown() bool { return s == StateUnknown }

// IsServiceable reports whether the site should be served to clients.
func (s SiteState) IsServiceable() bool { return s.IsActive() }

// NeedsShortTTL reports whether the site state is transient and should be
// cached with the short TTL so it revalidates sooner. Pending and broken
// sites are the transient states that benefit from more frequent rechecks.
func (s SiteState) NeedsShortTTL() bool { return s.IsPending() || s.IsBroken() }

// StateFromStatus maps a raw API status string to a normalized SiteState.
func StateFromStatus(status string) SiteState {
	switch status {
	case StatusActive:
		return StateActive
	case StatusPendingValidation:
		return StatePending
	case StatusBroken:
		return StateBroken
	case "":
		return StateUnknown
	default:
		return StateInactive
	}
}

// Classify normalizes a website response into its SiteState. A nil response
// is treated as unknown.
func Classify(website *GatewayWebsiteResponse) SiteState {
	if website == nil {
		return StateUnknown
	}
	return StateFromStatus(website.Status)
}

// ClassifyErr normalizes an API error into a SiteState. It is used to
// interpret request-time failures (e.g. website gone/broken) using the same
// vocabulary as Classify.
func ClassifyErr(err error) SiteState {
	switch {
	case errors.Is(err, ipfs.ErrGone):
		return StateBroken
	case errors.Is(err, ipfs.ErrNotFound):
		return StateInactive
	default:
		return StateUnknown
	}
}
