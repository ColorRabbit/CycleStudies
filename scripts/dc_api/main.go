package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type Config struct {
	ChannelID string `json:"channel_id"`
	AuthToken string `json:"auth_token"`
	ProxyAddr string `json:"proxy_addr"`
}

// DiscordMessage 结构（简化，与你的数据结构保持兼容）
type Author struct {
	Username string `json:"username"`
	Avatar   string `json:"avatar"`
	ID       string `json:"id"`
}

type Attachment struct {
	URL string `json:"url"`
}

type MessageReference struct {
	MessageID string `json:"message_id"`
}

type DiscordMessage struct {
	ID               string            `json:"id"`
	Content          string            `json:"content"`
	Timestamp        string            `json:"timestamp"`
	Author           Author            `json:"author"`
	Attachments      []Attachment      `json:"attachments"`
	MessageReference *MessageReference `json:"message_reference,omitempty"`
}

// main
func main() {
	outFile := flag.String("o", "discord_chat_data.json", "输出的 JSON 文件名（可带路径，例如 ../../data/2025-01.json）")
	flag.Parse()

	cfg, err := loadConfig()
	if err != nil {
		fmt.Println("加载配置失败:", err)
		return
	}
	// 抓取数据
	messages := scrapeMessages(cfg)
	if len(messages) == 0 {
		fmt.Println("未抓取到数据。")
		return
	}
	// 排序
	sort.Slice(messages, func(i, j int) bool { return messages[i].ID < messages[j].ID })

	// 写出 JSON
	saveToJSON(messages, *outFile)
}

// 读取并合并配置：config.json + config.local.json 覆盖
func loadConfig() (Config, error) {
	var cfg Config

	// 读取 config.json
	data, err := os.ReadFile("config.json")
	if err != nil {
		return cfg, fmt.Errorf("找不到 config.json，请确保在当前目录下有该文件")
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config.json 解析失败: %v", err)
	}

	// 如果有本地覆盖 cfg.local.json，合并覆盖
	if b, err := os.ReadFile("config.local.json"); err == nil {
		var local Config
		if err := json.Unmarshal(b, &local); err == nil {
			// 只覆盖非空字段
			if strings.TrimSpace(local.ChannelID) != "" {
				cfg.ChannelID = local.ChannelID
			}
			if strings.TrimSpace(local.AuthToken) != "" {
				cfg.AuthToken = local.AuthToken
			}
			if strings.TrimSpace(local.ProxyAddr) != "" {
				cfg.ProxyAddr = local.ProxyAddr
			}
		}
	}

	return cfg, nil
}

// 抓取数据（按照 Discord API 的分页方式）
func scrapeMessages(cfg Config) []DiscordMessage {
	transport := http.DefaultTransport
	if cfg.ProxyAddr != "" {
		u, err := url.Parse(cfg.ProxyAddr)
		if err == nil {
			transport = &http.Transport{Proxy: http.ProxyURL(u)}
		}
	}
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	var all []DiscordMessage
	lastID := ""

	for {
		reqURL := fmt.Sprintf("https://discord.com/api/v9/channels/%s/messages?limit=100", cfg.ChannelID)
		if lastID != "" {
			reqURL += "&before=" + lastID
		}
		req, _ := http.NewRequest("GET", reqURL, nil)
		req.Header.Set("Authorization", cfg.AuthToken)
		req.Header.Set("User-Agent", "Mozilla/5.0")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("网络错误:", err)
			break
		}

		// 429 限流处理
		if resp.StatusCode == 429 {
			fmt.Println("Rate Limit，等待 5 秒后重试...")
			resp.Body.Close()
			time.Sleep(5 * time.Second)
			continue
		}
		if resp.StatusCode != 200 {
			fmt.Printf("API 错误: %s\n", resp.Status)
			resp.Body.Close()
			break
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			fmt.Println("读取响应失败:", err)
			break
		}

		var batch []DiscordMessage
		if err := json.Unmarshal(body, &batch); err != nil {
			fmt.Println("JSON 解析失败:", err)
			break
		}
		if len(batch) == 0 {
			break
		}

		all = append(all, batch...)
		lastID = batch[len(batch)-1].ID

		fmt.Printf("已抓取 %d 条数据，最新 ID: %s\n", len(all), batch[0].ID)
		time.Sleep(1 * time.Second)
	}
	return all
}

// 保存为 JSON 文件
func saveToJSON(data []DiscordMessage, path string) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Println("创建输出文件失败:", err)
		return
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		fmt.Println("写入输出失败:", err)
		return
	}
	fmt.Printf("已将 %d 条数据写入 %s\n", len(data), path)
}
