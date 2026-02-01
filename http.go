package simforge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

type httpClient struct {
	apiKey     string
	serviceURL string
	client     *http.Client
	wg         sync.WaitGroup
}

func newHTTPClient(apiKey, serviceURL string) *httpClient {
	return &httpClient{
		apiKey:     apiKey,
		serviceURL: serviceURL,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// request makes a POST request to the Simforge API.
func (h *httpClient) request(endpoint string, payload map[string]any, opts ...requestOption) error {
	cfg := requestConfig{
		timeout:    0, // use default client timeout
		maxRetries: 1,
		retryDelay: 100 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	body, err := MarshalSpanPayload(payload)
	if err != nil {
		return fmt.Errorf("simforge: failed to marshal payload: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < cfg.maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(cfg.retryDelay)
		}

		client := h.client
		if cfg.timeout > 0 {
			client = &http.Client{Timeout: cfg.timeout}
		}

		req, err := http.NewRequest("POST", h.serviceURL+endpoint, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("simforge: failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+h.apiKey)

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("simforge: HTTP %d: %s", resp.StatusCode, string(respBody))
			continue
		}

		// Check for error in response body
		var result map[string]any
		if json.Unmarshal(respBody, &result) == nil {
			if errMsg, ok := result["error"].(string); ok {
				if url, ok := result["url"].(string); ok {
					return fmt.Errorf("%s Configure it at: %s%s", errMsg, h.serviceURL, url)
				}
				return fmt.Errorf("%s", errMsg)
			}
		}

		return nil
	}

	return lastErr
}

// sendExternalSpan sends a span payload in the background (fire-and-forget).
func (h *httpClient) sendExternalSpan(payload map[string]any) {
	merged := make(map[string]any, len(payload)+1)
	for k, v := range payload {
		merged[k] = v
	}
	merged["sdkVersion"] = Version

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		defer func() {
			recover() // Never crash the host app due to span sending
		}()
		_ = h.request("/api/sdk/externalSpans", merged, withTimeout(30*time.Second))
	}()
}

// flush waits for all pending background goroutines to complete.
func (h *httpClient) flush(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// requestOption configures a single request.
type requestOption func(*requestConfig)

type requestConfig struct {
	timeout    time.Duration
	maxRetries int
	retryDelay time.Duration
}

func withTimeout(d time.Duration) requestOption {
	return func(c *requestConfig) { c.timeout = d }
}
