package namecheap_provider

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-log/tflogtest"
)

// decodeOne runs fn against a bridge-backed slog.Logger whose tflog output is
// captured, then decodes and returns the single log entry it produced.
func decodeOne(t *testing.T, fn func(ctx context.Context, logger *slog.Logger)) map[string]interface{} {
	t.Helper()

	var buf bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &buf)
	logger := slog.New(newBridgeHandler())

	fn(ctx, logger)

	entries, err := tflogtest.MultilineJSONDecode(&buf)
	if err != nil {
		t.Fatalf("failed to decode tflog output: %s\nraw: %q", err, buf.String())
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 log entry, got %d: %v", len(entries), entries)
	}
	return entries[0]
}

func TestBridgeHandler_Handle_ForwardsMessageAndFields(t *testing.T) {
	var buf bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &buf)
	logger := slog.New(newBridgeHandler())

	logger.LogAttrs(ctx, slog.LevelInfo, "namecheap request completed",
		slog.String("command", "namecheap.domains.getInfo"),
		slog.Int("status", 200),
	)

	entries, err := tflogtest.MultilineJSONDecode(&buf)
	if err != nil {
		t.Fatalf("failed to decode tflog output: %s\nraw: %q", err, buf.String())
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d: %v", len(entries), entries)
	}
	entry := entries[0]

	if got := entry["@message"]; got != "namecheap request completed" {
		t.Errorf("@message = %v, want %q", got, "namecheap request completed")
	}
	if got := entry["@level"]; got != "info" {
		t.Errorf("@level = %v, want %q", got, "info")
	}
	if got := entry["command"]; got != "namecheap.domains.getInfo" {
		t.Errorf("command = %v, want %q", got, "namecheap.domains.getInfo")
	}
	// JSON numbers decode as float64.
	if got := entry["status"]; got != float64(200) {
		t.Errorf("status = %v (%T), want 200", got, got)
	}
}

// TestBridgeHandler_Handle_RedactsSensitiveParams feeds a real credential value
// through the exact shape the SDK forwards (a "params" map that carries
// cleartext account identifiers) and asserts the bridge redacts the
// provider-Sensitive keys (Username, ApiUser, ApiKey) before anything reaches
// the log, while leaving non-sensitive params intact. This guards the
// security-sensitive path: the SDK does NOT redact Username/ApiUser, so without
// the bridge's backstop these Sensitive identifiers would leak into
// TF_LOG_PROVIDER_NAMECHEAP=DEBUG output.
func TestBridgeHandler_Handle_RedactsSensitiveParams(t *testing.T) {
	const (
		secretUser   = "super-secret-account-user"
		secretApiKey = "super-secret-api-key-value"
	)

	var buf bytes.Buffer
	ctx := tflogtest.RootLogger(context.Background(), &buf)
	logger := slog.New(newBridgeHandler())

	logger.LogAttrs(ctx, slog.LevelDebug, "namecheap request start",
		slog.String("command", "namecheap.domains.getInfo"),
		slog.Any("params", map[string]string{
			"Command":    "namecheap.domains.getInfo",
			"DomainName": "example.com",
			"Username":   secretUser,
			"ApiUser":    secretUser,
			"ApiKey":     secretApiKey,
		}),
	)

	raw := buf.String()
	if strings.Contains(raw, secretUser) {
		t.Fatalf("Sensitive account identifier leaked into log output: %q", raw)
	}
	if strings.Contains(raw, secretApiKey) {
		t.Fatalf("Sensitive api key leaked into log output: %q", raw)
	}

	entries, err := tflogtest.MultilineJSONDecode(&buf)
	if err != nil {
		t.Fatalf("failed to decode tflog output: %s\nraw: %q", err, raw)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d: %v", len(entries), entries)
	}
	params, ok := entries[0]["params"].(map[string]interface{})
	if !ok {
		t.Fatalf("params = %v (%T), want a map", entries[0]["params"], entries[0]["params"])
	}

	for _, k := range []string{"Username", "ApiUser", "ApiKey"} {
		if got := params[k]; got != "***" {
			t.Errorf("params[%q] = %v, want redacted %q", k, got, "***")
		}
	}
	// Non-sensitive params must survive untouched so the log stays useful.
	if got := params["DomainName"]; got != "example.com" {
		t.Errorf("params[DomainName] = %v, want %q (non-sensitive, must not be redacted)", got, "example.com")
	}
	if got := params["Command"]; got != "namecheap.domains.getInfo" {
		t.Errorf("params[Command] = %v, want %q", got, "namecheap.domains.getInfo")
	}
}

func TestBridgeHandler_Handle_LevelMapping(t *testing.T) {
	cases := []struct {
		name  string
		level slog.Level
		want  string
	}{
		{"debug", slog.LevelDebug, "debug"},
		{"info", slog.LevelInfo, "info"},
		{"warn", slog.LevelWarn, "warn"},
		{"error", slog.LevelError, "error"},
		// Levels between the named ones round down to the lower tflog level.
		{"below-warn", slog.LevelInfo + 1, "info"},
		{"above-error", slog.LevelError + 4, "error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := decodeOne(t, func(ctx context.Context, logger *slog.Logger) {
				logger.LogAttrs(ctx, tc.level, "msg")
			})
			if got := entry["@level"]; got != tc.want {
				t.Errorf("@level = %v, want %q", got, tc.want)
			}
		})
	}
}

func TestBridgeHandler_Enabled(t *testing.T) {
	h := newBridgeHandler()
	for _, lvl := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		if !h.Enabled(context.Background(), lvl) {
			t.Errorf("Enabled(%v) = false, want true (tflog should do the filtering)", lvl)
		}
	}
}

func TestBridgeHandler_WithAttrs(t *testing.T) {
	entry := decodeOne(t, func(ctx context.Context, logger *slog.Logger) {
		logger.With(slog.String("command", "namecheap.domains.getList")).
			LogAttrs(ctx, slog.LevelWarn, "namecheap request failed",
				slog.Int("status", 420),
			)
	})

	if got := entry["command"]; got != "namecheap.domains.getList" {
		t.Errorf("command = %v, want preset attr to be forwarded", got)
	}
	if got := entry["status"]; got != float64(420) {
		t.Errorf("status = %v, want 420 (record attr alongside preset)", got)
	}
	if got := entry["@level"]; got != "warn" {
		t.Errorf("@level = %v, want warn", got)
	}
}

func TestBridgeHandler_WithGroup(t *testing.T) {
	entry := decodeOne(t, func(ctx context.Context, logger *slog.Logger) {
		logger.WithGroup("req").
			LogAttrs(ctx, slog.LevelInfo, "msg",
				slog.String("command", "namecheap.domains.getInfo"),
			)
	})

	if got := entry["req.command"]; got != "namecheap.domains.getInfo" {
		t.Errorf("req.command = %v, want group-prefixed key", got)
	}
	if _, ok := entry["command"]; ok {
		t.Errorf("unexpected unprefixed command key in %v", entry)
	}
}

func TestBridgeHandler_WithGroupThenAttrs(t *testing.T) {
	// Attrs set after a group opens are prefixed; attrs set before it are not.
	entry := decodeOne(t, func(ctx context.Context, logger *slog.Logger) {
		logger.With(slog.String("top", "a")).
			WithGroup("g").
			With(slog.String("inner", "b")).
			LogAttrs(ctx, slog.LevelInfo, "msg",
				slog.Int("status", 200),
			)
	})

	if got := entry["top"]; got != "a" {
		t.Errorf("top = %v, want %q (pre-group attr keeps bare key)", got, "a")
	}
	if got := entry["g.inner"]; got != "b" {
		t.Errorf("g.inner = %v, want %q (post-group attr is prefixed)", got, "b")
	}
	if got := entry["g.status"]; got != float64(200) {
		t.Errorf("g.status = %v, want 200 (record attr prefixed by open group)", got)
	}
}

// TestBridgeHandler_NoOpEdges covers the shortcut branches: WithAttrs with no
// attrs and WithGroup with an empty name both return the same handler.
func TestBridgeHandler_NoOpEdges(t *testing.T) {
	h := newBridgeHandler()
	if h.WithAttrs(nil) != h {
		t.Error("WithAttrs(nil) should return the receiver unchanged")
	}
	if h.WithGroup("") != h {
		t.Error("WithGroup(\"\") should return the receiver unchanged")
	}
}

// TestBridgeHandler_Handle_GroupValuedAttrs exercises addAttr's group handling:
// a named group is flattened under a dotted prefix, an empty-key group inlines
// its children without a prefix, an empty group is dropped, and an empty attr
// is dropped. These are defensive branches (the SDK emits only flat attrs), so
// without this test a regression in the recursive flattening would go
// unnoticed.
func TestBridgeHandler_Handle_GroupValuedAttrs(t *testing.T) {
	entry := decodeOne(t, func(ctx context.Context, logger *slog.Logger) {
		logger.LogAttrs(ctx, slog.LevelInfo, "msg",
			// Named group: children flattened under "req.<key>".
			slog.Group("req",
				slog.String("command", "namecheap.domains.getInfo"),
				slog.Int("attempt", 1),
			),
			// Empty-key group: children inlined without a prefix.
			slog.Group("", slog.String("inline", "v")),
			// Empty group: dropped entirely, no key emitted.
			slog.Group("dropme"),
			// Empty attr: dropped entirely.
			slog.Attr{},
		)
	})

	if got := entry["req.command"]; got != "namecheap.domains.getInfo" {
		t.Errorf("req.command = %v, want group child flattened under group prefix", got)
	}
	if got := entry["req.attempt"]; got != float64(1) {
		t.Errorf("req.attempt = %v, want 1", got)
	}
	if got := entry["inline"]; got != "v" {
		t.Errorf("inline = %v, want %q (empty-key group inlines children)", got, "v")
	}
	if _, ok := entry["dropme"]; ok {
		t.Errorf("empty group should be dropped, but key present in %v", entry)
	}
}

// TestRedactSensitive covers the bridge's credential backstop directly: it
// redacts the provider-Sensitive keys (Username/ApiUser/ApiKey) in both the
// map[string]string shape the SDK forwards and the defensive map[string]any
// shape, preserves non-sensitive entries, and passes non-map values through.
func TestRedactSensitive(t *testing.T) {
	gotStr, ok := redactSensitive(map[string]string{
		"Username":   "u",
		"ApiUser":    "au",
		"ApiKey":     "k",
		"DomainName": "example.com",
	}).(map[string]string)
	if !ok {
		t.Fatal("redactSensitive(map[string]string) did not return map[string]string")
	}
	for _, k := range []string{"Username", "ApiUser", "ApiKey"} {
		if gotStr[k] != "***" {
			t.Errorf("map[string]string[%q] = %q, want redacted %q", k, gotStr[k], "***")
		}
	}
	if gotStr["DomainName"] != "example.com" {
		t.Errorf("DomainName = %q, want preserved", gotStr["DomainName"])
	}

	gotAny, ok := redactSensitive(map[string]any{
		"ApiUser": "au",
		"count":   3,
	}).(map[string]any)
	if !ok {
		t.Fatal("redactSensitive(map[string]any) did not return map[string]any")
	}
	if gotAny["ApiUser"] != "***" {
		t.Errorf("map[string]any[ApiUser] = %v, want redacted %q", gotAny["ApiUser"], "***")
	}
	if gotAny["count"] != 3 {
		t.Errorf("count = %v, want 3 (non-sensitive preserved)", gotAny["count"])
	}

	if got := redactSensitive("plain"); got != "plain" {
		t.Errorf("redactSensitive(string) = %v, want passthrough", got)
	}
}
