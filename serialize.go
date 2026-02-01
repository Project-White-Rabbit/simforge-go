package simforge

import "encoding/json"

// MarshalSpanPayload serializes a span payload to JSON bytes, matching what
// the HTTP client does before sending to the API.
func MarshalSpanPayload(payload map[string]any) ([]byte, error) {
	return json.Marshal(payload)
}

// UnmarshalSpanPayload deserializes JSON bytes back into the target type T.
// This proves that serialized span data can be restored to its original Go type.
func UnmarshalSpanPayload[T any](data []byte) (T, error) {
	var result T
	err := json.Unmarshal(data, &result)
	return result, err
}
