package namecheap_provider

import (
	"context"
	"log/slog"
	"strings"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// bridgeHandler is a slog.Handler that forwards records emitted by the
// go-namecheap-sdk (via ClientOptions.Logger) into Terraform's tflog. The SDK
// invokes the logger as logger.LogAttrs(ctx, level, msg, attrs...), threading
// the per-operation context that tflog needs to route output to the right
// provider log stream. Enabling it is what surfaces per-API-call entries
// (command, attempt, duration, status, error_code) under
// TF_LOG_PROVIDER_NAMECHEAP=DEBUG.
//
// The SDK already redacts secret parameters (ApiKey, passwords, EPPCode, ...)
// to "***" before logging, and this bridge introduces no credentials of its
// own. As a backstop it additionally redacts the account identifiers the
// provider marks Sensitive (Username/ApiUser) inside any forwarded parameter
// map, because the SDK forwards those in cleartext (see sensitiveParamKeys).
type bridgeHandler struct {
	// groups is the open group path; it prefixes the keys of record attrs and
	// of any attrs added after the group was opened.
	groups []string
	// fields holds attrs accumulated via WithAttrs, already resolved and
	// qualified with the group path that was active when they were added.
	fields map[string]any
}

// newBridgeHandler returns a slog.Handler that bridges slog records to tflog.
func newBridgeHandler() *bridgeHandler {
	return &bridgeHandler{}
}

// Enabled always returns true so that tflog (driven by TF_LOG / the
// TF_LOG_PROVIDER_NAMECHEAP level) does the level filtering rather than the
// bridge dropping records early.
func (h *bridgeHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

// Handle maps the slog record's level to the matching tflog function and
// forwards the message plus a flattened field map. The context carries the
// tflog logger, so it must be the SDK's per-operation context.
func (h *bridgeHandler) Handle(ctx context.Context, record slog.Record) error {
	fields := make(map[string]any, len(h.fields)+record.NumAttrs())
	for k, v := range h.fields {
		fields[k] = v
	}
	record.Attrs(func(a slog.Attr) bool {
		addAttr(fields, h.groups, a)
		return true
	})

	msg := record.Message
	switch {
	case record.Level >= slog.LevelError:
		tflog.Error(ctx, msg, fields)
	case record.Level >= slog.LevelWarn:
		tflog.Warn(ctx, msg, fields)
	case record.Level >= slog.LevelInfo:
		tflog.Info(ctx, msg, fields)
	default:
		tflog.Debug(ctx, msg, fields)
	}
	return nil
}

// WithAttrs returns a handler that also emits the given attrs on every record.
// The attrs are resolved and qualified with the current group path up front,
// so later WithGroup calls do not retroactively re-prefix them.
func (h *bridgeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	fields := make(map[string]any, len(h.fields)+len(attrs))
	for k, v := range h.fields {
		fields[k] = v
	}
	for _, a := range attrs {
		addAttr(fields, h.groups, a)
	}
	return &bridgeHandler{groups: h.groups, fields: fields}
}

// WithGroup returns a handler that prefixes the keys of subsequently added
// attrs (and record attrs) with name, dot-separated. Already-accumulated
// fields keep their existing keys.
func (h *bridgeHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	groups := make([]string, 0, len(h.groups)+1)
	groups = append(groups, h.groups...)
	groups = append(groups, name)
	return &bridgeHandler{groups: groups, fields: h.fields}
}

// addAttr resolves a single attr and writes it into fields under a key that is
// prefixed by the group path. Group-valued attrs are flattened recursively; an
// empty group key inlines its children without adding a prefix, and empty
// attrs are dropped, matching slog.Handler semantics.
func addAttr(fields map[string]any, groups []string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}

	if a.Value.Kind() == slog.KindGroup {
		children := a.Value.Group()
		if len(children) == 0 {
			return
		}
		childGroups := groups
		if a.Key != "" {
			childGroups = make([]string, 0, len(groups)+1)
			childGroups = append(childGroups, groups...)
			childGroups = append(childGroups, a.Key)
		}
		for _, child := range children {
			addAttr(fields, childGroups, child)
		}
		return
	}

	key := a.Key
	if len(groups) > 0 {
		key = strings.Join(groups, ".") + "." + a.Key
	}
	fields[key] = redactSensitive(a.Value.Any())
}

// redactedValue is the placeholder written in place of a redacted value; it
// matches the placeholder the SDK uses for its own secret redaction.
const redactedValue = "***"

// sensitiveParamKeys are request-parameter keys whose values are account
// credentials or identifiers the provider marks Sensitive: user_name ->
// Username, api_user -> ApiUser, api_key -> ApiKey. The SDK already redacts
// ApiKey, but it forwards Username and ApiUser in cleartext inside the "params"
// attr; the bridge redacts them here so Sensitive account identifiers never
// reach the Terraform debug log. Re-redacting ApiKey is a harmless backstop.
var sensitiveParamKeys = map[string]struct{}{
	"Username": {},
	"ApiUser":  {},
	"ApiKey":   {},
}

// redactSensitive returns v with the values of any sensitiveParamKeys replaced
// by "***" when v is a string-keyed parameter map (the shape the SDK uses for
// the forwarded "params" attr). Non-map values, and maps carrying no sensitive
// keys, are returned unchanged. It never mutates the input.
func redactSensitive(v any) any {
	switch m := v.(type) {
	case map[string]string:
		out := make(map[string]string, len(m))
		for k, val := range m {
			if _, secret := sensitiveParamKeys[k]; secret {
				out[k] = redactedValue
			} else {
				out[k] = val
			}
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			if _, secret := sensitiveParamKeys[k]; secret {
				out[k] = redactedValue
			} else {
				out[k] = val
			}
		}
		return out
	default:
		return v
	}
}
