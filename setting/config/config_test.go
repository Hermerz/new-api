package config

import "testing"

type testLimits struct {
	Max int `json:"max"`
}

type testCfg struct {
	Discounts map[string]float64 `json:"discounts"`
	Limits    *testLimits        `json:"limits"`
	Name      string             `json:"name"`
}

// TestUpdateConfigFromMap_InvalidJSON verifies the #129 fix: an invalid JSON
// value for a layered setting must (1) return an error so the caller refuses to
// persist it, and (2) leave the live config untouched (no partial mutation),
// rather than silently swallowing the error and dropping the value.
func TestUpdateConfigFromMap_InvalidJSON(t *testing.T) {
	cfg := &testCfg{
		Discounts: map[string]float64{"vip": 0.8},
		Limits:    &testLimits{Max: 10},
		Name:      "orig",
	}

	// Map field: bad JSON must error and leave the existing value intact.
	if err := UpdateConfigFromMap(cfg, map[string]string{"discounts": "{not json"}); err == nil {
		t.Fatal("expected error for invalid map JSON, got nil")
	}
	if len(cfg.Discounts) != 1 || cfg.Discounts["vip"] != 0.8 {
		t.Fatalf("live map mutated on invalid JSON: %+v", cfg.Discounts)
	}

	// Ptr field: bad JSON must error and not half-initialize the pointer.
	if err := UpdateConfigFromMap(cfg, map[string]string{"limits": "{bad"}); err == nil {
		t.Fatal("expected error for invalid ptr JSON, got nil")
	}
	if cfg.Limits == nil || cfg.Limits.Max != 10 {
		t.Fatalf("live ptr mutated on invalid JSON: %+v", cfg.Limits)
	}

	// Valid JSON still applies.
	if err := UpdateConfigFromMap(cfg, map[string]string{"discounts": `{"vip":0.5,"pro":0.7}`}); err != nil {
		t.Fatalf("valid JSON should not error: %v", err)
	}
	if cfg.Discounts["pro"] != 0.7 {
		t.Fatalf("valid JSON did not apply: %+v", cfg.Discounts)
	}

	// Scalar string still applies (unchanged lenient behavior).
	if err := UpdateConfigFromMap(cfg, map[string]string{"name": "new"}); err != nil {
		t.Fatalf("string set should not error: %v", err)
	}
	if cfg.Name != "new" {
		t.Fatalf("string not set: %q", cfg.Name)
	}
}
