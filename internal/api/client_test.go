package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

func TestNewClient(t *testing.T) {
	baseURL := "https://api.example.com"
	secret := "test-secret"
	timeout := 30 * time.Second

	apiClient := NewClient(baseURL, secret, timeout)

	if apiClient == nil {
		t.Fatal("NewClient returned nil")
	}

	// Test that the client implements the interface by calling GetWebsite
	// This will fail due to invalid URL, but proves the interface is satisfied
	_, err := apiClient.GetWebsite(context.Background(), "test.com")
	if err == nil {
		t.Error("expected error for invalid URL, got nil")
	}
}

func TestGetWebsite_Success(t *testing.T) {
	expectedResponse := types.GatewayWebsiteResponse{
		Domain:     "example.com",
		TargetType: "ipfs",
		TargetHash: "QmExample",
		Status:     types.StatusActive,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET request, got %s", r.Method)
		}

		if r.URL.Path != "/internal/websites/example.com" {
			t.Errorf("expected path /internal/websites/example.com, got %s", r.URL.Path)
		}

		secret := r.Header.Get(gatewaySecretHeader)
		if secret != "test-secret" {
			t.Errorf("expected X-Gateway-Secret header 'test-secret', got '%s'", secret)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expectedResponse)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-secret", 10*time.Second)

	resp, err := client.GetWebsite(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Domain != expectedResponse.Domain {
		t.Errorf("expected domain %s, got %s", expectedResponse.Domain, resp.Domain)
	}

	if resp.TargetType != expectedResponse.TargetType {
		t.Errorf("expected target type %s, got %s", expectedResponse.TargetType, resp.TargetType)
	}

	if resp.TargetHash != expectedResponse.TargetHash {
		t.Errorf("expected target hash %s, got %s", expectedResponse.TargetHash, resp.TargetHash)
	}

	if resp.Status != expectedResponse.Status {
		t.Errorf("expected status %s, got %s", expectedResponse.Status, resp.Status)
	}
}

func TestGetWebsite_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-secret", 10*time.Second)

	_, err := client.GetWebsite(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected error for 404 status, got nil")
	}

	expectedErr := "website not found: example.com"
	if err.Error() != expectedErr {
		t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestGetWebsite_Gone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-secret", 10*time.Second)

	_, err := client.GetWebsite(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected error for 410 status, got nil")
	}

	expectedErr := "website is broken or gone: example.com"
	if err.Error() != expectedErr {
		t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestGetWebsite_UnexpectedStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-secret", 10*time.Second)

	_, err := client.GetWebsite(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected error for 500 status, got nil")
	}

	expectedErr := "unexpected status code: 500"
	if err.Error() != expectedErr {
		t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestGetWebsite_EmptyDomain(t *testing.T) {
	client := NewClient("https://api.example.com", "test-secret", 10*time.Second)

	_, err := client.GetWebsite(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty domain, got nil")
	}

	expectedErr := "domain cannot be empty"
	if err.Error() != expectedErr {
		t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestGetWebsite_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-secret", 10*time.Second)

	_, err := client.GetWebsite(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}

	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestGetWebsite_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-secret", 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.GetWebsite(ctx, "example.com")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
