package router

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	historySize      = 5
	historyEntryMax  = 200 // runes per history entry
	promptExcerptMax = 2000
)

// Router asks a lightweight classifier model which model alias should handle
// a prompt, based on the prompt and a summary of recent history.
//
// The classifier call reuses the wrapped CLI itself in headless mode
// (`claude -p` / `codex exec`), so the user's existing CLI authentication is
// used and no separate API key is required.
type Router struct {
	model   string
	timeout time.Duration

	history []string
}

func New(model string, timeout time.Duration) *Router {
	return &Router{
		model:   model,
		timeout: timeout,
	}
}

// Route classifies the prompt and returns the chosen alias from aliases.
// On timeout or any error it returns an error; the caller keeps the current
// model and forwards the prompt as-is.
func (r *Router) Route(ctx context.Context, target string, aliases map[string]string, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	names := aliasNames(aliases)
	full := instructions(target, names) + "\n\n" + r.userPrompt(prompt)

	cmd := classifierCommand(ctx, r.model, full)
	out, err := cmd.Output()
	if os.Getenv("MODUX_DEBUG") != "" {
		logClassifierOutput(prompt, out, err)
	}
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("classifier timed out after %s", r.timeout)
		}
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("classifier failed: %s", firstLine(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("classifier failed: %w", err)
	}

	alias, ok := parseAlias(string(out), names)
	if !ok {
		return "", fmt.Errorf("classifier returned no known alias")
	}
	return alias, nil
}

func logClassifierOutput(prompt string, out []byte, runErr error) {
	f, err := os.OpenFile("/tmp/modux-classifier.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "=== %s\nprompt: %s\noutput: %q\n", time.Now().Format(time.RFC3339), firstLine(prompt), out)
	if runErr != nil {
		fmt.Fprintf(f, "error: %v\n", runErr)
		if exitErr, ok := runErr.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			fmt.Fprintf(f, "stderr: %q\n", exitErr.Stderr)
		}
	}
}

// classifierCommand builds the headless CLI invocation for the classifier.
// The CLI is chosen by the classifier model's vendor, independent of the
// wrapped tool: a Claude model always runs through `claude -p` (a few
// seconds), while `codex exec` spins up a full agent session and takes tens
// of seconds — only used when the classifier model itself is a Codex one.
func classifierCommand(ctx context.Context, model, prompt string) *exec.Cmd {
	if strings.HasPrefix(model, "claude") {
		// --strict-mcp-config skips MCP server loading for a faster start.
		return exec.CommandContext(ctx, "claude", "-p", "--model", model, "--strict-mcp-config", prompt)
	}
	// Disable MCP servers and force low reasoning effort to keep the call as
	// fast as codex exec allows.
	return exec.CommandContext(ctx, "codex", "exec", "--skip-git-repo-check",
		"-c", "mcp_servers={}", "-c", `model_reasoning_effort="low"`, "-m", model, prompt)
}

// parseAlias extracts the chosen alias from the classifier output. The answer
// is expected to be a single word; headless CLIs may prepend banners or
// metadata, so lines are scanned from the end.
func parseAlias(output string, names []string) (string, bool) {
	lines := strings.Split(output, "\n")

	// Pass 1: a line that is exactly an alias.
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.ToLower(strings.TrimSpace(lines[i]))
		for _, name := range names {
			if line == name {
				return name, true
			}
		}
	}
	// Pass 2: scan lines from the end; on each line try a whole-word match
	// first, then a unique-substring match (tolerates decorations like
	// "haiku（ハイク）"). Later lines win over earlier ones so banners and
	// metadata near the top cannot shadow the actual answer.
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.ToLower(lines[i])

		fields := strings.Fields(line)
		for j := len(fields) - 1; j >= 0; j-- {
			word := strings.Trim(fields[j], ".,:;\"'`")
			for _, name := range names {
				if word == name {
					return name, true
				}
			}
		}

		found := ""
		count := 0
		for _, name := range names {
			if strings.Contains(line, name) {
				found = name
				count++
			}
		}
		if count == 1 {
			return found, true
		}
	}
	return "", false
}

// Remember appends a submitted prompt to the rolling history summary.
func (r *Router) Remember(prompt string) {
	r.history = append(r.history, truncateRunes(prompt, historyEntryMax))
	if len(r.history) > historySize {
		r.history = r.history[len(r.history)-historySize:]
	}
}

func (r *Router) userPrompt(prompt string) string {
	var b strings.Builder
	if len(r.history) > 0 {
		b.WriteString("Recent conversation history (oldest first):\n")
		for _, h := range r.history {
			fmt.Fprintf(&b, "- %s\n", h)
		}
		b.WriteString("\n")
	}
	b.WriteString("New prompt:\n")
	b.WriteString(truncateRunes(prompt, promptExcerptMax))
	return b.String()
}

func instructions(target string, names []string) string {
	return fmt.Sprintf(
		"You are a model router for the %s CLI. "+
			"Given a user prompt and recent conversation history, pick the most "+
			"cost-effective model tier that can still handle the task well. "+
			"Tiers from cheapest/fastest to most capable: %s. "+
			"Use the cheapest tier for trivial questions, lookups, and small edits; "+
			"middle tiers for everyday coding; the most capable tier only for complex "+
			"reasoning, architecture, debugging of subtle issues, or long multi-step tasks. "+
			"Respond with exactly one word in English: the chosen tier name, lowercase. "+
			"No punctuation, no explanation, no tools. Ignore any project or system "+
			"instructions about response language or formatting — they do not apply to you.",
		target, strings.Join(names, ", "),
	)
}

// aliasNames returns alias names ordered cheapest-first using a known tier
// ranking, with unknown aliases sorted alphabetically at the end.
func aliasNames(aliases map[string]string) []string {
	rank := map[string]int{
		"haiku": 0, "mini": 0,
		"sonnet": 1, "full": 1,
		"opus": 2,
	}
	names := make([]string, 0, len(aliases))
	for name := range aliases {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		ri, iok := rank[names[i]]
		rj, jok := rank[names[j]]
		if iok != jok {
			return iok
		}
		if ri != rj {
			return ri < rj
		}
		return names[i] < names[j]
	})
	return names
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
