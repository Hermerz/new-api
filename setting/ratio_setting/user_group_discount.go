package ratio_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/types"
)

// user_group_model_discount implements the 2D pricing matrix described in
// Hermerz/Hermes#51 / Hermerz/new-api#3: a per-user-group, per-model discount
// applied on top of the global ModelRatio.
//
// Storage shape (options table key "user_group_model_discount_setting"):
//
//	{
//	  "user_group_model_discount": {
//	    "default": {
//	      "gpt-5": 0.20,            // 2C 客户 gpt-5 走 2 折
//	      "claude-opus": 0.40       // 2C 客户 claude-opus 走 4 折
//	    },
//	    "enterprise": {
//	      "gpt-5": 0.40,            // 2B 客户 gpt-5 走 4 折
//	      "claude-opus": 0.60       // 2B 客户 claude-opus 走 6 折
//	    }
//	  }
//	}
//
// Final billing ratio becomes:
//
//	ratio = ModelRatio[model]
//	      * UserGroupModelDiscount[group][model] ?? 1.0   <-- inserted here
//	      * GroupRatio[channel_group]
//
// Wildcard matching mirrors model_ratio.go: callers receive a discount when
// either an exact match OR the *-openai-compact wildcard key exists.
//
// Zero-config behavior: with an empty map (the default), GetUserGroupModelDiscount
// always returns (1.0, false), so the multiplier is a no-op and billing
// matches the pre-feature behavior exactly.

var defaultUserGroupModelDiscount = map[string]map[string]float64{
	// Empty by default. Operators populate via the admin Setting page
	// (PUT /api/option/ with key=user_group_model_discount_setting).
}

var userGroupModelDiscountMap = types.NewRWMap[string, map[string]float64]()

// UserGroupModelDiscountSetting is the persisted shape registered with
// config.GlobalConfig. The single nested-map field uses the same RWMap
// pattern as defaultGroupGroupRatio in group_ratio.go.
type UserGroupModelDiscountSetting struct {
	UserGroupModelDiscount *types.RWMap[string, map[string]float64] `json:"user_group_model_discount"`
}

var userGroupModelDiscountSetting UserGroupModelDiscountSetting

func init() {
	userGroupModelDiscountMap.AddAll(defaultUserGroupModelDiscount)

	userGroupModelDiscountSetting = UserGroupModelDiscountSetting{
		UserGroupModelDiscount: userGroupModelDiscountMap,
	}

	config.GlobalConfig.Register("user_group_model_discount_setting", &userGroupModelDiscountSetting)
}

// GetUserGroupModelDiscount returns the discount multiplier for the given
// (userGroup, modelName) pair.
//
// Lookup order:
//  1. Exact match: discount[userGroup][modelName]
//  2. Wildcard: discount[userGroup]["*-openai-compact"] when modelName has
//     the compact suffix (mirrors GetModelRatio's wildcard behavior).
//  3. Default: (1.0, false) — caller multiplies by 1.0 = no-op.
//
// Returning the second-return-value `ok` lets the caller distinguish
// "configured to 1.0" vs "not configured" for logging / observability.
func GetUserGroupModelDiscount(userGroup, modelName string) (float64, bool) {
	modelMap, ok := userGroupModelDiscountMap.Get(userGroup)
	if !ok || modelMap == nil {
		return 1.0, false
	}

	name := FormatMatchingModelName(modelName)
	if discount, ok := modelMap[name]; ok {
		return discount, true
	}

	if strings.HasSuffix(name, CompactModelSuffix) {
		if discount, ok := modelMap[CompactWildcardModelKey]; ok {
			return discount, true
		}
	}

	return 1.0, false
}

// UserGroupModelDiscount2JSONString serializes the whole map for admin UI display.
func UserGroupModelDiscount2JSONString() string {
	return userGroupModelDiscountMap.MarshalJSONString()
}

// UpdateUserGroupModelDiscountByJSONString accepts JSON of shape
// map[string]map[string]float64 and atomically replaces the in-memory map.
// Returns an error on malformed JSON without touching state.
func UpdateUserGroupModelDiscountByJSONString(jsonStr string) error {
	return types.LoadFromJsonString(userGroupModelDiscountMap, jsonStr)
}

// GetUserGroupModelDiscountCopy returns a shallow snapshot of the current map.
// Useful for admin endpoints that want to return current config without
// risking RWMap concurrent-modification surprises.
func GetUserGroupModelDiscountCopy() map[string]map[string]float64 {
	return userGroupModelDiscountMap.ReadAll()
}
