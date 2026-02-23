package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed data/*.json
var embeddedFiles embed.FS
var ProxyURL = ""

// 内存数据库
var memoryStore = make(map[string][]DiscordMessage)
var storeMu sync.Mutex

// ==========================================
// 权限检查 - 从 Discord API 获取用户可访问的频道
// ==========================================

const VIEW_CHANNEL uint64 = 0x400

// toMask: 把 string permissions 转成 uint64（你文件中若已有此函数可删除重复）
func toMask(s string) uint64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// applyOverwrite: 按 Discord 规则把 allow/deny 应用到当前 perms 上
func applyOverwrite(perms uint64, allowStr, denyStr string) uint64 {
	allow := toMask(allowStr)
	deny := toMask(denyStr)
	perms = (perms & ^deny) | allow
	return perms
}

// 获取用户在 guild 中的角色列表
func getUserRolesInGuild(token, guildID, userID string) ([]string, error) {
	client := getClient()
	url := fmt.Sprintf("https://discord.com/api/v9/guilds/%s/members/%s", guildID, userID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("get member failed: %d", resp.StatusCode)
	}

	var member struct {
		Roles []string `json:"roles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&member); err != nil {
		return nil, err
	}
	return member.Roles, nil
}

// 获取 guild 中所有角色及其 permissions，返回 map[roleID]perm
func getGuildRolesPerms(token, guildID string) (map[string]uint64, error) {
	client := getClient()
	url := fmt.Sprintf("https://discord.com/api/v9/guilds/%s/roles", guildID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("get roles failed: %d", resp.StatusCode)
	}

	var roles []struct {
		ID          string `json:"id"`
		Permissions string `json:"permissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&roles); err != nil {
		return nil, err
	}

	m := make(map[string]uint64, len(roles))
	for _, r := range roles {
		m[r.ID] = toMask(r.Permissions)
	}
	return m, nil
}

// 计算 basePerm（用户角色并集）
func computeBasePerm(userRoles []string, rolePerms map[string]uint64) uint64 {
	var base uint64 = 0
	for _, rid := range userRoles {
		if p, ok := rolePerms[rid]; ok {
			base |= p
		}
	}
	return base
}

// 按顺序应用 @everyone -> roles -> member 的 overwrites，返回最终是否有 VIEW_CHANNEL
func channelReadableByUser(ch DiscordChannel, userRoles []string, perms uint64, guildID, userID string) bool {
	// 1) 应用 @everyone 覆写（type==0 且 id==guildID）
	for _, ow := range ch.PermissionOverwrites {
		if ow.Type == 0 && ow.ID == guildID {
			perms = applyOverwrite(perms, ow.Allow, ow.Deny)
			//fmt.Printf("  应用 @everyone 覆写 allow=%s deny=%s -> perms=%064b\n", ow.Allow, ow.Deny, perms)
			break
		}
	}

	// 2) 按用户角色逐个应用 role overwrites（type==0 且 id==roleID）
	for _, roleID := range userRoles {
		for _, ow := range ch.PermissionOverwrites {
			if ow.Type == 0 && ow.ID == roleID {
				perms = applyOverwrite(perms, ow.Allow, ow.Deny)
				//fmt.Printf("  应用 role(%s) 覆写 allow=%s deny=%s -> perms=%064b\n", roleID, ow.Allow, ow.Deny, perms)
				break
			}
		}
	}

	// 3) 最后应用 member 覆写（type==1 且 id==userID）
	for _, ow := range ch.PermissionOverwrites {
		if ow.Type == 1 && ow.ID == userID {
			perms = applyOverwrite(perms, ow.Allow, ow.Deny)
			//fmt.Printf("  应用 member(%s) 覆写 allow=%s deny=%s -> perms=%064b\n", userID, ow.Allow, ow.Deny, perms)
			break
		}
	}

	// 4) 判断 VIEW_CHANNEL
	has := (perms & VIEW_CHANNEL) != 0
	if has {
		fmt.Printf("  ✅ 最终权限: %s\n", ch.Name)
		// fmt.Printf("  ✅ 最终权限: 有 VIEW_CHANNEL (perms=%064b)\n", perms)
	} else {
		// fmt.Printf("  ❌ 最终权限: 无 VIEW_CHANNEL (perms=%064b)\n", perms)
	}
	return has
}

// 获取某个公会中用户可读的频道集合（传入 userID）
// 这里使用 channelReadableByUser（复杂计算，不调用 /permissions/@me）
func getUserAccessibleChannels(token, guildID, userID string) (map[string]bool, error) {
	client := getClient()
	discordUrl := fmt.Sprintf("https://discord.com/api/v9/guilds/%s/channels", guildID)
	req, _ := http.NewRequest("GET", discordUrl, nil)
	req.Header.Set("Authorization", token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		// body, _ := io.ReadAll(resp.Body)
		// fmt.Printf("DBG channels body on error: %s\n", string(body))
		return nil, fmt.Errorf("failed to fetch channels, status: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	// 可选择打印完整 body 供调试（production 可注释）
	// fmt.Println("DBG channels body:", string(body))

	var channels []DiscordChannel
	if err := json.Unmarshal(body, &channels); err != nil {
	}

	// 获取用户角色与 guild 角色权限映射
	userRoles, err := getUserRolesInGuild(token, guildID, userID)
	if err != nil {
		// 无法拿到 member 信息时，保守处理为不可读，并打印原因
		fmt.Printf("⚠️ getUserRolesInGuild failed: %v\n", err)
		return nil, err
	}
	rolePerms, err := getGuildRolesPermsWithCache(token, guildID)
	if err != nil {
		fmt.Printf("⚠️ getGuildRolesPermsWithCache failed: %v\n", err)
		return nil, err
	}

	basePerm := computeBasePerm(userRoles, rolePerms)
	perms := basePerm

	accessible := make(map[string]bool)
	for _, ch := range channels {
		// 只处理当前目标 guild 的频道
		if ch.GuildID != guildID {
			continue
		}
		//fmt.Printf("🔐 计算 user %s, channel %s:%s in guild %s\n", userID, ch.ID, ch.Name, guildID)
		if channelReadableByUser(ch, userRoles, perms, guildID, userID) {
			accessible[ch.ID] = true
		}
	}
	return accessible, nil
}

// getUserAllAccessibleChannels: 遍历用户加入的 guild（如果你只关注特定 GuildID 上的频道，可优化为只处理 GuildID）
func getUserAllAccessibleChannels(token, userID string) (map[string]bool, error) {
	// 先查看缓存
	if cached, found := getPermissionFromCache(userID, GuildID); found {
		return cached, nil
	}

	client := getClient()
	url := "https://discord.com/api/v9/users/@me/guilds?limit=200"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch guilds, status: %d", resp.StatusCode)
	}

	var guilds []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&guilds); err != nil {
		return nil, err
	}

	allAccessible := make(map[string]bool)
	for _, g := range guilds {
		if g.ID != GuildID {
			continue
		}
		chs, err := getUserAccessibleChannels(token, g.ID, userID)
		if err == nil {
			for cid := range chs {
				allAccessible[cid] = true
			}
		} else {
			fmt.Printf("⚠️ getUserAccessibleChannels failed for guild %s: %v\n", g.ID, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 保存到缓存
	setPermissionCache(userID, GuildID, allAccessible)

	return allAccessible, nil
}

// ==========================================
// 服务层 (Service & Logic)
// ==========================================

// 初始化加载
func initService() {
	// 加载代理
	if content, err := os.ReadFile("proxy.txt"); err == nil {
		ProxyURL = strings.TrimSpace(string(content))
	}
	// 加载数据
	count := 0
	dynamicPostListMu.RLock()
	for _, cfg := range dynamicPostList {
		path := "data/" + cfg.FileName
		bytes, err := embeddedFiles.ReadFile(path)
		if err != nil {
			continue
		}
		var msgs []DiscordMessage
		if err := json.Unmarshal(bytes, &msgs); err == nil {
			sort.Slice(msgs, func(i, j int) bool { return msgs[i].ID < msgs[j].ID })
			memoryStore[cfg.FileName] = msgs
			count++
		}
	}
	dynamicPostListMu.RUnlock()
	fmt.Printf("📦 已加载 %d 个数据文件\n", count)
}

// 验证Token并获取用户信息
func verifyToken(token string) (*UserSession, error) {
	client := getClient()
	req, _ := http.NewRequest("GET", "https://discord.com/api/v9/users/@me", nil)
	req.Header.Set("Authorization", token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("无效的 Token (状态码: %d)", resp.StatusCode)
	}

	var user UserSession
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}
	user.Avatar = getAvatar(user.UserID, user.Avatar)
	user.Token = token // 保存 Token 供后续使用
	return &user, nil
}

// 抓取消息逻辑
func fetchNewMessages(token, chanID, sinceID string) ([]DiscordMessage, error) {
	client := getClient()
	messageMap := make(map[string]DiscordMessage) // 用于去重

	// 辅助函数：在一个消息批次中找到最小和最大的 ID
	findMinMaxID := func(msgs []DiscordMessage) (string, string) {
		if len(msgs) == 0 {
			return "", ""
		}
		minID := msgs[0].ID
		maxID := msgs[0].ID
		for _, msg := range msgs {
			if msg.ID < minID { // Discord snowflake ID 是时间有序的
				minID = msg.ID
			}
			if msg.ID > maxID {
				maxID = msg.ID
			}
		}
		return minID, maxID
	}

	if sinceID == "" {
		// 情况1: 抓取所有消息 (从最新的开始，逐步向过去抓取)
		// 首先抓取第一批最新消息
		query := "limit=100"
		initialBatch, err := fetchBatch(client, token, chanID, query)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch initial batch of messages: %w", err)
		}
		if len(initialBatch) == 0 {
			return []DiscordMessage{}, nil // 频道中没有消息
		}

		for _, msg := range initialBatch {
			messageMap[msg.ID] = msg
		}

		// 循环向过去抓取 (使用 'before' 参数)
		currentOldestID, _ := findMinMaxID(initialBatch)
		for {
			query = "limit=100&before=" + currentOldestID
			batch, err := fetchBatch(client, token, chanID, query)
			if err != nil {
				log.Printf("Error fetching messages before %s: %v. Continuing with collected messages.", currentOldestID, err)
				break // 遇到错误，停止并返回已收集的消息
			}
			if len(batch) == 0 {
				break // 没有更旧的消息了
			}

			newOldestID, _ := findMinMaxID(batch)
			for _, msg := range batch {
				messageMap[msg.ID] = msg
			}

			// 如果最旧的消息ID没有变化，说明已经到达频道的起点
			if newOldestID == currentOldestID {
				break
			}
			currentOldestID = newOldestID
			time.Sleep(200 * time.Millisecond) // 尊重 Discord API 速率限制
		}

	} else {
		// 情况2: 抓取比 sinceID 更新的消息 (增量更新)
		// 从 sinceID 之后开始抓取 (使用 'after' 参数)
		currentNewestID := sinceID
		for {
			query := "limit=100&after=" + currentNewestID
			batch, err := fetchBatch(client, token, chanID, query)
			if err != nil {
				log.Printf("Error fetching messages after %s: %v. Continuing with collected messages.", currentNewestID, err)
				break // 遇到错误，停止并返回已收集的消息
			}
			if len(batch) == 0 {
				break // 没有更新的消息了
			}

			_, newestIDinBatch := findMinMaxID(batch)
			for _, msg := range batch {
				messageMap[msg.ID] = msg
			}

			// 如果最新消息ID没有变化，说明已经抓取到最新的消息
			if newestIDinBatch == currentNewestID {
				break
			}
			currentNewestID = newestIDinBatch
			time.Sleep(200 * time.Millisecond) // 尊重 Discord API 速率限制
		}
	}

	// 将 map 中的消息转换为切片
	var result []DiscordMessage
	for _, msg := range messageMap {
		result = append(result, msg)
	}

	// 按 ID 升序排序 (从旧到新)
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})

	return result, nil
}

// fetchBatch 执行单个 Discord API 请求来获取消息批次。
func fetchBatch(client *http.Client, token, chanID, query string) ([]DiscordMessage, error) {
	discordUrl := fmt.Sprintf("https://discord.com/api/v9/channels/%s/messages?%s", chanID, query)
	req, err := http.NewRequest("GET", discordUrl, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for %s: %w", discordUrl, err)
	}

	// 假设传入的 token 已经包含了 "Bot " 前缀（如果它是一个 Bot Token）
	req.Header.Set("Authorization", token)
	req.Header.Set("User-Agent", "DiscordArchiveViewer (CustomApp, 1.0)") // 建议设置 User-Agent

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request to %s: %w", discordUrl, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body) // 读取响应体以获取更多错误信息
		return nil, fmt.Errorf("received non-200 status code %d from %s: %s", resp.StatusCode, discordUrl, string(bodyBytes))
	}

	var msgs []DiscordMessage
	if err := json.NewDecoder(resp.Body).Decode(&msgs); err != nil {
		return nil, fmt.Errorf("failed to decode messages from %s: %w", discordUrl, err)
	}
	return msgs, nil
}

// HTTP Client 工厂
func getClient() *http.Client {
	client := &http.Client{Timeout: 30 * time.Second}
	if ProxyURL != "" {
		u, err := url.Parse(ProxyURL)
		if err == nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
		}
	}
	return client
}

// 限流检查
func checkRateLimit() (bool, string) {
	storeMu.Lock()
	defer storeMu.Unlock()

	var logData RateLog
	bytes, err := os.ReadFile(LimitFile)
	if err == nil {
		json.Unmarshal(bytes, &logData)
	}

	now := time.Now().Unix()
	var validTime []int64
	for _, ts := range logData.Timestamps {
		if now-ts < WindowSeconds {
			validTime = append(validTime, ts)
		}
	}

	if len(validTime) >= MaxRefreshes {
		waitSec := WindowSeconds - (now - validTime[0])
		return false, fmt.Sprintf("%d小时%d分", waitSec/3600, (waitSec%3600)/60)
	}

	validTime = append(validTime, now)
	logData.Timestamps = validTime
	newBytes, _ := json.Marshal(logData)
	os.WriteFile(LimitFile, newBytes, 0666)

	return true, ""
}
