package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ModelChoice represents the classifier's recommended model.
type ModelChoice struct {
	Model  string
	Reason string
}

// Classifier selects the optimal model for a given prompt.
type Classifier struct {
	apiKey string
	model  string
	client *http.Client
}

func New(apiKey, model string) *Classifier {
	return &Classifier{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type response struct {
	Content []contentBlock `json:"content"`
}

// Classify analyzes the prompt and returns the best model to use.
// It returns model IDs suitable for the /model command in claude/codex CLI.
func (c *Classifier) Classify(ctx context.Context, target, prompt string) (*ModelChoice, error) {
	systemPrompt := buildSystemPrompt(target)

	reqBody := request{
		Model:     c.model,
		MaxTokens: 128,
		Messages: []message{
			{
				Role:    "user",
				Content: fmt.Sprintf("Analyze this prompt and return the best model:\n\n%s", prompt),
			},
		},
	}

	// Prepend system prompt via a system role isn't supported in all APIs;
	// use the messages API system field instead.
	type fullRequest struct {
		Model     string    `json:"model"`
		MaxTokens int       `json:"max_tokens"`
		System    string    `json:"system"`
		Messages  []message `json:"messages"`
	}
	full := fullRequest{
		Model:     c.model,
		MaxTokens: 128,
		System:    systemPrompt,
		Messages:  reqBody.Messages,
	}

	body, err := json.Marshal(full)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("classifier API returned %d", resp.StatusCode)
	}

	var apiResp response
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("empty response from classifier")
	}

	return parseChoice(apiResp.Content[0].Text, target), nil
}

func buildSystemPrompt(target string) string {
	switch target {
	case "claude":
		return `You are a model selector. Given a user prompt, choose the most cost-effective Claude model.
Return ONLY a JSON object like: {"model": "claude-opus-4-8", "reason": "..."}

Available models (cheapest to most capable):
- claude-haiku-4-5-20251001: simple tasks, quick lookups, formatting
- claude-sonnet-4-6: moderate complexity, coding, analysis
- claude-opus-4-8: complex reasoning, architecture, critical decisions

Choose the LEAST capable model that can handle the task well.`
	case "codex":
		return `You are a model selector. Given a user prompt, choose the most cost-effective OpenAI model.
Return ONLY a JSON object like: {"model": "o4-mini", "reason": "..."}

Available models (cheapest to most capable):
- gpt-4o-mini: simple tasks, quick lookups, formatting
- gpt-4o: moderate complexity, coding, analysis
- o4-mini: reasoning tasks, complex logic
- o3: most complex reasoning, architecture

Choose the LEAST capable model that can handle the task well.`
	default:
		return "Choose the best model for the task. Return JSON: {\"model\": \"...\", \"reason\": \"...\"}"
	}
}

type choiceJSON struct {
	Model  string `json:"model"`
	Reason string `json:"reason"`
}

func parseChoice(text, target string) *ModelChoice {
	text = strings.TrimSpace(text)

	// Try to extract JSON even if there's surrounding text.
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		text = text[start : end+1]
	}

	var c choiceJSON
	if err := json.Unmarshal([]byte(text), &c); err == nil && c.Model != "" {
		return &ModelChoice{Model: c.Model, Reason: c.Reason}
	}

	// Fallback to safe defaults.
	switch target {
	case "claude":
		return &ModelChoice{Model: "claude-sonnet-4-6", Reason: "fallback default"}
	default:
		return &ModelChoice{Model: "gpt-4o-mini", Reason: "fallback default"}
	}
}
