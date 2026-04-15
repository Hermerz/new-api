package model

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// UserCacheStats 用户缓存命中统计（按小时聚合）
type UserCacheStats struct {
	Id               int    `json:"id"`
	UserID           int    `json:"user_id" gorm:"index"`
	Username         string `json:"username" gorm:"index;size:64;default:''"`
	ModelName        string `json:"model_name" gorm:"index;size:64;default:''"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;index"`
	PromptTokens     int64  `json:"prompt_tokens" gorm:"default:0"`
	CachedTokens     int64  `json:"cached_tokens" gorm:"default:0"`
	CompletionTokens int64  `json:"completion_tokens" gorm:"default:0"`
	CacheHitCount    int64  `json:"cache_hit_count" gorm:"default:0"`
	TotalRequests    int64  `json:"total_requests" gorm:"default:0"`
}

var CacheUserCacheStats = make(map[string]*UserCacheStats)
var CacheUserCacheStatsLock = sync.Mutex{}

// UpdateCacheStatsData 定期将内存缓存持久化到数据库（与 UpdateQuotaData 相同周期）
func UpdateCacheStatsData() {
	for {
		if common.DataExportEnabled {
			common.SysLog("正在更新缓存统计数据...")
			SaveUserCacheStatsCache()
		}
		time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
	}
}

func logUserCacheStatsInner(userId int, username, modelName string, promptTokens, cacheTokens, completionTokens int, createdAt int64) {
	key := fmt.Sprintf("%d-%s-%s-%d", userId, username, modelName, createdAt)
	stats, ok := CacheUserCacheStats[key]
	if ok {
		stats.TotalRequests++
		stats.PromptTokens += int64(promptTokens)
		stats.CachedTokens += int64(cacheTokens)
		stats.CompletionTokens += int64(completionTokens)
		if cacheTokens > 0 {
			stats.CacheHitCount++
		}
	} else {
		hitCount := int64(0)
		if cacheTokens > 0 {
			hitCount = 1
		}
		stats = &UserCacheStats{
			UserID:           userId,
			Username:         username,
			ModelName:        modelName,
			CreatedAt:        createdAt,
			PromptTokens:     int64(promptTokens),
			CachedTokens:     int64(cacheTokens),
			CompletionTokens: int64(completionTokens),
			TotalRequests:    1,
			CacheHitCount:    hitCount,
		}
	}
	CacheUserCacheStats[key] = stats
}

// LogUserCacheStats 记录单次请求的缓存统计到内存（按小时聚合）
func LogUserCacheStats(userId int, username, modelName string, promptTokens, cacheTokens, completionTokens int, createdAt int64) {
	createdAt = createdAt - (createdAt % 3600) // 精确到小时
	CacheUserCacheStatsLock.Lock()
	defer CacheUserCacheStatsLock.Unlock()
	logUserCacheStatsInner(userId, username, modelName, promptTokens, cacheTokens, completionTokens, createdAt)
}

// SaveUserCacheStatsCache 将内存中的缓存统计持久化到数据库
func SaveUserCacheStatsCache() {
	CacheUserCacheStatsLock.Lock()
	defer CacheUserCacheStatsLock.Unlock()
	size := len(CacheUserCacheStats)
	for _, stats := range CacheUserCacheStats {
		existing := &UserCacheStats{}
		DB.Table("user_cache_stats").Where(
			"user_id = ? and username = ? and model_name = ? and created_at = ?",
			stats.UserID, stats.Username, stats.ModelName, stats.CreatedAt,
		).First(existing)
		if existing.Id > 0 {
			DB.Table("user_cache_stats").Where("id = ?", existing.Id).Updates(map[string]interface{}{
				"total_requests":    gorm.Expr("total_requests + ?", stats.TotalRequests),
				"prompt_tokens":     gorm.Expr("prompt_tokens + ?", stats.PromptTokens),
				"cached_tokens":     gorm.Expr("cached_tokens + ?", stats.CachedTokens),
				"completion_tokens": gorm.Expr("completion_tokens + ?", stats.CompletionTokens),
				"cache_hit_count":   gorm.Expr("cache_hit_count + ?", stats.CacheHitCount),
			})
		} else {
			DB.Table("user_cache_stats").Create(stats)
		}
	}
	CacheUserCacheStats = make(map[string]*UserCacheStats)
	common.SysLog(fmt.Sprintf("保存缓存统计数据成功，共保存%d条数据", size))
}

// GetUserCacheStatsByUserId 查询用户在时间范围内的缓存统计
func GetUserCacheStatsByUserId(userId int, startTime, endTime int64) ([]*UserCacheStats, error) {
	var stats []*UserCacheStats
	err := DB.Table("user_cache_stats").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).
		Order("created_at desc").
		Find(&stats).Error
	return stats, err
}
