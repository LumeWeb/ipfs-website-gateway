package types

import (
	"testing"
	"time"
)

func TestWebsiteStatusConstants(t *testing.T) {
	tests := []struct {
		name   string
		status WebsiteStatus
		value  string
	}{
		{"PendingValidation", StatusPendingValidation, "pending_validation"},
		{"Active", StatusActive, "active"},
		{"Broken", StatusBroken, "broken"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.value {
				t.Errorf("WebsiteStatus = %v, want %v", tt.status, tt.value)
			}
		})
	}
}

func TestGatewayWebsiteResponse(t *testing.T) {
	response := GatewayWebsiteResponse{
		Domain:     "example.com",
		TargetType: "ipfs",
		TargetHash: "QmTest123",
		Status:     StatusActive,
	}

	if response.Domain != "example.com" {
		t.Errorf("Domain = %v, want %v", response.Domain, "example.com")
	}

	if response.TargetType != "ipfs" {
		t.Errorf("TargetType = %v, want %v", response.TargetType, "ipfs")
	}

	if response.TargetHash != "QmTest123" {
		t.Errorf("TargetHash = %v, want %v", response.TargetHash, "QmTest123")
	}

	if response.Status != StatusActive {
		t.Errorf("Status = %v, want %v", response.Status, StatusActive)
	}
}

func TestCacheEntry(t *testing.T) {
	now := time.Now()
	entry := CacheEntry{
		Response: &GatewayWebsiteResponse{
			Domain:     "example.com",
			TargetType: "ipfs",
			TargetHash: "QmTest123",
			Status:     StatusActive,
		},
		CachedAt:  now,
		ExpiresAt: now.Add(5 * time.Minute),
	}

	if entry.Response == nil {
		t.Error("Response should not be nil")
	}

	if entry.Response.Domain != "example.com" {
		t.Errorf("Response.Domain = %v, want %v", entry.Response.Domain, "example.com")
	}

	if entry.CachedAt.IsZero() {
		t.Error("CachedAt should not be zero")
	}

	if entry.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should not be zero")
	}
}

func TestCacheResult(t *testing.T) {
	tests := []struct {
		name    string
		result  CacheResult
		wantHit bool
	}{
		{
			name: "Cache Hit",
			result: CacheResult{
				Hit:     true,
				Entry:   &CacheEntry{},
				Expired: false,
			},
			wantHit: true,
		},
		{
			name: "Cache Miss",
			result: CacheResult{
				Hit:     false,
				Entry:   nil,
				Expired: false,
			},
			wantHit: false,
		},
		{
			name: "Cache Hit but Expired",
			result: CacheResult{
				Hit:     true,
				Entry:   &CacheEntry{},
				Expired: true,
			},
			wantHit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.result.Hit != tt.wantHit {
				t.Errorf("CacheResult.Hit = %v, want %v", tt.result.Hit, tt.wantHit)
			}
		})
	}
}

func TestCacheResultExpired(t *testing.T) {
	tests := []struct {
		name        string
		result      CacheResult
		wantExpired bool
	}{
		{
			name: "Not Expired",
			result: CacheResult{
				Hit:     true,
				Entry:   &CacheEntry{},
				Expired: false,
			},
			wantExpired: false,
		},
		{
			name: "Expired",
			result: CacheResult{
				Hit:     true,
				Entry:   &CacheEntry{},
				Expired: true,
			},
			wantExpired: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.result.Expired != tt.wantExpired {
				t.Errorf("CacheResult.Expired = %v, want %v", tt.result.Expired, tt.wantExpired)
			}
		})
	}
}
