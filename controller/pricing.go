package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func filterPricingByUsableGroups(pricing []model.Pricing, usableGroup map[string]string) []model.Pricing {
	if len(pricing) == 0 {
		return pricing
	}
	if len(usableGroup) == 0 {
		return []model.Pricing{}
	}

	filtered := make([]model.Pricing, 0, len(pricing))
	for _, item := range pricing {
		if common.StringsContains(item.EnableGroup, "all") {
			filtered = append(filtered, item)
			continue
		}
		for _, group := range item.EnableGroup {
			if _, ok := usableGroup[group]; ok {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	pricing := model.GetPricing()
	userId, exists := c.Get("id")
	usableGroup := map[string]string{}
	groupRatio := map[string]float64{}
	for s, f := range ratio_setting.GetGroupRatioCopy() {
		groupRatio[s] = f
	}
	var group string
	if exists {
		user, err := model.GetUserCache(userId.(int))
		if err == nil {
			group = user.Group
			for g := range groupRatio {
				ratio, ok := ratio_setting.GetGroupGroupRatio(group, g)
				if ok {
					groupRatio[g] = ratio
				}
			}
		}
	}

	usableGroup = service.GetUserUsableGroups(group)
	pricing = filterPricingByUsableGroups(pricing, usableGroup)
	// check groupRatio contains usableGroup
	for group := range ratio_setting.GetGroupRatioCopy() {
		if _, ok := usableGroup[group]; !ok {
			delete(groupRatio, group)
		}
	}

	// Resolve the per-model discount that billing would actually apply for this
	// user's group, so downstream catalogs (Hermes /pricing/catalog) display the
	// same effective price/discount as settle time. Only configured (>0) models
	// are emitted; absence means "fall back to group_ratio" — mirroring the
	// billing sentinel in relay/helper/price.go (Hermerz/Hermes#127).
	modelDiscount := map[string]float64{}
	// Token models whose model_ratio is UNCONFIGURED (fell back to the default
	// sentinel) — downstream shows "price pending" instead of the bogus fallback
	// price. Uses GetModelRatio's authoritative configured flag, NOT the numeric
	// value: a real model (e.g. gpt-4.5-preview) is legitimately 37.5 (#127).
	unpricedModels := []string{}
	for _, p := range pricing {
		if d := ratio_setting.LookupUserGroupDiscount(group, p.ModelName); d > 0 {
			modelDiscount[p.ModelName] = d
		}
		if p.QuotaType == 1 {
			continue // per-call models are priced via model_price, not model_ratio
		}
		// Use map membership (NOT GetModelRatio's success flag, which returns
		// SelfUseModeEnabled on miss and would mislabel unpriced models as priced
		// in self-use mode).
		if !ratio_setting.IsModelRatioConfigured(p.ModelName) {
			unpricedModels = append(unpricedModels, p.ModelName)
		}
	}

	c.JSON(200, gin.H{
		"success":                   true,
		"data":                      pricing,
		"vendors":                   model.GetVendors(),
		"group_ratio":               groupRatio,
		"user_group_model_discount": modelDiscount,
		"model_ratio_unconfigured":  unpricedModels,
		"usable_group":              usableGroup,
		"supported_endpoint":        model.GetSupportedEndpointMap(),
		"auto_groups":               service.GetUserAutoGroup(group),
		"pricing_version":           "a42d372ccf0b5dd13ecf71203521f9d2",
	})
}

func ResetModelRatio(c *gin.Context) {
	defaultStr := ratio_setting.DefaultModelRatio2JSONString()
	err := model.UpdateOption("ModelRatio", defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	err = ratio_setting.UpdateModelRatioByJSONString(defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "重置模型倍率成功",
	})
}
