// Package ai asks a language model (home fork): a local one through Ollama
// (or any OpenAI-compatible server, such as LM Studio), or Claude through
// Anthropic's API. Mediarium's AI features use it to suggest and explain;
// they never act on what it says without a person confirming, and they send
// it only what a question needs (titles as "Name (year)", never accounts).
package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Providers.
const (
	Ollama = "ollama" // Ollama's own API (local models, photos, JSON answers)
	OpenAI = "openai" // any OpenAI-compatible server (LM Studio, vLLM, OpenAI…)
	Claude = "claude" // Anthropic's API
)

// Defaults for a new setup.
const (
	DefaultOllamaURL   = "http://host.docker.internal:11434"
	DefaultOllamaModel = "gemma3:4b"
	DefaultClaudeModel = "claude-sonnet-5-5"
	claudeURL          = "https://api.anthropic.com/v1/messages"
)

// Config is where the model is and which one.
type Config struct {
	Provider string
	URL      string // Ollama's or the OpenAI-compatible server's address
	Model    string
	Key      string // Claude's (or the OpenAI-compatible server's) API key
}

// Image is a picture sent with the question.
type Image struct {
	MIME string // image/jpeg, image/png…
	Data []byte
}

// Request is one question.
type Request struct {
	System    string
	Prompt    string
	Images    []Image
	JSON      bool // the answer must be a JSON object
	MaxTokens int
}

var client = &http.Client{Timeout: 3 * time.Minute}

// Ask sends the question and returns the model's answer.
func Ask(ctx context.Context, cfg Config, req Request) (string, error) {
	if req.MaxTokens <= 0 {
		req.MaxTokens = 800
	}
	ans, cut, err := ask(ctx, cfg, req)
	if err != nil || !req.JSON {
		return ans, err
	}
	// A JSON answer cut off for room (a long reading order, many picks), or
	// one that doesn't read: ask once more with twice the room.
	if cut || !readable(ans) {
		log.Printf("ai: %s answer %s (%d tokens), asking again with more room", cfg.Provider, map[bool]string{true: "was cut off", false: "couldn't be read"}[cut], req.MaxTokens)
		again := req
		again.MaxTokens = min(req.MaxTokens*2, 8000)
		again.Prompt += "\n\nReply with the JSON object only, and keep each text short."
		if ans2, _, err2 := ask(ctx, cfg, again); err2 == nil && readable(ans2) {
			return ans2, nil
		} else if err2 == nil {
			log.Printf("ai: %s answer still unreadable: %.300s", cfg.Provider, ans2)
		}
	}
	return ans, nil
}

// ask sends one question; cut is true when the answer stopped for lack of room.
func ask(ctx context.Context, cfg Config, req Request) (string, bool, error) {
	switch cfg.Provider {
	case Ollama:
		return askOllama(ctx, cfg, req)
	case OpenAI:
		return askOpenAI(ctx, cfg, req)
	case Claude:
		return askClaude(ctx, cfg, req)
	}
	return "", false, errors.New("no AI is set up")
}

// readable reports whether an answer holds a JSON object.
func readable(ans string) bool {
	var v map[string]any
	return json.Unmarshal([]byte(JSONObject(ans)), &v) == nil
}

func post(ctx context.Context, url string, headers map[string]string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	resp, err := client.Do(r)
	if err != nil {
		return fmt.Errorf("couldn't reach the AI: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("the AI answered %d: %s", resp.StatusCode, errorText(data))
	}
	return json.Unmarshal(data, out)
}

// errorText pulls the message out of a provider's error answer.
func errorText(b []byte) string {
	var e struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != nil {
		switch v := e.Error.(type) {
		case string:
			return v
		case map[string]any:
			if m, ok := v["message"].(string); ok {
				return m
			}
		}
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func trimURL(u string) string { return strings.TrimRight(strings.TrimSpace(u), "/") }

func askOllama(ctx context.Context, cfg Config, req Request) (string, bool, error) {
	user := map[string]any{"role": "user", "content": req.Prompt}
	if len(req.Images) > 0 {
		imgs := make([]string, len(req.Images))
		for i, im := range req.Images {
			imgs[i] = base64.StdEncoding.EncodeToString(im.Data)
		}
		user["images"] = imgs
	}
	msgs := []map[string]any{}
	if req.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": req.System})
	}
	msgs = append(msgs, user)
	body := map[string]any{
		"model": cfg.Model, "messages": msgs, "stream": false, "keep_alive": "10m",
		"options": map[string]any{"temperature": 0.3, "num_predict": req.MaxTokens, "num_ctx": ollamaContext(req)},
	}
	if req.JSON {
		body["format"] = "json"
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		DoneReason string `json:"done_reason"`
	}
	if err := post(ctx, trimURL(cfg.URL)+"/api/chat", nil, body, &out); err != nil {
		return "", false, err
	}
	return out.Message.Content, out.DoneReason == "length", nil
}

// ollamaContext is the context window a question needs: Ollama's default is
// small and it silently drops what doesn't fit (a long list of albums came
// back as "{}"). About 3 characters a token, plus the answer and photos.
func ollamaContext(req Request) int {
	n := (len(req.System)+len(req.Prompt))/3 + req.MaxTokens + 512 + 800*len(req.Images)
	switch {
	case n < 4096:
		return 4096
	case n > 32768:
		return 32768
	}
	return (n + 1023) / 1024 * 1024
}

func askOpenAI(ctx context.Context, cfg Config, req Request) (string, bool, error) {
	var content any = req.Prompt
	if len(req.Images) > 0 {
		parts := []map[string]any{{"type": "text", "text": req.Prompt}}
		for _, im := range req.Images {
			parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]string{
				"url": "data:" + im.MIME + ";base64," + base64.StdEncoding.EncodeToString(im.Data),
			}})
		}
		content = parts
	}
	msgs := []map[string]any{}
	if req.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": req.System})
	}
	msgs = append(msgs, map[string]any{"role": "user", "content": content})
	body := map[string]any{"model": cfg.Model, "messages": msgs, "temperature": 0.3, "max_tokens": req.MaxTokens}
	if req.JSON {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	url := trimURL(cfg.URL)
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	headers := map[string]string{}
	if cfg.Key != "" {
		headers["Authorization"] = "Bearer " + cfg.Key
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := post(ctx, url+"/chat/completions", headers, body, &out); err != nil {
		return "", false, err
	}
	if len(out.Choices) == 0 {
		return "", false, errors.New("the AI gave no answer")
	}
	return out.Choices[0].Message.Content, out.Choices[0].FinishReason == "length", nil
}

func askClaude(ctx context.Context, cfg Config, req Request) (string, bool, error) {
	if cfg.Key == "" {
		return "", false, errors.New("Claude needs an API key")
	}
	var parts []map[string]any
	for _, im := range req.Images {
		parts = append(parts, map[string]any{"type": "image", "source": map[string]string{
			"type": "base64", "media_type": im.MIME, "data": base64.StdEncoding.EncodeToString(im.Data),
		}})
	}
	prompt := req.Prompt
	if req.JSON {
		prompt += "\n\nAnswer with the JSON object only."
	}
	parts = append(parts, map[string]any{"type": "text", "text": prompt})
	body := map[string]any{
		"model": cfg.Model, "max_tokens": req.MaxTokens,
		"messages": []map[string]any{{"role": "user", "content": parts}},
	}
	if req.System != "" {
		body["system"] = req.System
	}
	headers := map[string]string{"x-api-key": cfg.Key, "anthropic-version": "2023-06-01"}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := post(ctx, claudeURL, headers, body, &out); err != nil {
		return "", false, err
	}
	var sb strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), out.StopReason == "max_tokens", nil
}

// JSONObject finds the JSON object in an answer: models sometimes wrap it in
// a code fence or a sentence.
func JSONObject(answer string) string {
	start := strings.Index(answer, "{")
	end := strings.LastIndex(answer, "}")
	if start < 0 || end < start {
		return ""
	}
	return answer[start : end+1]
}
