package router

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeUserMessage(t *testing.T) {
	b, err := encodeUserMessage("line1\n\"quoted\" 日本語")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(b), "\n") {
		t.Fatal("message must be newline-terminated (stream-json is line-delimited)")
	}
	var m struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Type != "user" || m.Message.Role != "user" {
		t.Fatalf("type/role = %q/%q", m.Type, m.Message.Role)
	}
	if len(m.Message.Content) != 1 || m.Message.Content[0].Text != "line1\n\"quoted\" 日本語" {
		t.Fatalf("content = %+v", m.Message.Content)
	}
}

func TestParseResultLine(t *testing.T) {
	res, ok := parseResultLine([]byte(`{"type":"result","subtype":"success","is_error":false,"result":"haiku","duration_ms":3081}` + "\n"))
	if !ok || res.Result != "haiku" || res.IsError {
		t.Fatalf("parseResultLine = (%+v, %v)", res, ok)
	}
	if _, ok := parseResultLine([]byte(`{"type":"assistant","message":{}}`)); ok {
		t.Fatal("non-result lines must be skipped")
	}
	if _, ok := parseResultLine([]byte("not json")); ok {
		t.Fatal("garbage must be skipped")
	}
	res, ok = parseResultLine([]byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":""}`))
	if !ok || !res.IsError {
		t.Fatalf("error result = (%+v, %v)", res, ok)
	}
}

func TestParseToolResult(t *testing.T) {
	// Real shape from codex mcp-server 0.128.0.
	got, err := parseToolResult([]byte(`{"content":[{"type":"text","text":"mini"}],"structuredContent":{"threadId":"019eb5a2","content":"mini"}}`))
	if err != nil || got != "mini" {
		t.Fatalf("parseToolResult = (%q, %v)", got, err)
	}
	// structuredContent missing → falls back to content[0].text.
	got, err = parseToolResult([]byte(`{"content":[{"type":"text","text":"full"}]}`))
	if err != nil || got != "full" {
		t.Fatalf("fallback = (%q, %v)", got, err)
	}
	if _, err = parseToolResult([]byte(`{"isError":true,"content":[{"type":"text","text":"boom"}]}`)); err == nil {
		t.Fatal("isError result must fail")
	}
	if _, err = parseToolResult([]byte(`not json`)); err == nil {
		t.Fatal("garbage must fail")
	}
}
