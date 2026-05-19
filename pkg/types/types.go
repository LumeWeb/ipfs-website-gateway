package types

import (
	"time"

	ipfs "go.lumeweb.com/ipfs-sdk"
)

const (
	StatusPendingValidation = "pending_validation"
	StatusActive             = "active"
	StatusBroken             = "broken"
)

type GatewayWebsiteResponse = ipfs.GatewayWebsiteResponse

type CacheEntry struct {
	Response  *ipfs.GatewayWebsiteResponse
	Err       error
	CachedAt  time.Time
	ExpiresAt time.Time
}

type CacheResult struct {
	Hit     bool
	Entry   *CacheEntry
	Expired bool
}
