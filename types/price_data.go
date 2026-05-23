package types

import "fmt"

type GroupRatioInfo struct {
	GroupRatio        float64
	GroupSpecialRatio float64
	HasSpecialRatio   bool
}

type PriceData struct {
	FreeModel  bool
	ModelPrice float64
	// ModelRatio: 模型市场基线（对齐官方价 USD/M ÷ $2）。NOT pre-multiplied
	// with UserGroupDiscount — settle path applies effective via
	// EffectiveGroupRatio() method.
	ModelRatio           float64
	CompletionRatio      float64
	CacheRatio           float64
	CacheCreationRatio   float64
	CacheCreation5mRatio float64
	CacheCreation1hRatio float64
	ImageRatio           float64
	AudioRatio           float64
	AudioCompletionRatio float64
	OtherRatios          map[string]float64
	UsePrice             bool
	Quota                int // 按次计费的最终额度（MJ / Task）
	QuotaToPreConsume    int // 按量计费的预消耗额度
	// GroupRatioInfo.GroupRatio: 客户分层基线 GroupRatio (raw)。NOT forced
	// to 1.0 when UserGroupDiscount > 0 — settle path picks effective via
	// EffectiveGroupRatio() method.
	GroupRatioInfo GroupRatioInfo
	// UserGroupDiscount: the raw discount factor configured for this
	// (user_group, model) pair (Hermerz/Hermes#51 option A). 0 means "not
	// configured, fallback to ModelRatio × GroupRatio". When > 0, billing
	// uses ModelRatio × UserGroupDiscount (bypassing GroupRatio per option A).
	// Stored as raw factor (not baked into ModelRatio) so log + 对账 can
	// show market baseline + customer discount as separate fields per
	// Hermerz/Hermes#68.
	UserGroupDiscount float64
}

// EffectiveGroupRatio returns the group-level multiplier actually used for
// billing: UserGroupDiscount if explicitly configured for this (group, model)
// pair, otherwise GroupRatioInfo.GroupRatio. Per #51 Phase 1 option A,
// configuring a discount overrides the per-tier GroupRatio so BD's "0.2 in
// admin UI = customer pays 2折 of official" holds regardless of customer tier.
func (p PriceData) EffectiveGroupRatio() float64 {
	if p.UserGroupDiscount > 0 {
		return p.UserGroupDiscount
	}
	return p.GroupRatioInfo.GroupRatio
}

func (p *PriceData) AddOtherRatio(key string, ratio float64) {
	if p.OtherRatios == nil {
		p.OtherRatios = make(map[string]float64)
	}
	if ratio <= 0 {
		return
	}
	p.OtherRatios[key] = ratio
}

func (p *PriceData) ToSetting() string {
	return fmt.Sprintf("ModelPrice: %f, ModelRatio: %f, CompletionRatio: %f, CacheRatio: %f, GroupRatio: %f, UsePrice: %t, CacheCreationRatio: %f, CacheCreation5mRatio: %f, CacheCreation1hRatio: %f, QuotaToPreConsume: %d, ImageRatio: %f, AudioRatio: %f, AudioCompletionRatio: %f", p.ModelPrice, p.ModelRatio, p.CompletionRatio, p.CacheRatio, p.GroupRatioInfo.GroupRatio, p.UsePrice, p.CacheCreationRatio, p.CacheCreation5mRatio, p.CacheCreation1hRatio, p.QuotaToPreConsume, p.ImageRatio, p.AudioRatio, p.AudioCompletionRatio)
}
