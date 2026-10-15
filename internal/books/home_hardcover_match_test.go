package books

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHardcoverMatchLocal(t *testing.T) {
	results := map[string]any{"hits": []any{
		map[string]any{"document": map[string]any{"id": "77", "title": "Dragonwings", "author_names": []string{"Laurence Yep"}, "release_year": 1975}},
		map[string]any{"document": map[string]any{"id": "42", "title": "Dragon Wing", "author_names": []string{"Margaret Weis", "Tracy Hickman"}, "release_year": 1990,
			"image": map[string]any{"url": "https://assets.hardcover.app/x.jpg"}, "description": "The first Death Gate book."}},
		map[string]any{"document": map[string]any{"id": "43", "title": "Dragonwing", "author_names": []string{"Margaret Weis"}, "release_year": 1990}},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "search(") {
			t.Errorf("unexpected query %s", body)
		}
		raw, _ := json.Marshal(results)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"search": map[string]any{"results": json.RawMessage(raw)}}})
	}))
	defer srv.Close()
	h := NewHardcover("token", "test")
	h.Base = srv.URL
	m, err := h.MatchLocal(context.Background(), LocalBook{Author: "Margaret Weis", Title: "Dragonwing"})
	if err != nil || m == nil {
		t.Fatalf("MatchLocal = %v, %v", m, err)
	}
	if mm, _ := h.MatchLocal(context.Background(), LocalBook{Author: "Tracy Hickman", Title: "Dragonwing"}); mm == nil || mm.Key != "HC42" {
		t.Errorf("Dragonwing by Tracy Hickman should match Dragon Wing (HC42), got %+v", mm)
	}
	if !LooksSwapped("The Light Fantastic", "Terry Pratchett") || LooksSwapped("Terry Pratchett", "The Light Fantastic") || LooksSwapped("Margaret Weis", "Dragon Wing") {
		t.Error("LooksSwapped")
	}
	if m.Key != "HC43" || m.Title != "Dragonwing" {
		t.Errorf("matched %s %q, want HC43 Dragonwing", m.Key, m.Title)
	}
	if !IsHardcoverKey(m.Key) || IsHardcoverKey("OL123W") {
		t.Error("IsHardcoverKey")
	}
}
