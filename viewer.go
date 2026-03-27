package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ==========================================
// 视图逻辑 (View & Templates)
// ==========================================
func buildViewNodes(raw []DiscordMessage, myID string) []*ViewNode {
	nodeMap := make(map[string]*ViewNode)
	var mainAxis []*ViewNode

	// 第一步：初步转换
	for _, m := range raw {
		t, _ := time.Parse(time.RFC3339, m.Timestamp)
		var imgs []string
		for _, att := range m.Attachments {
			imgs = append(imgs, att.URL)
		}

		isMe := (m.Author.ID == myID)

		replyToID := ""
		if m.MsgRef != nil {
			replyToID = m.MsgRef.MessageID
		}

		node := &ViewNode{
			ID:          m.ID,
			AuthorName:  m.Author.Username,
			Avatar:      getAvatar(m.Author.ID, m.Author.Avatar),
			Time:        t.Format("2006-01-02 15:04"),
			RawTime:     t,
			Content:     m.Content,
			Images:      imgs,
			ReplyTarget: "",
			ReplyToID:   replyToID,
			IsReply:     false,
			IsMention:   false,
			IsMe:        isMe,
		}
		nodeMap[m.ID] = node
	}

	// 第二步：构建“根消息 + 扁平回复流”结构
	findRoot := func(node *ViewNode) *ViewNode {
		visited := map[string]bool{}
		curr := node

		for curr != nil && curr.ReplyToID != "" {
			if visited[curr.ID] {
				break
			}
			visited[curr.ID] = true

			parent, ok := nodeMap[curr.ReplyToID]
			if !ok {
				break
			}
			curr = parent
		}
		return curr
	}

	for _, m := range raw {
		curr := nodeMap[m.ID]

		if m.MsgRef != nil {
			if parent, ok := nodeMap[m.MsgRef.MessageID]; ok {
				curr.IsReply = true
				curr.ReplyTarget = parent.AuthorName

				root := findRoot(curr)
				if root != nil && root.ID != curr.ID {
					root.Replies = append(root.Replies, curr)
					continue
				}
			}
		}

		mainAxis = append(mainAxis, curr)
	}

	// 这个函数会被复用：既处理主消息，也处理回复列表
	processNodes := func(nodes []*ViewNode) []*ViewNode {
		if len(nodes) == 0 {
			return nil
		}

		// A. 执行合并 (保持5分钟限制，保持时间线清晰)
		var merged []*ViewNode
		var last *ViewNode
		for _, curr := range nodes {
			shouldMerge := false
			if last != nil && last.AuthorName == curr.AuthorName {
				diff := curr.RawTime.Sub(last.RawTime)
				if diff >= 0 && diff <= 5*time.Minute {
					shouldMerge = true
				}
			}

			if shouldMerge {
				if curr.Content != "" {
					if last.Content != "" {
						last.Content += "\n\n" + curr.Content
					} else {
						last.Content = curr.Content
					}
				}
				last.Images = append(last.Images, curr.Images...)
				// 此时回复节点通常不会再有下级回复，简单追加即可
				last.Replies = append(last.Replies, curr.Replies...)
			} else {
				merged = append(merged, curr)
				last = curr
			}
		}

		// B. 高亮判定
		for _, node := range merged {
			hasEveryone := strings.Contains(node.Content, "@everyone")
			hqQuestion := strings.Contains(node.Content, "优质问题")
			hqQuestion2 := strings.Contains(node.Content, "优质提问")
			isNewbieQA := strings.Contains(node.Content, "新手问答")
			if (hasEveryone || hqQuestion || hqQuestion2) && !isNewbieQA {
				node.IsMention = true
			} else {
				node.IsMention = false
			}
		}

		// C. 传染逻辑 (只要下面亮了，且是同一人，上面也得亮)
		for i := len(merged) - 1; i > 0; i-- {
			curr := merged[i]
			prev := merged[i-1]
			if curr.IsMention && curr.AuthorName == prev.AuthorName {
				prev.IsMention = true
			}
		}

		return merged
	}

	// 第三步：处理主轴消息
	finalRoot := processNodes(mainAxis)

	// 删掉对回复列表的 merge，fix来回回复消息bug
	//for _, node := range finalRoot {
	//	if len(node.Replies) > 0 {
	//		node.Replies = processNodes(node.Replies)
	//	}
	//}

	return finalRoot
}

func getAvatar(id, hash string) string {
	if hash == "" {
		return "https://cdn.discordapp.com/embed/avatars/0.png"
	}
	return fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", id, hash)
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	case "linux":
		exec.Command("xdg-open", url).Start()
	}
}

// ---------------- HTML 渲染 ----------------

func renderLogin(w http.ResponseWriter, errStr string) {
	tpl := `
<!DOCTYPE html>
<html>
<head>
    <title>登录 - 聊天存档</title>
    <style>
        body { font-family: sans-serif; display: flex; justify-content: center; align-items: center; height: 100vh; background: #2f3136; color: #dcddde; margin: 0; }
        .login-box { background: #36393f; padding: 40px; border-radius: 5px; box-shadow: 0 2px 10px 0 rgba(0,0,0,0.2); width: 400px; }
        h2 { text-align: center; color: #fff; margin-bottom: 20px; }
        .input-group { margin-bottom: 20px; }
        label { display: block; margin-bottom: 8px; font-size: 12px; font-weight: bold; text-transform: uppercase; color: #b9bbbe; }
        input[type="password"] { width: 100%; padding: 10px; background: #202225; border: 1px solid #202225; border-radius: 3px; color: #dcddde; box-sizing: border-box; }
        input:focus { outline: none; border-color: #7289da; }
        button { width: 100%; background: #5865f2; color: white; padding: 12px; border: none; border-radius: 3px; cursor: pointer; font-size: 16px; transition: 0.2s; }
        button:hover { background: #4752c4; }
        .error { color: #f04747; font-size: 14px; margin-bottom: 15px; text-align: center; }
        .help { font-size: 12px; color: #72767d; margin-top: 15px; line-height: 1.5; }
    </style>
</head>
<body>
    <div class="login-box">
        <h2>🔐 身份验证</h2>
        {{if .}}<div class="error">{{.}}</div>{{end}}
        <form method="POST">
            <div class="input-group">
                <label>User Token</label>
                <input type="password" name="token" placeholder="请输入您的 Discord User Token" required>
            </div>
            <button type="submit">登录</button>
        </form>
        <div class="help">
            <strong>如何获取 Token?</strong><br>
            1. 在浏览器打开 Discord 网页版<br>
            2. 按 F12 打开开发者工具 -> Network (网络)<br>
            3. 刷新页面，在过滤器输入 "credentials"<br>
            4. 点击请求，在 Request Headers 中找到 "Authorization"
        </div>
    </div>
</body>
</html>
`
	t, _ := template.New("login").Parse(tpl)
	t.Execute(w, errStr)
}

func renderHome(w http.ResponseWriter, data PageData) {
	tpl := `
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<title>聊天存档索引</title>
<style>
    :root { --sidebar-bg: #202225; --sidebar-item-bg: #2F3136; --text-color: #DCDDDE; --active-border: #FFD700; --main-bg: #FFFFFF; }
    body { margin: 0; display: flex; height: 100vh; font-family: "Microsoft YaHei", sans-serif; overflow: hidden; }
    
    /* 侧边栏 */
    .sidebar { width: 280px; background: var(--sidebar-bg); display: flex; flex-direction: column; overflow: hidden; flex-shrink: 0; }
    .user-panel { padding: 15px; background: #292b2f; display: flex; align-items: center; border-bottom: 1px solid #202225; }
    .user-avatar { width: 32px; height: 32px; border-radius: 50%; margin-right: 10px; }
    .user-info { flex: 1; overflow: hidden; }
    .user-name { color: #fff; font-weight: bold; font-size: 14px; }
    .btn-logout { font-size: 12px; color: #f04747; text-decoration: none; cursor: pointer; }
    
    .nav-list { flex: 1; overflow-y: auto; padding: 10px; }
    .nav-item { display: flex; align-items: stretch; background: var(--sidebar-item-bg); margin-bottom: 10px; border-radius: 4px; cursor: pointer; transition: 0.2s; border: 1px solid transparent; text-decoration: none; }
    .nav-item:hover { background: #36393f; }
    .nav-item.active { border-left: 4px solid var(--active-border); background: #36393f; }
    .month-box { width: 60px; display: flex; align-items: center; justify-content: center; font-size: 20px; font-weight: bold; color: #FFF; border-right: 1px solid #202225; }
    .meta-box { flex: 1; padding: 10px; display: flex; flex-direction: column; justify-content: center; }
    .meta-title { color: #FFF; font-size: 14px; margin-bottom: 4px; }
    .meta-sub { color: #8E9297; font-size: 12px; }
    .meta-count { color: #8E9297; font-size: 12px; margin-top: 4px; text-align: right; }

    .content { flex: 1; background: var(--main-bg); overflow-y: auto; padding: 20px 40px; display: flex; justify-content: center; }
    .chat-container { width: 100%; max-width: 900px; }
    .refresh-bar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
    .btn-refresh { background: #5865F2; color: #fff; padding: 8px 15px; border-radius: 4px; text-decoration: none; font-size: 13px; border: none; cursor: pointer; }
    
    .msg-group { display: flex; margin-bottom: 25px; border-bottom: 1px solid #EEE; padding-bottom: 20px; padding-top: 5px; padding-left: 5px; border-radius: 4px; transition: background 0.2s;}
    .msg-group.mentioned { background-color: rgba(250, 166, 26, 0.25); border-left: 4px solid #faa61a; padding-left: 15px; }
    .msg-group.is-me .username { color: #2ecc71 !important; }
    .msg-group.is-me .avatar { border: 2px solid #2ecc71; }
    
    .avatar { width: 50px; height: 50px; border-radius: 50%; margin-right: 20px; border: 1px solid #eee; }
    .msg-body { flex: 1; min-width: 0; }
    .user-row { margin-bottom: 8px; }
    .username { color: #E91E63; font-weight: bold; font-size: 15px; margin-right: 10px; }
    .timestamp { color: #999; font-size: 12px; }
    .msg-text { font-size: 15px; line-height: 1.7; color: #2e3338; white-space: pre-wrap; margin-bottom: 10px; }
    
    .img-grid { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 10px; }
    .chat-img { max-width: 200px; max-height: 200px; border-radius: 6px; cursor: zoom-in; border: 1px solid #eee; }
    
    .reply-list { background: #F5F7FA; border-radius: 8px; padding: 10px 15px; margin-top: 10px; }
    .reply-item { margin-bottom: 8px; font-size: 13px; line-height: 1.5; color: #555; white-space: normal; }
    .reply-content { white-space: pre-wrap; }
    .reply-item.mentioned { background-color: rgba(250, 166, 26, 0.15); border-left: 3px solid #faa61a; padding: 5px; border-radius: 0 4px 4px 0;}
    .reply-user { color: #E91E63; font-weight: bold; margin-right: 5px; }
    .reply-at { color: #00AEEC; margin: 0 5px; }

    #loading { display: none; position: fixed; top:0; left:0; width:100%; height:100%; background: rgba(0,0,0,0.5); z-index: 100; align-items: center; justify-content: center; color: #fff; font-size: 18px; }
    #lightbox { display: none; position: fixed; top:0; left:0; width:100%; height:100%; background: rgba(0,0,0,0.9); z-index: 200; align-items: center; justify-content: center; }
    #lightbox img { max-width: 90%; max-height: 90%; }
</style>
</head>
<body>
<div id="loading">🔄 正在同步...</div>
<div id="lightbox" onclick="this.style.display='none'"><img id="lb-img"></div>

<div class="sidebar">
    <div class="user-panel">
        <img class="user-avatar" src="{{.CurrentUser.Avatar}}">
        <div class="user-info">
            <div class="user-name">{{.CurrentUser.Username}}</div>
            <a href="/logout" class="btn-logout">退出登录</a>
        </div>
    </div>
    <div class="nav-list">
        {{range .NavItems}}
        <a href="/?f={{.FileName}}" class="nav-item {{if .IsActive}}active{{end}}">
            <div class="month-box">{{.MonthStr}}</div>
            <div class="meta-box">
                <div class="meta-title">{{.Title}}</div>
                <div class="meta-sub">{{.SubTitle}}</div>
                <div class="meta-count">{{.Count}}</div>
            </div>
        </a>
        {{end}}
    </div>
</div>

<div class="content">
    <div class="chat-container">
        <div class="refresh-bar">
            <span style="font-size:12px;color:#999">网络: {{if .ProxyInfo}}{{.ProxyInfo}}{{else}}直连{{end}}</span>
            <button onclick="confirmRefresh('{{.ActiveFile}}')" class="btn-refresh">⚡ 抓取最新消息</button>
        </div>
        
        {{if .Messages}}
            {{range .Messages}}
            <div class="msg-group {{if .IsMention}}mentioned{{end}} {{if .IsMe}}is-me{{end}}">
                <img class="avatar" src="{{.Avatar}}">
                <div class="msg-body">
                    <div class="user-row">
                        <span class="username">{{.AuthorName}}</span>
                        <span class="timestamp">{{.Time}}</span>
                    </div>
                    <div class="msg-text">{{.Content | formatMsg}}</div>
                    {{if .Images}}
                    <div class="img-grid">{{range .Images}}<img class="chat-img" src="{{.}}" onclick="viewImg(this.src)">{{end}}</div>
                    {{end}}
                    {{if .Replies}}
                    <div class="reply-list">
                        {{range .Replies}}
                        <div class="reply-item {{if .IsMention}}mentioned{{end}}">
                            <span class="reply-user">{{.AuthorName}}</span>回复 <span class="reply-at">@{{.ReplyTarget}}</span> : 
                            <span class="reply-content">{{.Content | formatMsg}}</span>
                            {{if .Images}}<span style="color:#00AEEC;cursor:pointer" onclick="viewImg('{{index .Images 0}}')">[图片]</span>{{end}}
                            <span style="color:#999; margin-left:8px; font-size:12px;">{{.Time}}</span>
                        </div>
                        {{end}}
                    </div>
                    {{end}}
                </div>
            </div>
            {{end}}
            <div style="text-align:center; color:#999; padding:20px;">- End -</div>
        {{else}}
            <div style="text-align:center; padding:50px; color:#666;">请在左侧选择要查看的月份</div>
        {{end}}
    </div>
</div>

<script>
function viewImg(src) { document.getElementById('lb-img').src = src; document.getElementById('lightbox').style.display = 'flex'; }
function confirmRefresh(file) {
    if(confirm('抓取最新消息需要使用您的 Token 发送请求。\n\n确定继续吗？')) {
        document.getElementById('loading').style.display='flex';
        window.location.href = '/refresh?f=' + file;
    }
}
</script>
</body>
</html>
`
	funcMap := template.FuncMap{
		"index": func(arr []string, i int) string { return arr[i] },
		"formatMsg": func(content string) template.HTML {
			safe := template.HTMLEscapeString(content)
			return template.HTML(safe)
		},
	}
	t, _ := template.New("home").Funcs(funcMap).Parse(tpl)
	t.Execute(w, data)
}

func renderLimitError(w http.ResponseWriter, waitTime string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<h1>🚫 刷新次数限制</h1><p>请等待 %s 后再试。</p><a href='/'>返回</a>", waitTime)
}
