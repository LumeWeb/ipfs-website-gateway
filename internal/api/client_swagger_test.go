package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	clientgen "go.lumeweb.com/ipfs-website-gateway/internal/client"
	"go.lumeweb.com/ipfs-website-gateway/pkg/types"
)

func TestNewSwaggerClient(t *testing.T) {
	baseURL := "https://api.example.com"
	secret := "test-secret"
	timeout := 30 * time.Second

	apiClient := NewSwaggerClient(baseURL, secret, timeout)

	if apiClient == nil {
		t.Fatal("NewSwaggerClient returned nil")
	}

	swaggerClient, ok := apiClient.(*swaggerAPIClient)
	if !ok {
		t.Fatal("NewSwaggerClient did not return *swaggerAPIClient")
	}

	if swaggerClient.baseURL != baseURL {
		t.Errorf("expected baseURL %s, got %s", baseURL, swaggerClient.baseURL)
	}

	if swaggerClient.secret != secret {
		t.Errorf("expected secret %s, got %s", secret, swaggerClient.secret)
	}

	if swaggerClient.client == nil {
		t.Error("expected swaggerClient.client to be initialized")
	}
}

func TestSwaggerClient_GetWebsite_Success(t *testing.T) {
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

	client := NewSwaggerClient(server.URL, "test-secret", 10*time.Second)

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

func TestSwaggerClient_GetWebsite_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))
	defer server.Close()

	client := NewSwaggerClient(server.URL, "test-secret", 10*time.Second)

	_, err := client.GetWebsite(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected error for 404 status, got nil")
	}

	expectedErr := "website not found: example.com"
	if err.Error() != expectedErr {
		t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestSwaggerClient_GetWebsite_Gone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer server.Close()

	client := NewSwaggerClient(server.URL, "test-secret", 10*time.Second)

	_, err := client.GetWebsite(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected error for 410 status, got nil")
	}

	expectedErr := "website is broken or gone: example.com"
	if err.Error() != expectedErr {
		t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestSwaggerClient_GetWebsite_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	}))
	defer server.Close()

	client := NewSwaggerClient(server.URL, "test-secret", 10*time.Second)

	_, err := client.GetWebsite(context.Background(), "example.com")
	if err == nil {
		t.Fatal("expected error for 401 status, got nil")
	}

	// The swagger client returns "unexpected status code: 401" for 401 without a JSON body
	expectedErr := "unexpected status code: 401"
	if err.Error() != expectedErr {
		t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestSwaggerClient_GetWebsite_EmptyDomain(t *testing.T) {
	client := NewSwaggerClient("https://api.example.com", "test-secret", 10*time.Second)

	_, err := client.GetWebsite(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty domain, got nil")
	}

	expectedErr := "domain cannot be empty"
	if err.Error() != expectedErr {
		t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestConvertGatewayWebsiteResponse(t *testing.T) {
	src := &clientgen.GatewayWebsiteResponse{
		Domain:     "example.com",
		TargetType: "ipfs",
		TargetHash: "QmExample",
		Status:     "active",
	}

	result := convertGatewayWebsiteResponse(src)

	if result.Domain != src.Domain {
		t.Errorf("expected domain %s, got %s", src.Domain, result.Domain)
	}

	if result.TargetType != src.TargetType {
		t.Errorf("expected target type %s, got %s", src.TargetType, result.TargetType)
	}

	if result.TargetHash != src.TargetHash {
		t.Errorf("expected target hash %s, got %s", src.TargetHash, result.TargetHash)
	}

	if result.Status != types.WebsiteStatus(src.Status) {
		t.Errorf("expected status %s, got %s", src.Status, result.Status)
	}
}
