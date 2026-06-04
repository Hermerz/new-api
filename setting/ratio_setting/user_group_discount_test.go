package ratio_setting

import (
	"testing"
)

// Tests for GetUserGroupModelDiscount.
//
// The package-level userGroupModelDiscount store is a singleton. Each test loads
// its own fixture via UpdateUserGroupModelDiscountByJSONString (which accepts the
// canonical array shape and the legacy map shape) and Clears on cleanup. Tests
// are NOT t.Parallel() — they share the singleton.

func loadDiscount(t *testing.T, jsonStr string) {
	t.Helper()
	if err := UpdateUserGroupModelDiscountByJSONString(jsonStr); err != nil {
		t.Fatalf("load fixture failed: %v", err)
	}
	t.Cleanup(func() { userGroupModelDiscount.Clear() })
}

func TestGetUserGroupModelDiscount_EmptyStore_ReturnsNoOp(t *testing.T) {
	userGroupModelDiscount.Clear()
	t.Cleanup(func() { userGroupModelDiscount.Clear() })

	got, ok := GetUserGroupModelDiscount("default", "gpt-5")

	if ok || got != 1.0 {
		t.Errorf("empty store: got (%v, %v), want (1.0, false)", got, ok)
	}
}

func TestGetUserGroupModelDiscount_UnknownGroup_ReturnsNoOp(t *testing.T) {
	loadDiscount(t, `{"default": [{"pattern":"gpt*","discount":0.20}]}`)

	got, ok := GetUserGroupModelDiscount("nonexistent-group", "gpt-5")

	if ok || got != 1.0 {
		t.Errorf("unknown group: got (%v, %v), want (1.0, false)", got, ok)
	}
}

func TestGetUserGroupModelDiscount_NoMatch_ReturnsNoOp(t *testing.T) {
	loadDiscount(t, `{"default": [{"pattern":"gpt*","discount":0.20}]}`)

	got, ok := GetUserGroupModelDiscount("default", "claude-opus-4")

	if ok || got != 1.0 {
		t.Errorf("no matching rule: got (%v, %v), want (1.0, false)", got, ok)
	}
}

func TestGetUserGroupModelDiscount_PrefixGlob(t *testing.T) {
	loadDiscount(t, `{"personal-standard": [
		{"pattern":"gpt*","discount":0.30},
		{"pattern":"claude*","discount":0.35},
		{"pattern":"deepseek*","discount":0.50}
	]}`)

	cases := []struct {
		model string
		want  float64
	}{
		{"gpt-4o", 0.30},
		{"gpt-5.4-pro", 0.30},
		{"claude-opus-4-6", 0.35},
		{"deepseek-chat", 0.50},
		{"deepseek-reasoner", 0.50},
	}
	for _, c := range cases {
		got, ok := GetUserGroupModelDiscount("personal-standard", c.model)
		if !ok || got != c.want {
			t.Errorf("model=%s: got (%v, %v), want (%v, true)", c.model, got, ok, c.want)
		}
	}
}

func TestGetUserGroupModelDiscount_SuffixAndContainsGlob(t *testing.T) {
	loadDiscount(t, `{"default": [
		{"pattern":"*-openai-compact","discount":0.30},
		{"pattern":"*embedding*","discount":0.80}
	]}`)

	if got, ok := GetUserGroupModelDiscount("default", "some-model-openai-compact"); !ok || got != 0.30 {
		t.Errorf("suffix glob: got (%v, %v), want (0.30, true)", got, ok)
	}
	if got, ok := GetUserGroupModelDiscount("default", "gemini-embedding-2-preview"); !ok || got != 0.80 {
		t.Errorf("contains glob: got (%v, %v), want (0.80, true)", got, ok)
	}
}

func TestGetUserGroupModelDiscount_OrderedFirstMatchWins(t *testing.T) {
	// gpt-5-pro matches both *pro* and gpt*. The rule listed first wins.
	loadDiscount(t, `{"default": [
		{"pattern":"*pro*","discount":0.45},
		{"pattern":"gpt*","discount":0.30}
	]}`)
	if got, ok := GetUserGroupModelDiscount("default", "gpt-5-pro"); !ok || got != 0.45 {
		t.Errorf("first-match (*pro* first): got (%v, %v), want (0.45, true)", got, ok)
	}

	// Reversed order → gpt* now wins for the same model.
	loadDiscount(t, `{"default": [
		{"pattern":"gpt*","discount":0.30},
		{"pattern":"*pro*","discount":0.45}
	]}`)
	if got, ok := GetUserGroupModelDiscount("default", "gpt-5-pro"); !ok || got != 0.30 {
		t.Errorf("first-match (gpt* first): got (%v, %v), want (0.30, true)", got, ok)
	}
}

func TestGetUserGroupModelDiscount_CaseInsensitive(t *testing.T) {
	// Operator wrote capitalized Qwen*; real model names are lower-case.
	loadDiscount(t, `{"default": [{"pattern":"Qwen*","discount":0.45}]}`)

	if got, ok := GetUserGroupModelDiscount("default", "qwen3-max"); !ok || got != 0.45 {
		t.Errorf("case-insensitive Qwen*: got (%v, %v), want (0.45, true)", got, ok)
	}
}

func TestGetUserGroupModelDiscount_ExactPattern(t *testing.T) {
	loadDiscount(t, `{"default": [
		{"pattern":"gpt-5.4","discount":0.10},
		{"pattern":"gpt*","discount":0.30}
	]}`)

	if got, ok := GetUserGroupModelDiscount("default", "gpt-5.4"); !ok || got != 0.10 {
		t.Errorf("exact rule first: got (%v, %v), want (0.10, true)", got, ok)
	}
	if got, ok := GetUserGroupModelDiscount("default", "gpt-4o"); !ok || got != 0.30 {
		t.Errorf("falls to glob: got (%v, %v), want (0.30, true)", got, ok)
	}
}

func TestGetUserGroupModelDiscount_NonPositiveRuleSkipped(t *testing.T) {
	// A discount<=0 rule must not match/shadow a later valid rule (#71).
	loadDiscount(t, `{"default": [
		{"pattern":"gpt*","discount":0},
		{"pattern":"gpt-4o","discount":0.30}
	]}`)

	if got, ok := GetUserGroupModelDiscount("default", "gpt-4o"); !ok || got != 0.30 {
		t.Errorf("skip non-positive rule: got (%v, %v), want (0.30, true)", got, ok)
	}
	// A model only covered by the 0-discount rule falls through to no-op.
	if got, ok := GetUserGroupModelDiscount("default", "gpt-5"); ok || got != 1.0 {
		t.Errorf("only non-positive matches: got (%v, %v), want (1.0, false)", got, ok)
	}
}

// --- backward-compat: legacy map shape ------------------------------------

func TestGetUserGroupModelDiscount_LegacyMapShape(t *testing.T) {
	loadDiscount(t, `{"default": {"gpt-5": 0.20, "claude-opus": 0.40}}`)

	if got, ok := GetUserGroupModelDiscount("default", "gpt-5"); !ok || got != 0.20 {
		t.Errorf("legacy gpt-5: got (%v, %v), want (0.20, true)", got, ok)
	}
	if got, ok := GetUserGroupModelDiscount("default", "claude-opus"); !ok || got != 0.40 {
		t.Errorf("legacy claude-opus: got (%v, %v), want (0.40, true)", got, ok)
	}
}

func TestGetUserGroupModelDiscount_LegacyMap_ExactBeatsGlob(t *testing.T) {
	// Legacy maps are unordered; specificity sort must make the exact key win.
	loadDiscount(t, `{"default": {"gpt-4o": 0.10, "gpt*": 0.30}}`)

	if got, ok := GetUserGroupModelDiscount("default", "gpt-4o"); !ok || got != 0.10 {
		t.Errorf("legacy exact beats glob: got (%v, %v), want (0.10, true)", got, ok)
	}
	if got, ok := GetUserGroupModelDiscount("default", "gpt-5"); !ok || got != 0.30 {
		t.Errorf("legacy glob fallback: got (%v, %v), want (0.30, true)", got, ok)
	}
}

func TestGetUserGroupModelDiscount_LegacyCompactWildcard(t *testing.T) {
	// The old *-openai-compact special case is now just a normal suffix glob.
	loadDiscount(t, `{"default": {"*-openai-compact": 0.30}}`)

	model := "some-custom-model-openai-compact"
	if got, ok := GetUserGroupModelDiscount("default", model); !ok || got != 0.30 {
		t.Errorf("legacy compact wildcard: got (%v, %v), want (0.30, true)", got, ok)
	}
}

// --- serialization round-trip + atomicity ---------------------------------

func TestUpdateUserGroupModelDiscountByJSONString_RoundTrip(t *testing.T) {
	loadDiscount(t, `{"default":[{"pattern":"gpt*","discount":0.3}]}`)

	if got, ok := GetUserGroupModelDiscount("default", "gpt-4o"); !ok || got != 0.3 {
		t.Errorf("round-trip: got (%v, %v), want (0.3, true)", got, ok)
	}
}

func TestUpdateUserGroupModelDiscountByJSONString_MalformedJSON_IsAtomic(t *testing.T) {
	loadDiscount(t, `{"default":[{"pattern":"gpt*","discount":0.3}]}`)

	if err := UpdateUserGroupModelDiscountByJSONString("not-json"); err == nil {
		t.Fatal("expected error on malformed JSON, got nil")
	}

	// Atomic: the prior valid config must survive a failed update (improves on
	// the old non-atomic LoadFromJsonString footgun).
	if got, ok := GetUserGroupModelDiscount("default", "gpt-4o"); !ok || got != 0.3 {
		t.Errorf("config must survive failed update: got (%v, %v), want (0.3, true)", got, ok)
	}
}

func TestParseGroupRules_BadShape_ReturnsError(t *testing.T) {
	if err := UpdateUserGroupModelDiscountByJSONString(`{"default": 0.5}`); err == nil {
		t.Fatal("expected error for scalar group value, got nil")
	}
}

func TestMarshalReparse_CanonicalShapeStable(t *testing.T) {
	// MarshalJSON must emit the canonical array shape that re-parses to identical
	// lookups — this is the persistence round-trip the config system relies on
	// (SaveToDB marshals; boot/admin-save unmarshals).
	loadDiscount(t, `{"personal-standard": [
		{"pattern":"gpt-5.4","discount":0.10},
		{"pattern":"gpt*","discount":0.30},
		{"pattern":"deepseek*","discount":0.50}
	]}`)

	serialized := UserGroupModelDiscount2JSONString()

	userGroupModelDiscount.Clear()
	if err := UpdateUserGroupModelDiscountByJSONString(serialized); err != nil {
		t.Fatalf("reparse of marshaled output failed: %v", err)
	}

	cases := []struct {
		model string
		want  float64
	}{
		{"gpt-5.4", 0.10}, // ordering (exact before glob) survives round-trip
		{"gpt-4o", 0.30},
		{"deepseek-chat", 0.50},
	}
	for _, c := range cases {
		if got, ok := GetUserGroupModelDiscount("personal-standard", c.model); !ok || got != c.want {
			t.Errorf("after reparse model=%s: got (%v, %v), want (%v, true)", c.model, got, ok, c.want)
		}
	}
}

// --- globMatch unit coverage ----------------------------------------------

func TestIsModelRatioConfigured(t *testing.T) {
	InitRatioSettings() // seed modelRatioMap from defaults (not auto-run in tests)
	// A model present in the default ModelRatio map (gpt-4.5-preview, legitimately
	// 37.5) must read as configured; an unknown model must not — independent of
	// the 37.5 fallback value (Hermerz/Hermes#127 critical-fix).
	if !IsModelRatioConfigured("gpt-4.5-preview") {
		t.Errorf("gpt-4.5-preview is in the ratio map; want configured=true")
	}
	if IsModelRatioConfigured("totally-unknown-model-xyz-123") {
		t.Errorf("unknown model must read configured=false")
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"gpt*", "gpt-4o", true},
		{"gpt*", "claude", false},
		{"*pro", "gpt-5-pro", true},
		{"*pro", "gpt-5-pro-max", false},
		{"*pro*", "gpt-5-pro-max", true},
		{"*pro*", "gpt-4o", false},
		{"a*c", "abc", true},
		{"a*c", "abdc", true},
		{"a*c", "ab", false},
		{"*", "anything/with-slash", true},
		{"deep*chat", "deepseek-chat", true},
		{"exact", "exact", true},
		{"exact", "exactly", false},
		// '*' must span '/' (unlike path.Match)
		{"*chat*", "provider/deepseek-chat", true},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.name); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}
