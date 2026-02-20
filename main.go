package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// ==========================================
// 控制器层 (Handlers)
// ==========================================

func main() {
	initService()

	// 路由注册
	http.HandleFunc("/login", handleLogin)                     // 登录页 & 提交
	http.HandleFunc("/logout", handleLogout)                   // 登出
	http.HandleFunc("/refresh", authMiddleware(handleRefresh)) // 刷新 (需登录)
	http.HandleFunc("/", authMiddleware(handleIndex))          // 主页 (需登录)

	link := "http://localhost:" + Port
	fmt.Println("-------------------------------------------")
	fmt.Println("✅ 聊天存档查看器已启动")
	fmt.Printf("👉 请访问: %s\n", link)
	fmt.Println("-------------------------------------------")

	openBrowser(link)
	log.Fatal(http.ListenAndServe(":"+Port, nil))
}

// 中间件：验证登录状态
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(CookieName)
		if err != nil || cookie.Value == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// 简单解码 Session (实际生产环境应加密)
		jsonBytes, err := base64.StdEncoding.DecodeString(cookie.Value)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		var session UserSession
		if err := json.Unmarshal(jsonBytes, &session); err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next(w, r)
	}
}

// 获取当前用户 helper
func getCurrentUser(r *http.Request) *UserSession {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return nil
	}
	bytes, _ := base64.StdEncoding.DecodeString(cookie.Value)
	var user UserSession
	json.Unmarshal(bytes, &user)
	return &user
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		token := r.FormValue("token")
		user, err := verifyToken(token)
		if err != nil {
			renderLogin(w, err.Error())
			return
		}

		// 创建 Session
		sessionBytes, _ := json.Marshal(user)
		encoded := base64.StdEncoding.EncodeToString(sessionBytes)

		http.SetCookie(w, &http.Cookie{
			Name:     CookieName,
			Value:    encoded,
			Path:     "/",
			HttpOnly: true,
			MaxAge:   3600 * 24 * 30, // 30天
		})

		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	renderLogin(w, "")
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:   CookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	currentUser := getCurrentUser(r)
	if currentUser == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// 获取用户可访问的所有频道
	fmt.Printf("🔐 正在获取用户 [%s] 的频道权限...\n", currentUser.Username)
	accessibleChannels, err := getUserAllAccessibleChannels(currentUser.Token, currentUser.UserID)
	if err != nil {
		fmt.Printf("⚠️  权限获取失败: %v\n", err)
		accessibleChannels = make(map[string]bool)
	}

	// 检查用户是否有权访问此频道
	if !accessibleChannels[ChannelID] {
		fmt.Printf("⛔ 用户 [%s] 无权访问频道\n", currentUser.Username)
		renderLogin(w, "无权访问频道")
		return
	}

	activeFile := r.URL.Query().Get("f")
	if activeFile == "" && len(ChannelList) > 0 {
		activeFile = ChannelList[0].FileName
	}

	var navItems []NavItem
	for _, cfg := range ChannelList {
		msgs, exists := memoryStore[cfg.FileName]
		count := "0"
		if exists {
			count = fmt.Sprintf("%d", len(msgs))
		}
		navItems = append(navItems, NavItem{
			MonthStr: cfg.MonthStr,
			Title:    cfg.Title,
			SubTitle: cfg.SubTitle,
			FileName: cfg.FileName,
			Count:    count + "条",
			IsActive: (cfg.FileName == activeFile),
		})
	}

	var nodes []*ViewNode
	if msgs, ok := memoryStore[activeFile]; ok {
		nodes = buildViewNodes(msgs, currentUser.UserID)
	}

	renderHome(w, PageData{
		NavItems:    navItems,
		Messages:    nodes,
		ActiveFile:  activeFile,
		ProxyInfo:   ProxyURL,
		CurrentUser: currentUser,
	})
}

func handleRefresh(w http.ResponseWriter, r *http.Request) {
	currentUser := getCurrentUser(r)
	if currentUser == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	targetFile := r.URL.Query().Get("f")

	//// 获取用户的频道权限
	//accessibleChannels, err := getUserAllAccessibleChannels(currentUser.Token, currentUser.UserID)
	//if err != nil {
	//	return
	//}
	//// 检查用户是否有权访问此频道
	//if !accessibleChannels[ChannelID] {
	//	return
	//}

	if allowed, waitTime := checkRateLimit(); !allowed {
		renderLimitError(w, waitTime)
		return
	}

	storeMu.Lock()
	msgs, ok := memoryStore[targetFile]
	storeMu.Unlock()

	if !ok || len(msgs) == 0 {
		http.Redirect(w, r, "/?f="+targetFile, http.StatusSeeOther)
		return
	}

	var targetPostID string
	for _, cfg := range ChannelList {
		if cfg.FileName == targetFile {
			targetPostID = cfg.PostID
			break
		}
	}

	fmt.Printf("🔄 用户 [%s] 正在抓取新消息...\n", currentUser.Username)
	newMsgs, err := fetchNewMessages(currentUser.Token, targetPostID, msgs[0].ID)

	if err == nil {
		storeMu.Lock()
		memoryStore[targetFile] = newMsgs
		storeMu.Unlock()
		fmt.Printf("✅ 同步成功，当前共 %d 条\n", len(newMsgs))
	} else {
		fmt.Printf("❌ 同步失败: %v\n", err)
	}

	http.Redirect(w, r, "/?f="+targetFile, http.StatusSeeOther)
}
