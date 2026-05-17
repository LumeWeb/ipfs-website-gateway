package dns

import (
	"context"
	"fmt"
	"net"
	"time"

	dnslinkstd "github.com/dnslink-std/go"
)

const (
	defaultDNSTimeout = 5 * time.Second
	ipfsNamespace     = "ipfs"
	ipnsNamespace     = "ipns"
)

// ValidateDNSLink checks if a domain has a valid DNSLink record.
// It queries DNS TXT records for _dnslink.{domain} and returns the IPFS path
// if a valid DNSLink record is found.
//
// Context cancellation and timeout are respected during the DNS query.
func ValidateDNSLink(ctx context.Context, domain string) (string, error) {
	if domain == "" {
		return "", fmt.Errorf("domain cannot be empty")
	}

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultDNSTimeout) //nolint:staticcheck // SA4006: ctx must be reassigned to apply deadline
		defer cancel()
	}

	dnslinkName := "_dnslink." + domain

	resolver := &dnslinkstd.Resolver{
		LookupTXT: func(name string) ([]dnslinkstd.LookupEntry, error) {
			txt, err := net.DefaultResolver.LookupTXT(ctx, name)
			if err != nil {
				return nil, err
			}
			entries := make([]dnslinkstd.LookupEntry, len(txt))
			for i, v := range txt {
				entries[i] = dnslinkstd.LookupEntry{Value: v}
			}
			return entries, nil
		},
	}

	result, err := resolver.Resolve(dnslinkName)
	if err != nil {
		return "", fmt.Errorf("DNS query failed: %w", err)
	}

	if ipfsEntries, ok := result.Links[ipfsNamespace]; ok && len(ipfsEntries) > 0 {
		return fmt.Sprintf("/%s/%s", ipfsNamespace, ipfsEntries[0].Identifier), nil
	}

	if ipnsEntries, ok := result.Links[ipnsNamespace]; ok && len(ipnsEntries) > 0 {
		return fmt.Sprintf("/%s/%s", ipnsNamespace, ipnsEntries[0].Identifier), nil
	}

	return "", fmt.Errorf("no valid DNSLink record found for %s", domain)
}
