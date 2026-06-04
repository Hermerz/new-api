package ratio_setting

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/setting/config"
)

// user_group_model_discount implements the per-user-group, per-model discount
// applied on top of the global ModelRatio (Hermerz/Hermes#51, #127).
//
// Storage shape (options table key "user_group_model_discount_setting"):
//
//	{
//	  "personal-standard": [
//	    { "pattern": "*pro-thinking*", "discount": 0.45 },
//	    { "pattern": "gpt*",           "discount": 0.30 },
//	    { "pattern": "deepseek*",      "discount": 0.50 }
//	  ]
//	}
//
// Each group maps to an ORDERED list of rules. A model name is matched against
// each rule's `pattern` using glob semantics where `*` matches any (possibly
// empty) run of characters — so prefix (`abc*`), suffix (`*abc`), contains
// (`*abc*`) and interior (`a*c`) patterns all work. The FIRST matching rule in
// config order wins, so operators put specific rules before broad ones.
// Matching is case-insensitive (fixes the `Qwen*` vs `qwen-…` footgun).
//
// Final billing ratio (relay/helper/price.go, service/quota.go):
//
//	ratio = ModelRatio[model] × discount      (discount OVERRIDES GroupRatio)
//	ratio = ModelRatio[model] × GroupRatio     (when no rule matches)
//
// Backward compatibility: the legacy shape
//
//	{ "default": { "gpt-5": 0.2, "claude-opus": 0.4 } }
//
// is still accepted on load. A JSON object has no order, so legacy entries are
// converted to rules sorted by specificity (exact names first, then more
// literal characters), preserving the pre-existing "exact beats wildcard"
// behaviour. The canonical array shape is what gets written back on save.
//
// Zero-config behaviour: with an empty store, GetUserGroupModelDiscount returns
// (1.0, false) and billing matches the pre-feature path exactly.

// DiscountRule is one ordered pattern→discount mapping.
type DiscountRule struct {
	Pattern  string  `json:"pattern"`
	Discount float64 `json:"discount"`
}

// UserGroupModelDiscountMap is a thread-safe, ordered per-group rule store. It
// implements json.Marshaler/Unmarshaler with backward-compat for the legacy
// map[group]map[model]float64 shape; setting/config's reflection-based loader
// invokes these on both boot-load and admin-save.
type UserGroupModelDiscountMap struct {
	mutex sync.RWMutex
	data  map[string][]DiscountRule
}

func newUserGroupModelDiscountMap() *UserGroupModelDiscountMap {
	return &UserGroupModelDiscountMap{data: make(map[string][]DiscountRule)}
}

func (m *UserGroupModelDiscountMap) getRules(group string) ([]DiscountRule, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	r, ok := m.data[group]
	return r, ok
}

func (m *UserGroupModelDiscountMap) replace(data map[string][]DiscountRule) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.data = data
}

// Clear empties the store (used by tests).
func (m *UserGroupModelDiscountMap) Clear() {
	m.replace(make(map[string][]DiscountRule))
}

// UnmarshalJSON parses both the canonical array shape and the legacy map shape.
// Atomic: on any parse error the live store is left untouched.
func (m *UserGroupModelDiscountMap) UnmarshalJSON(b []byte) error {
	parsed, err := parseUserGroupModelDiscount(b)
	if err != nil {
		return err
	}
	m.replace(parsed)
	return nil
}

// MarshalJSON always emits the canonical array shape.
func (m *UserGroupModelDiscountMap) MarshalJSON() ([]byte, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if m.data == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(m.data)
}

// MarshalJSONString returns the JSON string representation, or "{}" on error.
func (m *UserGroupModelDiscountMap) MarshalJSONString() string {
	b, err := m.MarshalJSON()
	if err != nil {
		return "{}"
	}
	return string(b)
}

// parseUserGroupModelDiscount decodes the whole {group: rules} document,
// accepting either shape per group, without mutating any shared state.
func parseUserGroupModelDiscount(b []byte) (map[string][]DiscountRule, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make(map[string][]DiscountRule, len(raw))
	for group, rm := range raw {
		rules, err := parseGroupRules(rm)
		if err != nil {
			return nil, fmt.Errorf("user_group_model_discount: group %q: %w", group, err)
		}
		out[group] = rules
	}
	return out, nil
}

// parseGroupRules decodes one group's value: a JSON array of rules (canonical)
// or a JSON object of model→discount (legacy, sorted by specificity).
func parseGroupRules(rm json.RawMessage) ([]DiscountRule, error) {
	s := strings.TrimSpace(string(rm))
	if s == "" || s == "null" {
		return nil, nil
	}
	switch s[0] {
	case '[':
		var rules []DiscountRule
		if err := json.Unmarshal([]byte(s), &rules); err != nil {
			return nil, err
		}
		return rules, nil
	case '{':
		var legacy map[string]float64
		if err := json.Unmarshal([]byte(s), &legacy); err != nil {
			return nil, err
		}
		return legacyMapToRules(legacy), nil
	default:
		return nil, fmt.Errorf("rules must be a JSON array or object, got %q", s)
	}
}

// legacyMapToRules converts an unordered model→discount map into ordered rules,
// most-specific first, so legacy configs keep deterministic "exact beats glob".
func legacyMapToRules(legacy map[string]float64) []DiscountRule {
	rules := make([]DiscountRule, 0, len(legacy))
	for pattern, discount := range legacy {
		rules = append(rules, DiscountRule{Pattern: pattern, Discount: discount})
	}
	sort.SliceStable(rules, func(i, j int) bool {
		return moreSpecific(rules[i].Pattern, rules[j].Pattern)
	})
	return rules
}

// moreSpecific orders patterns when config order is undefined (legacy import):
// exact patterns first, then more literal characters, then longer pattern, then
// lexicographic — fully deterministic.
func moreSpecific(a, b string) bool {
	ea, eb := !isGlob(a), !isGlob(b)
	if ea != eb {
		return ea
	}
	if la, lb := literalLen(a), literalLen(b); la != lb {
		return la > lb
	}
	if len(a) != len(b) {
		return len(a) > len(b)
	}
	return a < b
}

func isGlob(p string) bool { return strings.Contains(p, "*") }

func literalLen(p string) int {
	return len(p) - strings.Count(p, "*")
}

// GetUserGroupModelDiscount returns the discount multiplier for (userGroup,
// modelName) using ordered first-match-wins glob matching.
//
// Returns (1.0, false) when no rule matches; the false lets callers distinguish
// "not configured" from a configured factor.
func GetUserGroupModelDiscount(userGroup, modelName string) (float64, bool) {
	rules, ok := userGroupModelDiscount.getRules(userGroup)
	if !ok || len(rules) == 0 {
		return 1.0, false
	}
	name := strings.ToLower(modelName)
	for _, r := range rules {
		// Skip non-positive discounts: settle-time billing treats discount<=0
		// as "not configured" and falls back to GroupRatio, so such a rule must
		// not shadow a later valid one (Hermerz/Hermes#71).
		if r.Discount <= 0 {
			continue
		}
		if globMatch(strings.ToLower(r.Pattern), name) {
			return r.Discount, true
		}
	}
	return 1.0, false
}

// globMatch reports whether name matches pattern, where '*' matches any
// (possibly empty) run of characters — including '/', unlike path.Match, so
// slash-containing model names match. Caller passes both inputs lower-cased.
func globMatch(pattern, name string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}
	parts := strings.Split(pattern, "*")
	// Leading segment (before first '*') must be a prefix.
	if head := parts[0]; head != "" {
		if !strings.HasPrefix(name, head) {
			return false
		}
		name = name[len(head):]
	}
	// Trailing segment (after last '*') must be a suffix.
	if tail := parts[len(parts)-1]; tail != "" {
		if !strings.HasSuffix(name, tail) {
			return false
		}
		name = name[:len(name)-len(tail)]
	}
	// Interior segments must appear in order.
	for _, seg := range parts[1 : len(parts)-1] {
		if seg == "" {
			continue
		}
		idx := strings.Index(name, seg)
		if idx < 0 {
			return false
		}
		name = name[idx+len(seg):]
	}
	return true
}

// UserGroupModelDiscountSetting is the persisted shape registered with config.
type UserGroupModelDiscountSetting struct {
	UserGroupModelDiscount *UserGroupModelDiscountMap `json:"user_group_model_discount"`
}

var userGroupModelDiscount = newUserGroupModelDiscountMap()

var userGroupModelDiscountSetting UserGroupModelDiscountSetting

func init() {
	userGroupModelDiscountSetting = UserGroupModelDiscountSetting{
		UserGroupModelDiscount: userGroupModelDiscount,
	}
	config.GlobalConfig.Register("user_group_model_discount_setting", &userGroupModelDiscountSetting)
}

// UserGroupModelDiscount2JSONString serializes the whole store for admin display.
func UserGroupModelDiscount2JSONString() string {
	return userGroupModelDiscount.MarshalJSONString()
}

// UpdateUserGroupModelDiscountByJSONString replaces the in-memory store from a
// JSON string (accepts both the canonical array shape and the legacy map shape).
// Atomic: malformed JSON returns an error and leaves the current store intact.
func UpdateUserGroupModelDiscountByJSONString(jsonStr string) error {
	return userGroupModelDiscount.UnmarshalJSON([]byte(jsonStr))
}

// LookupUserGroupDiscount returns the configured discount factor for
// (userGroup, modelName) or 0 if not configured (Hermerz/Hermes#67 helper
// extraction). Sentinel 0 lets callers detect "not configured" cleanly without
// the (factor, ok) tuple — they store the return value as the
// PriceData.UserGroupDiscount field which uses 0 = "fallback to GroupRatio" per
// Hermerz/Hermes#68's no-bake semantics.
//
// Use this from billing entry points (ModelPriceHelper, PreWssConsumeQuota, any
// future per-call billing path) so the lookup convention stays in one place.
//
// Constraint: BD must not configure discount = 0 — it would collide with the
// "not configured" sentinel and silently fall back to GroupRatio. For "free
// tier" semantics, use GroupRatio = 0 on a dedicated channel group instead.
// Admin UI rejects 0 values.
func LookupUserGroupDiscount(userGroup, modelName string) float64 {
	if discount, ok := GetUserGroupModelDiscount(userGroup, modelName); ok {
		return discount
	}
	return 0
}
