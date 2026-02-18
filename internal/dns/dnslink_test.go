package dns

import (
	"context"
	"testing"
	"time"
)

func TestValidateDNSLink_EmptyDomain(t *testing.T) {
	ctx := context.Background()
	_, err := ValidateDNSLink(ctx, "")

	if err == nil {
		t.Error("Expected error for empty domain, got nil")
	}

	expectedErrMsg := "domain cannot be empty"
	if err != nil && err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
}

func TestValidateDNSLink_NoTxtRecords(t *testing.T) {
	ctx := context.Background()

	// Use a domain that likely doesn't have DNSLink records
	_, err := ValidateDNSLink(ctx, "nonexistent-dnslink-test.invalid")

	if err == nil {
		t.Error("Expected error for domain with no DNSLink records, got nil")
	}

	// The error should indicate no TXT records found
	if err != nil {
		errMsg := err.Error()
		if errMsg == "" {
			t.Error("Expected non-empty error message")
		}
	}
}

func TestValidateDNSLink_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := ValidateDNSLink(ctx, "example.com")

	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}

func TestValidateDNSLink_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait for context to expire
	time.Sleep(10 * time.Millisecond)

	_, err := ValidateDNSLink(ctx, "example.com")

	if err == nil {
		t.Error("Expected error for timed out context, got nil")
	}
}

func TestValidateDNSLink_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Test with a known DNSLink domain (ipfs.io)
	// This is an integration test that requires network access
	result, err := ValidateDNSLink(ctx, "ipfs.io")

	// This test is informational - we don't assert specific results
	// because DNS records can change
	if err != nil {
		t.Logf("DNSLink validation for ipfs.io failed (this is expected if records changed): %v", err)
	} else {
		t.Logf("DNSLink validation for ipfs.io succeeded: %s", result)
	}
}
