package simforge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func newTestClient(serverURL string) *Client {
	return NewClient("test-key", WithServiceURL(serverURL))
}

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient("test-key")
	if c.apiKey != "test-key" {
		t.Errorf("apiKey = %q, want test-key", c.apiKey)
	}
	if c.serviceURL != DefaultServiceURL {
		t.Errorf("serviceURL = %q, want %q", c.serviceURL, DefaultServiceURL)
	}
}

func TestNewClient_WithServiceURL(t *testing.T) {
	c := NewClient("test-key", WithServiceURL("https://custom.example.com"))
	if c.serviceURL != "https://custom.example.com" {
		t.Errorf("serviceURL = %q, want https://custom.example.com", c.serviceURL)
	}
}

func TestSpan_BasicExecution(t *testing.T) {
	server := newSpanCaptureServer(t)
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	result, err := client.Span(ctx, "test-service", func(ctx context.Context) (any, error) {
		return "hello", nil
	})

	client.FlushTraces(5 * time.Second)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("result = %v, want hello", result)
	}
}

func TestSpan_WithNameAndType(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	client.Span(ctx, "order-service", func(ctx context.Context) (any, error) {
		return map[string]any{"total": 100}, nil
	}, WithName("ProcessOrder"), WithType("function"), WithFunctionName("processOrder"))

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	if captured == nil {
		t.Fatal("no span captured")
	}

	rawSpan := captured["rawSpan"].(map[string]any)
	spanData := rawSpan["span_data"].(map[string]any)

	if spanData["name"] != "ProcessOrder" {
		t.Errorf("name = %v, want ProcessOrder", spanData["name"])
	}
	if spanData["type"] != "function" {
		t.Errorf("type = %v, want function", spanData["type"])
	}
	if spanData["function_name"] != "processOrder" {
		t.Errorf("function_name = %v, want processOrder", spanData["function_name"])
	}
	if captured["traceFunctionKey"] != "order-service" {
		t.Errorf("traceFunctionKey = %v, want order-service", captured["traceFunctionKey"])
	}
	if captured["source"] != "go-sdk-function" {
		t.Errorf("source = %v, want go-sdk-function", captured["source"])
	}
}

func TestSpan_CapturesError(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	_, err := client.Span(ctx, "failing-service", func(ctx context.Context) (any, error) {
		return nil, errors.New("something went wrong")
	})

	client.FlushTraces(5 * time.Second)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "something went wrong" {
		t.Errorf("error = %q, want 'something went wrong'", err.Error())
	}

	mu.Lock()
	defer mu.Unlock()

	rawSpan := captured["rawSpan"].(map[string]any)
	spanData := rawSpan["span_data"].(map[string]any)
	if spanData["error"] != "something went wrong" {
		t.Errorf("span error = %v, want 'something went wrong'", spanData["error"])
	}
}

func TestSpan_InvalidType(t *testing.T) {
	client := NewClient("test-key")
	ctx := context.Background()

	_, err := client.Span(ctx, "test", func(ctx context.Context) (any, error) {
		return nil, nil
	}, WithType("invalid"))

	if err == nil {
		t.Fatal("expected error for invalid type")
	}
}

func TestSpan_NestedSpans_ShareTraceID(t *testing.T) {
	var mu sync.Mutex
	var payloads []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		payloads = append(payloads, payload)
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	client.Span(ctx, "outer", func(ctx context.Context) (any, error) {
		return client.Span(ctx, "inner", func(ctx context.Context) (any, error) {
			return "inner-result", nil
		})
	})

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	if len(payloads) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(payloads))
	}

	// Both should share the same trace ID
	traceID0 := payloads[0]["rawSpan"].(map[string]any)["trace_id"].(string)
	traceID1 := payloads[1]["rawSpan"].(map[string]any)["trace_id"].(string)
	if traceID0 != traceID1 {
		t.Errorf("trace IDs differ: %q vs %q", traceID0, traceID1)
	}
}

func TestSpan_NestedSpans_HaveParentID(t *testing.T) {
	var mu sync.Mutex
	var payloads []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		payloads = append(payloads, payload)
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	client.Span(ctx, "outer", func(ctx context.Context) (any, error) {
		return client.Span(ctx, "inner", func(ctx context.Context) (any, error) {
			return "result", nil
		})
	})

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	// Find inner and outer by traceFunctionKey
	var innerSpan, outerSpan map[string]any
	for _, p := range payloads {
		if p["traceFunctionKey"] == "inner" {
			innerSpan = p
		}
		if p["traceFunctionKey"] == "outer" {
			outerSpan = p
		}
	}

	if innerSpan == nil || outerSpan == nil {
		t.Fatal("could not find inner/outer spans")
	}

	innerRaw := innerSpan["rawSpan"].(map[string]any)
	outerRaw := outerSpan["rawSpan"].(map[string]any)

	// Inner should have parent_id pointing to outer
	if innerRaw["parent_id"] != outerRaw["id"] {
		t.Errorf("inner parent_id = %v, want %v (outer id)", innerRaw["parent_id"], outerRaw["id"])
	}

	// Outer should not have parent_id
	if _, ok := outerRaw["parent_id"]; ok {
		t.Error("outer span should not have parent_id")
	}
}

func TestSpan_IndependentCalls_DifferentTraceIDs(t *testing.T) {
	var mu sync.Mutex
	var payloads []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		payloads = append(payloads, payload)
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	client.Span(ctx, "service-1", func(ctx context.Context) (any, error) {
		return "a", nil
	})
	client.Span(ctx, "service-2", func(ctx context.Context) (any, error) {
		return "b", nil
	})

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	if len(payloads) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(payloads))
	}

	traceID0 := payloads[0]["rawSpan"].(map[string]any)["trace_id"].(string)
	traceID1 := payloads[1]["rawSpan"].(map[string]any)["trace_id"].(string)
	if traceID0 == traceID1 {
		t.Error("independent calls should have different trace IDs")
	}
}

func TestGetFunction_Span(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	fn := client.GetFunction("order-processing")
	ctx := context.Background()

	result, err := fn.Span(ctx, func(ctx context.Context) (any, error) {
		return "done", nil
	}, WithName("ProcessOrder"), WithType("function"))

	client.FlushTraces(5 * time.Second)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "done" {
		t.Errorf("result = %v, want done", result)
	}

	mu.Lock()
	defer mu.Unlock()

	if captured["traceFunctionKey"] != "order-processing" {
		t.Errorf("traceFunctionKey = %v, want order-processing", captured["traceFunctionKey"])
	}
}

func TestSpan_AllTypes(t *testing.T) {
	server := newSpanCaptureServer(t)
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	types := []string{"llm", "agent", "function", "guardrail", "handoff", "custom"}
	for _, st := range types {
		_, err := client.Span(ctx, "test", func(ctx context.Context) (any, error) {
			return nil, nil
		}, WithType(st))
		if err != nil {
			t.Errorf("unexpected error for type %q: %v", st, err)
		}
	}

	client.FlushTraces(5 * time.Second)
}

func TestSpan_CapturesOutput(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	client.Span(ctx, "test", func(ctx context.Context) (any, error) {
		return map[string]any{"total": 42}, nil
	})

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	rawSpan := captured["rawSpan"].(map[string]any)
	spanData := rawSpan["span_data"].(map[string]any)
	output := spanData["output"].(map[string]any)
	if output["total"] != float64(42) {
		t.Errorf("output total = %v, want 42", output["total"])
	}
}

func TestSpan_WithInput(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	client.Span(ctx, "test", func(ctx context.Context) (any, error) {
		return "result", nil
	}, WithInput("order-123", 99.99))

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	rawSpan := captured["rawSpan"].(map[string]any)
	spanData := rawSpan["span_data"].(map[string]any)
	input := spanData["input"].([]any)
	if len(input) != 2 {
		t.Fatalf("input len = %d, want 2", len(input))
	}
	if input[0] != "order-123" {
		t.Errorf("input[0] = %v, want order-123", input[0])
	}
	if input[1] != 99.99 {
		t.Errorf("input[1] = %v, want 99.99", input[1])
	}
}

func TestSpan_WithInputSingleArg(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	client.Span(ctx, "test", func(ctx context.Context) (any, error) {
		return nil, nil
	}, WithInput("order-123"))

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	rawSpan := captured["rawSpan"].(map[string]any)
	spanData := rawSpan["span_data"].(map[string]any)
	if spanData["input"] != "order-123" {
		t.Errorf("input = %v, want order-123", spanData["input"])
	}
}

func TestStart_BasicExecution(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	ctx, span := client.Start(ctx, "order-service", "ProcessOrder", WithType("function"))
	span.SetInput("order-123")
	span.SetOutput(map[string]any{"total": 100})
	span.End()

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	if captured == nil {
		t.Fatal("no span captured")
	}

	rawSpan := captured["rawSpan"].(map[string]any)
	spanData := rawSpan["span_data"].(map[string]any)

	if spanData["name"] != "ProcessOrder" {
		t.Errorf("name = %v, want ProcessOrder", spanData["name"])
	}
	if spanData["type"] != "function" {
		t.Errorf("type = %v, want function", spanData["type"])
	}
	if spanData["input"] != "order-123" {
		t.Errorf("input = %v, want order-123", spanData["input"])
	}
	output := spanData["output"].(map[string]any)
	if output["total"] != float64(100) {
		t.Errorf("output total = %v, want 100", output["total"])
	}
	if captured["traceFunctionKey"] != "order-service" {
		t.Errorf("traceFunctionKey = %v, want order-service", captured["traceFunctionKey"])
	}

	// Verify context was updated (span should not be nil)
	if currentSpan(ctx) == nil {
		t.Error("expected span in context after Start")
	}
}

func TestStart_CapturesError(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	_, span := client.Start(ctx, "failing-service", "RiskyOp")
	span.SetError(errors.New("something went wrong"))
	span.End()

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	rawSpan := captured["rawSpan"].(map[string]any)
	spanData := rawSpan["span_data"].(map[string]any)
	if spanData["error"] != "something went wrong" {
		t.Errorf("error = %v, want 'something went wrong'", spanData["error"])
	}
}

func TestStart_IdempotentEnd(t *testing.T) {
	var mu sync.Mutex
	var count int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	_, span := client.Start(ctx, "test", "Test")
	span.End()
	span.End() // second call should be a no-op
	span.End() // third call should be a no-op

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	if count != 1 {
		t.Errorf("span sent %d times, want 1", count)
	}
}

func TestStart_NestedSpans(t *testing.T) {
	var mu sync.Mutex
	var payloads []map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		payloads = append(payloads, payload)
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	ctx, outerSpan := client.Start(ctx, "pipeline", "Outer")
	ctx, innerSpan := client.Start(ctx, "pipeline", "Inner")
	innerSpan.End()
	outerSpan.End()

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	if len(payloads) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(payloads))
	}

	// Find inner and outer
	var inner, outer map[string]any
	for _, p := range payloads {
		raw := p["rawSpan"].(map[string]any)
		sd := raw["span_data"].(map[string]any)
		if sd["name"] == "Inner" {
			inner = p
		}
		if sd["name"] == "Outer" {
			outer = p
		}
	}

	if inner == nil || outer == nil {
		t.Fatal("could not find inner/outer spans")
	}

	// Same trace ID
	innerTraceID := inner["rawSpan"].(map[string]any)["trace_id"].(string)
	outerTraceID := outer["rawSpan"].(map[string]any)["trace_id"].(string)
	if innerTraceID != outerTraceID {
		t.Errorf("trace IDs differ: %q vs %q", innerTraceID, outerTraceID)
	}

	// Inner has parent_id pointing to outer
	innerParentID := inner["rawSpan"].(map[string]any)["parent_id"].(string)
	outerID := outer["rawSpan"].(map[string]any)["id"].(string)
	if innerParentID != outerID {
		t.Errorf("inner parent_id = %v, want %v", innerParentID, outerID)
	}
}

func TestFunction_Start(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	fn := client.GetFunction("order-processing")
	ctx := context.Background()

	_, span := fn.Start(ctx, "ProcessOrder", WithType("function"))
	span.SetInput("order-456")
	span.SetOutput("done")
	span.End()

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	if captured["traceFunctionKey"] != "order-processing" {
		t.Errorf("traceFunctionKey = %v, want order-processing", captured["traceFunctionKey"])
	}
	rawSpan := captured["rawSpan"].(map[string]any)
	spanData := rawSpan["span_data"].(map[string]any)
	if spanData["name"] != "ProcessOrder" {
		t.Errorf("name = %v, want ProcessOrder", spanData["name"])
	}
}

func TestDisabled_SpanExecutesButDoesNotSend(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := NewClient("test-key", WithServiceURL(server.URL), WithEnabled(false))
	ctx := context.Background()

	result, err := client.Span(ctx, "test-service", func(ctx context.Context) (any, error) {
		return "executed", nil
	})

	client.FlushTraces(1 * time.Second)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "executed" {
		t.Errorf("result = %v, want executed", result)
	}
	if requestCount != 0 {
		t.Errorf("sent %d requests, want 0", requestCount)
	}
}

func TestDisabled_StartReturnsNoOpSpan(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := NewClient("test-key", WithServiceURL(server.URL), WithEnabled(false))
	ctx := context.Background()

	_, span := client.Start(ctx, "test-service", "TestSpan", WithType("function"))
	span.SetInput("hello")
	span.SetOutput("world")
	span.End()

	client.FlushTraces(1 * time.Second)

	if requestCount != 0 {
		t.Errorf("sent %d requests, want 0", requestCount)
	}
}

func TestEnabled_DefaultsToTrue(t *testing.T) {
	client := NewClient("test-key")
	if !client.enabled {
		t.Error("enabled should default to true")
	}
}

func TestSpan_UnserializableOutput_DoesNotCrash(t *testing.T) {
	server := newSpanCaptureServer(t)
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	// Channels cannot be serialized by json.Marshal — this must not crash the app
	result, err := client.Span(ctx, "test", func(ctx context.Context) (any, error) {
		return map[string]any{"ch": make(chan int)}, nil
	})

	client.FlushTraces(5 * time.Second)

	if err != nil {
		t.Fatalf("span should not return error for unserializable output: %v", err)
	}
	// The result should still be returned to the caller
	m := result.(map[string]any)
	if m["ch"] == nil {
		t.Error("result should contain the channel value")
	}
}

func TestStart_UnserializableOutput_DoesNotCrash(t *testing.T) {
	server := newSpanCaptureServer(t)
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	_, span := client.Start(ctx, "test", "Test")
	span.SetOutput(map[string]any{"fn": func() {}})
	// End() must not panic even though func() can't be serialized
	span.End()

	client.FlushTraces(5 * time.Second)
}

func TestSpan_ServerDown_ReturnsResult(t *testing.T) {
	// Point at a server that isn't listening — span sending will fail,
	// but the user's result and error must still be returned.
	client := newTestClient("http://127.0.0.1:1")
	ctx := context.Background()

	result, err := client.Span(ctx, "test", func(ctx context.Context) (any, error) {
		return "my-result", nil
	})

	client.FlushTraces(1 * time.Second)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "my-result" {
		t.Errorf("result = %v, want my-result", result)
	}
}

func TestSpan_ServerDown_ReturnsUserError(t *testing.T) {
	client := newTestClient("http://127.0.0.1:1")
	ctx := context.Background()

	result, err := client.Span(ctx, "test", func(ctx context.Context) (any, error) {
		return nil, errors.New("user error")
	})

	client.FlushTraces(1 * time.Second)

	if err == nil || err.Error() != "user error" {
		t.Fatalf("err = %v, want 'user error'", err)
	}
	if result != nil {
		t.Errorf("result = %v, want nil", result)
	}
}

func TestSpan_WithMetadata(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	client.Span(ctx, "test", func(ctx context.Context) (any, error) {
		return "result", nil
	}, WithMetadata(map[string]any{"user_id": "u-123", "region": "us-east"}))

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	rawSpan := captured["rawSpan"].(map[string]any)
	spanData := rawSpan["span_data"].(map[string]any)
	metadata := spanData["metadata"].(map[string]any)
	if metadata["user_id"] != "u-123" {
		t.Errorf("metadata user_id = %v, want u-123", metadata["user_id"])
	}
	if metadata["region"] != "us-east" {
		t.Errorf("metadata region = %v, want us-east", metadata["region"])
	}
}

func TestSpan_NoMetadata(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	client.Span(ctx, "test", func(ctx context.Context) (any, error) {
		return "result", nil
	})

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	rawSpan := captured["rawSpan"].(map[string]any)
	spanData := rawSpan["span_data"].(map[string]any)
	if _, ok := spanData["metadata"]; ok {
		t.Error("metadata should not be present when not set")
	}
}

func TestStart_WithMetadata(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	_, span := client.Start(ctx, "test", "TestSpan", WithType("function"))
	span.SetMetadata(map[string]any{"request_id": "req-456", "env": "staging"})
	span.End()

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	rawSpan := captured["rawSpan"].(map[string]any)
	spanData := rawSpan["span_data"].(map[string]any)
	metadata := spanData["metadata"].(map[string]any)
	if metadata["request_id"] != "req-456" {
		t.Errorf("metadata request_id = %v, want req-456", metadata["request_id"])
	}
	if metadata["env"] != "staging" {
		t.Errorf("metadata env = %v, want staging", metadata["env"])
	}
}

func TestStart_MetadataMerge(t *testing.T) {
	var mu sync.Mutex
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		captured = payload
		mu.Unlock()
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client := newTestClient(server.URL)
	ctx := context.Background()

	_, span := client.Start(ctx, "test", "TestSpan", WithType("function"),
		WithMetadata(map[string]any{"user_id": "u-123", "region": "us-east"}))
	span.SetMetadata(map[string]any{"region": "eu-west", "request_id": "req-789"})
	span.End()

	client.FlushTraces(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	rawSpan := captured["rawSpan"].(map[string]any)
	spanData := rawSpan["span_data"].(map[string]any)
	metadata := spanData["metadata"].(map[string]any)
	if metadata["user_id"] != "u-123" {
		t.Errorf("metadata user_id = %v, want u-123", metadata["user_id"])
	}
	if metadata["region"] != "eu-west" {
		t.Errorf("metadata region = %v, want eu-west (runtime should win)", metadata["region"])
	}
	if metadata["request_id"] != "req-789" {
		t.Errorf("metadata request_id = %v, want req-789", metadata["request_id"])
	}
}

func TestNewClient_EmptyAPIKeyAutoDisables(t *testing.T) {
	client := NewClient("")
	if client.enabled {
		t.Error("client with empty apiKey should be auto-disabled")
	}
}

func TestNewClient_WhitespaceAPIKeyAutoDisables(t *testing.T) {
	client := NewClient("   ")
	if client.enabled {
		t.Error("client with whitespace apiKey should be auto-disabled")
	}
}

func TestSpan_EmptyAPIKeyRunsFunctionSilently(t *testing.T) {
	client := NewClient("")
	ctx := context.Background()

	result, err := client.Span(ctx, "test", func(ctx context.Context) (any, error) {
		return "result", nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "result" {
		t.Errorf("result = %v, want result", result)
	}
}

func TestStart_EmptyAPIKeyReturnsNoOpSpan(t *testing.T) {
	client := NewClient("")
	ctx := context.Background()

	_, span := client.Start(ctx, "test", "Test")
	span.End() // should not panic
}

func TestNewClient_ExplicitDisabledWithEmptyAPIKeyStaysDisabled(t *testing.T) {
	client := NewClient("", WithEnabled(false))
	if client.enabled {
		t.Error("explicitly disabled client should stay disabled")
	}
}

func newSpanCaptureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
}
