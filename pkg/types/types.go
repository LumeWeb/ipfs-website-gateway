package types

import "time"

// WebsiteStatus represents the status of a website from the internal API.
type WebsiteStatus string

const (
	// StatusPendingValidation indicates the website is awaiting validation.
	StatusPendingValidation WebsiteStatus = "pending_validation"

	// StatusActive indicates the website is active and serving content.
	StatusActive WebsiteStatus = "active"

	// StatusBroken indicates the website is broken or unreachable.
	StatusBroken WebsiteStatus = "broken"
)

// GatewayWebsiteResponse represents the response from the internal API.
type GatewayWebsiteResponse struct {
	// Domain is the domain name of the website.
	Domain string `json:"domain"`

	// TargetType specifies the content type: "ipfs" or "ipns".
	TargetType string `json:"target_type"`

	// TargetHash is the CID for IPFS or IPNS name for IPNS.
	TargetHash string `json:"target_hash"`

	// Status indicates the current status of the website.
	Status WebsiteStatus `json:"status"`
}

// CacheEntry represents a cached website status with metadata.
type CacheEntry struct {
	// Response is the cached API response.
	Response *GatewayWebsiteResponse

	// CachedAt is when this entry was added to the cache.
	CachedAt time.Time

	// ExpiresAt is when this cache entry should be considered expired.
	ExpiresAt time.Time
}

// CacheResult represents the result of a cache lookup.
type CacheResult struct {
	// Hit indicates whether the cache contained an entry for the requested key.
	Hit bool

	// Entry is the cached entry, if Hit is true.
	Entry *CacheEntry

	// Expired indicates whether the cached entry has passed its expiration time.
	Expired bool
}
