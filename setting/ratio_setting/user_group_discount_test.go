package ratio_setting

import (
	"testing"
)

// Tests for GetUserGroupModelDiscount.
//
// Because the package-level userGroupModelDiscountMap is a singleton initialized
// in init() with an empty default, each test starts from a known empty state via
// userGroupModelDiscountMap.Clear() and then loads its own fixture data. Tests
// are NOT t.Parallel() — they share the singleton.

func resetUserGroupModelDiscount(t *testing.T, fixture map[string]map[string]float64) {
	t.Helper()
	userGroupModelDiscountMap.Clear()
	if fixture != nil {
		userGroupModelDiscountMap.AddAll(fixture)
	}
	t.Cleanup(func() { userGroupModelDiscountMap.Clear() })
}

func TestGetUserGroupModelDiscount_EmptyMap_ReturnsNoOp(t *testing.T) {
	resetUserGroupModelDiscount(t, nil)

	got, ok := GetUserGroupModelDiscount("default", "gpt-5")

	if ok {
		t.Errorf("expected ok=false on empty map, got ok=true (discount=%v)", got)
	}
	if got != 1.0 {
		t.Errorf("expected discount=1.0 (no-op) on empty map, got %v", got)
	}
}

func TestGetUserGroupModelDiscount_GroupExistsModelMissing_ReturnsNoOp(t *testing.T) {
	resetUserGroupModelDiscount(t, map[string]map[string]float64{
		"default": {"gpt-5": 0.20},
	})

	got, ok := GetUserGroupModelDiscount("default", "claude-opus")

	if ok {
		t.Errorf("expected ok=false when model not in map, got ok=true (discount=%v)", got)
	}
	if got != 1.0 {
		t.Errorf("expected discount=1.0 when model not in map, got %v", got)
	}
}

func TestGetUserGroupModelDiscount_ExactMatch_ReturnsDiscount(t *testing.T) {
	resetUserGroupModelDiscount(t, map[string]map[string]float64{
		"default":    {"gpt-5": 0.20, "claude-opus": 0.40},
		"enterprise": {"gpt-5": 0.40, "claude-opus": 0.60},
	})

	cases := []struct {
		group, model string
		want         float64
	}{
		{"default", "gpt-5", 0.20},
		{"default", "claude-opus", 0.40},
		{"enterprise", "gpt-5", 0.40},
		{"enterprise", "claude-opus", 0.60},
	}

	for _, c := range cases {
		got, ok := GetUserGroupModelDiscount(c.group, c.model)
		if !ok || got != c.want {
			t.Errorf("group=%s model=%s: got (%v, %v), want (%v, true)", c.group, c.model, got, ok, c.want)
		}
	}
}

func TestGetUserGroupModelDiscount_UnknownGroup_ReturnsNoOp(t *testing.T) {
	resetUserGroupModelDiscount(t, map[string]map[string]float64{
		"default": {"gpt-5": 0.20},
	})

	got, ok := GetUserGroupModelDiscount("nonexistent-group", "gpt-5")

	if ok {
		t.Errorf("expected ok=false for unknown group, got ok=true (discount=%v)", got)
	}
	if got != 1.0 {
		t.Errorf("expected discount=1.0 for unknown group, got %v", got)
	}
}

func TestGetUserGroupModelDiscount_WildcardCompactSuffix_Matches(t *testing.T) {
	// Wildcard pattern '*-openai-compact' applies when the requested model
	// ends in the compact suffix and no exact match exists. Mirrors
	// GetModelRatio's wildcard behavior.
	resetUserGroupModelDiscount(t, map[string]map[string]float64{
		"default": {CompactWildcardModelKey: 0.30},
	})

	modelName := "some-custom-model" + CompactModelSuffix
	got, ok := GetUserGroupModelDiscount("default", modelName)

	if !ok {
		t.Fatalf("expected wildcard match, got ok=false")
	}
	if got != 0.30 {
		t.Errorf("expected discount=0.30 via wildcard, got %v", got)
	}
}

func TestGetUserGroupModelDiscount_ExactBeatsWildcard(t *testing.T) {
	// If both an exact match and a wildcard match are possible, exact wins.
	exactModel := "gpt-5" + CompactModelSuffix
	resetUserGroupModelDiscount(t, map[string]map[string]float64{
		"default": {
			exactModel:              0.10, // exact match
			CompactWildcardModelKey: 0.30, // wildcard fallback
		},
	})

	got, ok := GetUserGroupModelDiscount("default", exactModel)

	if !ok || got != 0.10 {
		t.Errorf("expected exact match to win (0.10, true), got (%v, %v)", got, ok)
	}
}

func TestGetUserGroupModelDiscount_NilModelMap_HandledSafely(t *testing.T) {
	// Defensive: if a group key ever maps to nil (corrupt JSON / partial load),
	// we should fall back to no-op rather than panic.
	userGroupModelDiscountMap.Clear()
	userGroupModelDiscountMap.Set("broken-group", nil)
	t.Cleanup(func() { userGroupModelDiscountMap.Clear() })

	got, ok := GetUserGroupModelDiscount("broken-group", "gpt-5")

	if ok {
		t.Errorf("expected ok=false for nil model map, got ok=true (discount=%v)", got)
	}
	if got != 1.0 {
		t.Errorf("expected discount=1.0 for nil model map, got %v", got)
	}
}

func TestUpdateUserGroupModelDiscountByJSONString_RoundTrip(t *testing.T) {
	resetUserGroupModelDiscount(t, nil)

	jsonStr := `{"default": {"gpt-5": 0.2}, "enterprise": {"gpt-5": 0.4}}`
	if err := UpdateUserGroupModelDiscountByJSONString(jsonStr); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	if got, ok := GetUserGroupModelDiscount("default", "gpt-5"); !ok || got != 0.2 {
		t.Errorf("default/gpt-5: got (%v, %v), want (0.2, true)", got, ok)
	}
	if got, ok := GetUserGroupModelDiscount("enterprise", "gpt-5"); !ok || got != 0.4 {
		t.Errorf("enterprise/gpt-5: got (%v, %v), want (0.4, true)", got, ok)
	}
}

func TestUpdateUserGroupModelDiscountByJSONString_MalformedJSON_ReturnsError(t *testing.T) {
	resetUserGroupModelDiscount(t, map[string]map[string]float64{
		"default": {"gpt-5": 0.20},
	})

	err := UpdateUserGroupModelDiscountByJSONString("not-json")
	if err == nil {
		t.Fatal("expected error on malformed JSON, got nil")
	}

	// Document existing (non-atomic) behavior of types.LoadFromJsonString:
	// the underlying RWMap is Cleared before Unmarshal, so a parse failure
	// leaves the map empty rather than untouched. All settings in new-api
	// share this sharp edge (group_ratio, model_ratio, etc.), so we assert
	// the current behavior rather than ideal atomicity.
	//
	// TODO(Hermerz/new-api): make types.LoadFromJsonString atomic — parse
	// to a temp map first, only swap on success. Would benefit every setting
	// caller and remove this footgun for admins pushing malformed JSON.
	if got, ok := GetUserGroupModelDiscount("default", "gpt-5"); ok || got != 1.0 {
		t.Errorf("expected map cleared after failed update (current non-atomic behavior): got (%v, %v), want (1.0, false)", got, ok)
	}
}
