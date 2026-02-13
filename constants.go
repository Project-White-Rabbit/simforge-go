package simforge

// DefaultServiceURL is the default Simforge API base URL.
const DefaultServiceURL = "https://simforge.goharvest.ai"

// Version is the SDK version string sent with every API request.
const Version = "0.6.0"

// Valid span types matching the backend enum.
var validSpanTypes = map[string]bool{
	"llm":       true,
	"agent":     true,
	"function":  true,
	"guardrail": true,
	"handoff":   true,
	"custom":    true,
}
