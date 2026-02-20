package main

import (
	"fmt"
	"sync"
	"time"
)

type PermissionCache struct {
	Channels  map[string]bool // channelID -> hasAccess
	ExpiresAt time.Time
}

var permCacheMu sync.RWMutex
var permCache = make(map[string]*PermissionCache) // key: userID+guildID

// 从缓存获取用户权限（如果未过期）
func getPermissionFromCache(userID, guildID string) (map[string]bool, bool) {
	permCacheMu.RLock()
	defer permCacheMu.RUnlock()

	key := userID + ":" + guildID
	if cache, exists := permCache[key]; exists && time.Now().Before(cache.ExpiresAt) {
		fmt.Printf("✅ 使用缓存权限 (user=%s)\n", userID)
		return cache.Channels, true
	}
	return nil, false
}

// 保存权限到缓存（2小时过期）
func setPermissionCache(userID, guildID string, channels map[string]bool) {
	permCacheMu.Lock()
	defer permCacheMu.Unlock()

	key := userID + ":" + guildID
	permCache[key] = &PermissionCache{
		Channels:  channels,
		ExpiresAt: time.Now().Add(2 * time.Hour),
	}
	fmt.Printf("💾 缓存权限 (user=%s, ttl=5min)\n", userID)
}

// 缓存 guild 角色权限（1天过期）
var rolePermsCacheMu sync.RWMutex
var rolePermsCache = make(map[string]map[string]uint64)
var rolePermsCacheTime = make(map[string]time.Time)

func getGuildRolesPermsWithCache(token, guildID string) (map[string]uint64, error) {
	rolePermsCacheMu.RLock()
	if cache, exists := rolePermsCache[guildID]; exists && time.Now().Before(rolePermsCacheTime[guildID].Add(24*time.Hour)) {
		rolePermsCacheMu.RUnlock()
		fmt.Printf("✅ 使用缓存 guild roles (guild=%s)\n", guildID)
		return cache, nil
	}
	rolePermsCacheMu.RUnlock()

	// 调用 API 获取
	rolePerms, err := getGuildRolesPerms(token, guildID)
	if err == nil {
		rolePermsCacheMu.Lock()
		rolePermsCache[guildID] = rolePerms
		rolePermsCacheTime[guildID] = time.Now()
		rolePermsCacheMu.Unlock()
	}
	return rolePerms, err
}
