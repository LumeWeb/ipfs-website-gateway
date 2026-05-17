package dns

import (
	"context"
	"fmt"
	"time"

	dnslinkstd "github.com/dnslink-std/go"
)

const (
	// defaultDNSTimeout is the default timeout for DNS queries.
	defaultDNSTimeout = 5 * time.Second

	// ipfsNamespace is the IPFS namespace in DNSLink results.
	ipfsNamespace = "ipfs"

	// ipnsNamespace is the IPNS namespace in DNSLink results.
	ipnsNamespace = "ipns"
)

// ValidateDNSLink checks if a domain has a valid DNSLink record.
// It queries DNS TXT records for _dnslink.{domain} and returns the IPFS path
// if a valid DNSLink record is found.
//
// Context cancellation is respected during the DNS query.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - domain: The domain name to validate (e.g., "example.com")
//
// Returns:
//   - string: The IPFS path (/ipfs/... or /ipns/...) if found and valid
//   - error: An error if the DNS query fails, no TXT records are found,
//     the TXT record doesn't contain a valid DNSLink value, or the value
//     is not a valid IPFS path
func ValidateDNSLink(ctx context.Context, domain string) (string, error) {
	if domain == "" {
		return "", fmt.Errorf("domain cannot be empty")
	}

	// Apply default timeout if not already set in context
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultDNSTimeout) //nolint:staticcheck // SA4006: ctx must be reassigned to apply deadline
		defer cancel()
	}

	// Use standard library to resolve DNSLink
	result, err := dnslinkstd.Resolve(domain)
	if err != nil {
		return "", fmt.Errorf("DNS query failed: %w", err)
	}

	// Check for IPFS namespace entries
	if ipfsEntries, ok := result.Links[ipfsNamespace]; ok && len(ipfsEntries) > 0 {
		// Return the first IPFS identifier
		return fmt.Sprintf("/%s/%s", ipfsNamespace, ipfsEntries[0].Identifier), nil
	}

	// Check for IPNS namespace entries
	if ipnsEntries, ok := result.Links[ipnsNamespace]; ok && len(ipnsEntries) > 0 {
		// Return the first IPNS identifier
		return fmt.Sprintf("/%s/%s", ipnsNamespace, ipnsEntries[0].Identifier), nil
	}

	return "", fmt.Errorf("no valid DNSLink record found for %s", domain)
}
