package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// ==========================================
// 模型定义 (Models)
// ==========================================

// Discord 原始数据模型
type DiscordMessage struct {
	ID          string       `json:"id"`
	Content     string       `json:"content"`
	Timestamp   string       `json:"timestamp"`
	Author      Author       `json:"author"`
	Attachments []Attachment `json:"attachments"`
	MsgRef      *MsgRef      `json:"message_reference,omitempty"`
}
type Author struct {
	Username string `json:"username"`
	Avatar   string `json:"avatar"`
	ID       string `json:"id"`
}
type Attachment struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

type MsgRef struct {
	MessageID string `json:"message_id"`
}

// 视图渲染模型
type ViewNode struct {
	ID, AuthorName, Avatar, Time, Content string
	RawTime                               time.Time
	Images                                []string
	Replies                               []*ViewNode
	IsReply                               bool
	ReplyTarget                           string
	IsMention                             bool
	IsMe                                  bool // 是否是当前登录用户
}

// 页面数据包
type PageData struct {
	NavItems    []NavItem
	Messages    []*ViewNode
	ActiveFile  string
	ProxyInfo   string
	CurrentUser *UserSession
}
type NavItem struct {
	MonthStr, Title, SubTitle, FileName, Count string
	IsActive                                   bool
}

// 用户会话信息
type UserSession struct {
	Token    string `json:"token"`
	UserID   string `json:"id"`
	Username string `json:"username"`
	Avatar   string `json:"avatar"`
}

// 限流日志
type RateLog struct {
	Timestamps []int64 `json:"timestamps"`
}

// 如果 Discord 返回该字段，表示该频道对不同对象的权限覆盖
type Overwrite struct {
	ID    string `json:"id"`
	Type  int    `json:"type"` // 0 = role, 1 = member
	Allow string `json:"allow"`
	Deny  string `json:"deny"`
}

// Discord API 返回的频道对象
type DiscordChannel struct {
	ID                   string      `json:"id"`
	Name                 string      `json:"name"`
	GuildID              string      `json:"guild_id"`
	Type                 int         `json:"type"`
	Position             int         `json:"position"`
	PermissionOverwrites []Overwrite `json:"permission_overwrites"`
}

type Uint64Like struct {
	V uint64
}

func (u *Uint64Like) UnmarshalJSON(b []byte) error {
	// 先尝试作为数字
	var n uint64
	if err := json.Unmarshal(b, &n); err == nil {
		u.V = n
		return nil
	}
	// 再尝试作为字符串
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		u.V = v
		return nil
	}
	return fmt.Errorf("invalid uint64 like: %s", string(b))
}
