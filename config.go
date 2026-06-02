package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

var (
	// dynamicPostList 和保护它的读写锁
	dynamicPostListMu sync.RWMutex
	dynamicPostList   []PostConfig // 这个列表将在登录时动态填充
)

// ==========================================
// 配置与常量 (Config)
// ==========================================

const (
	CookieName    = "discord_session"
	PostFiles     = "post_config.json"
	LimitFile     = "refresh.log"
	WindowSeconds = 24 * 3600
	MaxRefreshes  = 300
	Port          = "9966"
	GuildID       = "1159839373001498718" // 可选，特定判断公会ID
	ChannelID     = "1499655365757894707" // 可选，特定判断频道ID （初阶问答）
)

// fetchPostConfigurations 从外部文件获取 PostConfig 列表
func fetchPostConfigurations() ([]PostConfig, error) {
	fmt.Println("fetchPostConfigurations: (从外部文件", PostFiles, "获取频道配置列表)")

	var configs []PostConfig
	fileContent, err := os.ReadFile(PostFiles)
	if err != nil {
		configs = []PostConfig{
			{MonthStr: "1-5月", Title: "2026年1月-2026年5月", SubTitle: "百万Eric_王老板", FileName: "202601-202605.json", PostID: "1510880652566728834"},
			{MonthStr: "6月", Title: "2026年6月", SubTitle: "百万Eric_王老板", FileName: "2026-06.json", PostID: "1510988742318489681"},
		}
		fmt.Printf("✅ %s 获取默认 %d 个频道配置\n", PostFiles, len(configs))
		return configs, nil
	}

	if err := json.Unmarshal(fileContent, &configs); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败: %w", PostFiles, err)
	}

	fmt.Printf("✅ 成功从文件 %s 获取 %d 个频道配置\n", PostFiles, len(configs))
	return configs, nil
}
