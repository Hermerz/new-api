package types

import "testing"

// TestRWMap_AtomicLoadOnBadJSON is the regression test for #134: a malformed
// payload must NOT clear the live map — the previous clears-before-parse made a
// bad group_ratio PUT transiently empty the live billing tables.
func TestRWMap_AtomicLoadOnBadJSON(t *testing.T) {
	cases := []struct {
		name string
		load func(m *RWMap[string, float64]) error
	}{
		{"UnmarshalJSON", func(m *RWMap[string, float64]) error { return m.UnmarshalJSON([]byte("{not json")) }},
		{"LoadFromJsonString", func(m *RWMap[string, float64]) error { return LoadFromJsonString(m, "{not json") }},
		{"LoadFromJsonStringWithCallback", func(m *RWMap[string, float64]) error {
			return LoadFromJsonStringWithCallback(m, "{not json", func() { t.Fatal("onSuccess must not fire on bad JSON") })
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := NewRWMap[string, float64]()
			m.AddAll(map[string]float64{"personal-standard": 0.5})

			if err := c.load(m); err == nil {
				t.Fatal("expected error for malformed JSON, got nil")
			}
			// Live map must be untouched (NOT cleared).
			if v, ok := m.Get("personal-standard"); !ok || v != 0.5 {
				t.Fatalf("live map cleared on bad JSON: get=%v ok=%v len=%d", v, ok, m.Len())
			}
		})
	}
}

// TestRWMap_GoodJSONSwapsIn confirms a valid payload still replaces contents
// (old keys dropped) for all three loaders.
func TestRWMap_GoodJSONSwapsIn(t *testing.T) {
	loaders := []struct {
		name string
		load func(m *RWMap[string, float64]) error
	}{
		{"UnmarshalJSON", func(m *RWMap[string, float64]) error { return m.UnmarshalJSON([]byte(`{"vip":0.3}`)) }},
		{"LoadFromJsonString", func(m *RWMap[string, float64]) error { return LoadFromJsonString(m, `{"vip":0.3}`) }},
		{"LoadFromJsonStringWithCallback", func(m *RWMap[string, float64]) error {
			fired := false
			err := LoadFromJsonStringWithCallback(m, `{"vip":0.3}`, func() { fired = true })
			if err == nil && !fired {
				t.Fatal("onSuccess must fire on valid JSON")
			}
			return err
		}},
	}
	for _, l := range loaders {
		t.Run(l.name, func(t *testing.T) {
			m := NewRWMap[string, float64]()
			m.AddAll(map[string]float64{"old": 1})
			if err := l.load(m); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v, ok := m.Get("vip"); !ok || v != 0.3 {
				t.Fatalf("valid JSON not applied: %v %v", v, ok)
			}
			if _, ok := m.Get("old"); ok {
				t.Fatal("replace should drop old keys")
			}
		})
	}
}
