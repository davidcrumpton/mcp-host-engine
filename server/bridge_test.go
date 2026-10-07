// server/bridge_test.go
package server

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMarshalNoEscapeKeepsAmpersands(t *testing.T) {
	got, err := marshalNoEscape(map[string]interface{}{"command": "apt-get update && apt-get install -y x"})
	if err != nil {
		t.Fatalf("marshalNoEscape: %v", err)
	}
	want := `{"command":"apt-get update && apt-get install -y x"}`
	if string(got) != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestMarshalNoEscapeKeepsAngleBrackets(t *testing.T) {
	got, err := marshalNoEscape(map[string]interface{}{"code": "if (a < b && c > d)"})
	if err != nil {
		t.Fatalf("marshalNoEscape: %v", err)
	}
	for _, escaped := range []string{`\u0026`, `\u003c`, `\u003e`} {
		if strings.Contains(string(got), escaped) {
			t.Fatalf("output %s unexpectedly contains %s", got, escaped)
		}
	}
}

func TestResultToSDKDoesNotEscapeAmpersands(t *testing.T) {
	res := ResultToSDK(map[string]interface{}{"stdout": "if (a && b)"})
	if len(res.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(res.Content))
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", res.Content[0])
	}
	if strings.Contains(tc.Text, `\u0026`) {
		t.Fatalf("text content %q still contains escaped ampersands", tc.Text)
	}
	if tc.Text != `{"stdout":"if (a && b)"}` {
		t.Fatalf("unexpected text content: %q", tc.Text)
	}
}

func TestResultToSDKPassesStringsThrough(t *testing.T) {
	res := ResultToSDK("plain && string")
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", res.Content[0])
	}
	if tc.Text != "plain && string" {
		t.Fatalf("unexpected text content: %q", tc.Text)
	}
}
