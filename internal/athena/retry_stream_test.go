package athena

import (
	"encoding/json"
	"testing"
)

func TestPiRetryUsesTerminalAssistantOutcome(t *testing.T) {
	message := func(stop string, tool bool) string {
		content := []map[string]string{{"type": "text", "text": `{"version":2,"actions":[]}`}}
		if tool {
			content = append(content, map[string]string{"type": "toolCall", "text": "forbidden"})
		}
		b, _ := json.Marshal(map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "stopReason": stop, "content": content}})
		return string(b) + "\n"
	}
	end := "{\"type\":\"agent_end\"}\n"
	retry := "{\"type\":\"auto_retry_start\"}\n{\"type\":\"agent_start\"}\n"
	good, failed := message("stop", false), message("error", false)
	for _, tc := range []struct {
		name, stream string
		ok           bool
	}{
		{"success", good + end, true},
		{"recovered", failed + end + retry + good + end, true},
		{"recovered-twice", failed + end + retry + failed + end + retry + good + end, true},
		{"final-error", good + end + retry + failed + end, false},
		{"incomplete-retry", good + end + retry, false},
		{"missing-final-end", failed + end + retry + good, false},
		{"missing-message-end", good + end + retry + end, false},
		{"truncated", message("length", false) + end, false},
		{"tool-in-failed-attempt", message("error", true) + end + retry + good + end, false},
		{"tool-execution", "{\"type\":\"tool_execution_start\"}\n" + good + end, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseEvents([]byte(tc.stream))
			if (err == nil) != tc.ok {
				t.Fatalf("success=%v, error=%v", tc.ok, err)
			}
		})
	}
}
