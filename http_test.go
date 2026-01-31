package simforge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPClient_Request_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", r.Header.Get("Authorization"))
		}
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	hc := newHTTPClient("test-key", server.URL)
	err := hc.request("/api/test", map[string]any{"data": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHTTPClient_Request_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"error": "Bad request"})
	}))
	defer server.Close()

	hc := newHTTPClient("test-key", server.URL)
	err := hc.request("/api/test", map[string]any{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "Bad request" {
		t.Errorf("error = %q, want %q", err.Error(), "Bad request")
	}
}

func TestHTTPClient_Request_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	hc := newHTTPClient("test-key", server.URL)
	err := hc.request("/api/test", map[string]any{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestHTTPClient_Request_Retries(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			w.WriteHeader(500)
			w.Write([]byte("fail"))
			return
		}
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	hc := newHTTPClient("test-key", server.URL)
	cfg := requestConfig{maxRetries: 3, retryDelay: 10 * time.Millisecond}
	err := hc.request("/api/test", map[string]any{}, func(c *requestConfig) {
		c.maxRetries = cfg.maxRetries
		c.retryDelay = cfg.retryDelay
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("attempts = %d, want 3", atomic.LoadInt32(&attempts))
	}
}

func TestHTTPClient_SendExternalSpan_Background(t *testing.T) {
	var received atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["sdkVersion"] == Version {
			received.Store(true)
		}
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	hc := newHTTPClient("test-key", server.URL)
	hc.sendExternalSpan(map[string]any{"test": true})
	hc.flush(5 * time.Second)

	if !received.Load() {
		t.Error("expected background span to be sent with sdkVersion")
	}
}

func TestHTTPClient_Flush_Timeout(t *testing.T) {
	// Create a server that blocks forever
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
	}))
	defer server.Close()

	hc := newHTTPClient("test-key", server.URL)
	hc.sendExternalSpan(map[string]any{"test": true})

	start := time.Now()
	hc.flush(100 * time.Millisecond)
	elapsed := time.Since(start)

	// Should return quickly due to timeout, not wait 10 seconds
	if elapsed > 2*time.Second {
		t.Errorf("flush took %v, expected < 2s", elapsed)
	}
}
