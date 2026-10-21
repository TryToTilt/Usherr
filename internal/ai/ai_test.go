package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The shared part goes after the system prompt, marked for caching; the
// question itself stays in the message.
func TestClaudeCachesTheSharedPart(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
	}))
	defer srv.Close()
	old := claudeURL
	claudeURL = srv.URL
	defer func() { claudeURL = old }()

	ans, err := Ask(context.Background(), Config{Provider: Claude, Key: "k", Model: "claude-haiku-5-5"},
		Request{System: "You program TV.", Shared: "the library", Prompt: "fill slot 3", MaxTokens: 100})
	if err != nil || ans != "ok" {
		t.Fatalf("%q %v", ans, err)
	}
	sys, _ := got["system"].([]any)
	if len(sys) != 2 {
		t.Fatalf("system: %v", got["system"])
	}
	first, second := sys[0].(map[string]any), sys[1].(map[string]any)
	if first["text"] != "You program TV." || first["cache_control"] != nil {
		t.Fatalf("first block: %v", first)
	}
	cc, _ := second["cache_control"].(map[string]any)
	if second["text"] != "the library" || cc["type"] != "ephemeral" {
		t.Fatalf("shared block: %v", second)
	}
	msgs := got["messages"].([]any)
	content := msgs[0].(map[string]any)["content"].([]any)
	if content[len(content)-1].(map[string]any)["text"] != "fill slot 3" {
		t.Fatalf("the question: %v", content)
	}
}
