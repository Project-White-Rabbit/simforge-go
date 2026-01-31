package simforge

import (
	"testing"
	"time"
)

func TestSerializeValue_Nil(t *testing.T) {
	if got := serializeValue(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestSerializeValue_Primitives(t *testing.T) {
	tests := []struct {
		input any
		want  any
	}{
		{"hello", "hello"},
		{true, true},
		{false, false},
		{42, 42},
		{int64(100), int64(100)},
		{3.14, 3.14},
		{float32(2.5), float64(2.5)},
	}

	for _, tt := range tests {
		got := serializeValue(tt.input)
		if got != tt.want {
			t.Errorf("serializeValue(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestSerializeValue_Time(t *testing.T) {
	ts := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	got := serializeValue(ts)
	want := "2024-01-15T10:30:00.000Z"
	if got != want {
		t.Errorf("serializeValue(time) = %q, want %q", got, want)
	}
}

func TestSerializeValue_Slice(t *testing.T) {
	input := []any{"a", 1, true}
	got := serializeValue(input).([]any)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0] != "a" || got[1] != 1 || got[2] != true {
		t.Errorf("unexpected values: %v", got)
	}
}

func TestSerializeValue_Map(t *testing.T) {
	input := map[string]any{"name": "Alice", "age": 30}
	got := serializeValue(input).(map[string]any)
	if got["name"] != "Alice" || got["age"] != 30 {
		t.Errorf("unexpected values: %v", got)
	}
}

type testStruct struct {
	Name  string `json:"name"`
	Age   int    `json:"age"`
	Email string `json:"email,omitempty"`
}

func TestSerializeValue_Struct(t *testing.T) {
	input := testStruct{Name: "Alice", Age: 30, Email: "alice@example.com"}
	got := serializeValue(input).(map[string]any)
	if got["name"] != "Alice" {
		t.Errorf("name = %v, want Alice", got["name"])
	}
	if got["age"] != 30 {
		t.Errorf("age = %v, want 30", got["age"])
	}
}

type testStringer struct{}

func (testStringer) String() string { return "custom-string" }

func TestSerializeValue_Stringer(t *testing.T) {
	got := serializeValue(testStringer{})
	if got != "custom-string" {
		t.Errorf("got %v, want custom-string", got)
	}
}

func TestSerializeValue_Pointer(t *testing.T) {
	s := "hello"
	got := serializeValue(&s)
	if got != "hello" {
		t.Errorf("got %v, want hello", got)
	}
}

func TestSerializeValue_NilPointer(t *testing.T) {
	var s *string
	got := serializeValue(s)
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestSerializeInputs(t *testing.T) {
	args := []any{"hello", 42, true}
	got := serializeInputs(args)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0] != "hello" || got[1] != 42 || got[2] != true {
		t.Errorf("unexpected values: %v", got)
	}
}

type jsonIgnoreStruct struct {
	Name   string `json:"name"`
	Secret string `json:"-"`
}

func TestSerializeValue_JsonIgnoreTag(t *testing.T) {
	input := jsonIgnoreStruct{Name: "Alice", Secret: "hidden"}
	got := serializeValue(input).(map[string]any)
	if _, ok := got["Secret"]; ok {
		t.Error("expected Secret to be omitted via json:\"-\" tag")
	}
	if got["name"] != "Alice" {
		t.Errorf("name = %v, want Alice", got["name"])
	}
}
