package monitor

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/config"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/notifier"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/store"
)

type cooldownState struct {
	until      time.Time
	suppressed int
}

// Engine 跨平台消息监控与冷却防刷屏引擎。
// 同时处理 Telegram、微信以及未来任意平台的消息，统一完成 @人/@组别/关键词匹配与跨通道转发。
type Engine struct {
	cfgMgr   *config.Manager
	store    *store.Store
	notifier *notifier.Dispatcher

	mu        sync.Mutex
	cooldowns map[string]*cooldownState
}

func New(cfgMgr *config.Manager, st *store.Store, disp *notifier.Dispatcher) *Engine {
	return &Engine{
		cfgMgr:    cfgMgr,
		store:     st,
		notifier:  disp,
		cooldowns: make(map[string]*cooldownState),
	}
}

// CooldownSnapshot 供前端展示当前处于冷却中的监控条目。
type CooldownSnapshot struct {
	Key        string `json:"key"`
	RemainSec  int64  `json:"remain_sec"`
	Suppressed int    `json:"suppressed"`
}

func (e *Engine) ActiveCooldowns() []CooldownSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	out := make([]CooldownSnapshot, 0)
	for k, v := range e.cooldowns {
		if now.After(v.until) {
			delete(e.cooldowns, k)
			continue
		}
		out = append(out, CooldownSnapshot{
			Key:        k,
			RemainSec:  int64(time.Until(v.until).Seconds()),
			Suppressed: v.suppressed,
		})
	}
	return out
}

// ClearCooldowns 手动重置所有冷却状态。
func (e *Engine) ClearCooldowns() {
	e.mu.Lock()
	e.cooldowns = make(map[string]*cooldownState)
	e.mu.Unlock()
}

// HandleMessage 在收到任意平台的入站消息时触发监控规则评估。
func (e *Engine) HandleMessage(ctx context.Context, ev lwplugin.Event) {
	if ev.Message == nil {
		return
	}
	msg := ev.Message
	if msg.Direction == "outbound" {
		return
	}
	platform := msg.Platform
	if platform == "" {
		platform = ev.Account.Platform
	}
	chatID := msg.Conversation()
	chatTitle := extractChatTitle(msg)
	senderID, senderName := extractSender(msg)
	senderIsBot := extractSenderIsBot(msg)
	mentions := extractMentions(msg)
	text := strings.TrimSpace(msg.Text)
	wxMeta := parseWechatRawMeta(msg, platform)
	msgTypes := detectMessageTypes(msg, wxMeta)
	msgTypeLabel := formatMessageTypesLabel(msgTypes)
	contentSummary := buildContentSummary(msg, msgTypes, wxMeta)
	media := extractMediaPayload(msg, platform, ev.Account.ID, chatID, wxMeta)

	e.store.TouchChat(platform, ev.Account.ID, chatID, chatTitle)

	cfg := e.cfgMgr.Get()
	groupByID := make(map[string]config.MentionGroup, len(cfg.MentionGroups))
	for _, g := range cfg.MentionGroups {
		groupByID[g.ID] = g
	}

	for _, rule := range cfg.MonitorRules {
		if !rule.Enabled || len(rule.Targets) == 0 {
			continue
		}
		if !matchList(rule.Platforms, platform) {
			continue
		}
		if !matchList(rule.AccountIDs, ev.Account.ID) {
			continue
		}
		if !matchList(rule.ChatIDs, chatID) {
			continue
		}
		if containsCI(rule.IgnoreSenders, senderID) || containsCI(rule.IgnoreSenders, senderName) {
			continue
		}
		if shouldExcludeMessage(rule, text, contentSummary, senderIsBot) {
			continue
		}
		if !matchesMessageTypes(rule.MessageTypes, msgTypes) {
			continue
		}

		matchedTargets := evaluateRule(rule, groupByID, mentions, text)
		if len(matchedTargets) == 0 {
			continue
		}

		for _, targetLabel := range matchedTargets {
			ck := buildCooldownKey(rule, chatID, targetLabel)
			if suppressed, ok := e.checkCooldown(ck, rule.CooldownSeconds); !ok {
				e.store.IncAlertSuppressed()
				e.store.AddLog(store.LogEntry{
					Category:  "cooldown",
					Level:     "info",
					Platform:  platform,
					AccountID: ev.Account.ID,
					ChatID:    chatID,
					ChatTitle: chatTitle,
					RuleName:  rule.Name,
					Summary:   fmt.Sprintf("冷却抑制 [%s]（窗口内已拦截 %d 次）", targetLabel, suppressed),
					Detail:    contentSummary,
				})
				continue
			}

			var body string
			var fallbackText string
			if rule.RawForward {
				if media != nil && rule.ShouldForwardMedia() {
					// 纯原样转发媒体：无配文时不发多余文本，仅在媒体发送失败或超限时使用 fallbackText 兜底
					body = cleanRawCaption(msg)
					fallbackText = contentSummary
				} else {
					body = contentSummary
				}
			} else {
				body = renderTemplate(rule.Template, map[string]string{
					"{platform}": platformLabel(platform),
					"{account}":  ev.Account.ID,
					"{group}":    firstNonEmpty(chatTitle, chatID),
					"{chat_id}":  chatID,
					"{sender}":   firstNonEmpty(senderName, senderID),
					"{target}":   targetLabel,
					"{msg_type}": msgTypeLabel,
					"{content}":  contentSummary,
					"{time}":     time.Now().Format("2006-01-02 15:04:05"),
					"{rule}":     rule.Name,
				})
				fallbackText = body
			}

			mediaOpts := notifier.MediaOptions{
				ForwardMedia: rule.ShouldForwardMedia(),
				MaxBytes:     int64(rule.EffectiveMaxMediaMB()) * 1024 * 1024,
				FallbackText: fallbackText,
			}
			perTargetTimeout := 20 * time.Second
			if media != nil && mediaOpts.ForwardMedia {
				perTargetTimeout = 60 * time.Second
			}

			var errs []string
			for _, tgt := range rule.Targets {
				sendCtx, cancel := context.WithTimeout(ctx, perTargetTimeout)
				if err := e.notifier.SendWithMediaOptions(sendCtx, tgt, body, media, mediaOpts); err != nil {
					errs = append(errs, fmt.Sprintf("%s(%s): %v", tgt.Type, tgt.To, err))
				}
				cancel()
			}
			if len(errs) == 0 {
				e.store.IncAlertSent()
				e.store.AddLog(store.LogEntry{
					Category:  "monitor",
					Level:     "info",
					Platform:  platform,
					AccountID: ev.Account.ID,
					ChatID:    chatID,
					ChatTitle: chatTitle,
					RuleName:  rule.Name,
					Summary:   fmt.Sprintf("命中 [%s]（%s）→ 已转发至 %d 个目标", targetLabel, msgTypeLabel, len(rule.Targets)),
					Detail:    contentSummary,
				})
			} else {
				e.store.AddLog(store.LogEntry{
					Category:  "monitor",
					Level:     "error",
					Platform:  platform,
					AccountID: ev.Account.ID,
					ChatID:    chatID,
					ChatTitle: chatTitle,
					RuleName:  rule.Name,
					Summary:   fmt.Sprintf("命中 [%s] 转发部分失败", targetLabel),
					Detail:    strings.Join(errs, " | "),
				})
			}
		}
	}
}

func (e *Engine) checkCooldown(key string, cooldownSec int) (int, bool) {
	if cooldownSec <= 0 {
		return 0, true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	if st, ok := e.cooldowns[key]; ok && now.Before(st.until) {
		st.suppressed++
		return st.suppressed, false
	}
	e.cooldowns[key] = &cooldownState{
		until:      now.Add(time.Duration(cooldownSec) * time.Second),
		suppressed: 0,
	}
	return 0, true
}

func buildCooldownKey(rule config.MonitorRule, chatID, targetLabel string) string {
	switch rule.CooldownScope {
	case "rule":
		return rule.ID
	case "chat":
		return rule.ID + "|" + chatID
	default:
		return rule.ID + "|" + chatID + "|" + targetLabel
	}
}

func evaluateRule(
	rule config.MonitorRule,
	groups map[string]config.MentionGroup,
	mentions []string,
	text string,
) []string {
	if rule.MatchAllMessages {
		return []string{"全部消息"}
	}

	var hits []string
	seen := make(map[string]struct{})
	addHit := func(label string) {
		if _, ok := seen[label]; !ok {
			seen[label] = struct{}{}
			hits = append(hits, label)
		}
	}

	lowerText := strings.ToLower(text)
	isAllMention := strings.Contains(lowerText, "@all") ||
		strings.Contains(text, "@所有人") ||
		strings.Contains(text, "@全体成员")

	if rule.WatchAllMention && isAllMention {
		addHit("@全体成员")
	}

	// 1. 直接监控的个人 @
	for _, watched := range rule.WatchMentions {
		w := strings.TrimSpace(watched)
		if w == "" {
			continue
		}
		if matchMentionTarget(w, mentions, lowerText) {
			addHit("@" + strings.TrimPrefix(w, "@"))
		}
	}

	// 2. 监控的人员分组（组内任意成员被 @ 时触发）
	for _, gid := range rule.WatchGroupIDs {
		g, ok := groups[gid]
		if !ok {
			continue
		}
		for _, member := range g.Members {
			if matchMentionTarget(member, mentions, lowerText) {
				addHit("组:" + g.Name)
				break
			}
		}
	}

	// 3. 关键词监控
	for _, kw := range rule.Keywords {
		kw = strings.TrimSpace(kw)
		if kw != "" && strings.Contains(lowerText, strings.ToLower(kw)) {
			addHit("关键词:" + kw)
		}
	}

	return hits
}

func matchMentionTarget(target string, mentions []string, lowerText string) bool {
	norm := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(target), "@"))
	if norm == "" {
		return false
	}
	for _, m := range mentions {
		if strings.ToLower(strings.TrimPrefix(strings.TrimSpace(m), "@")) == norm {
			return true
		}
	}
	return strings.Contains(lowerText, "@"+norm)
}

func extractMentions(msg *lwplugin.Message) []string {
	var out []string
	for _, seg := range msg.Segments {
		if seg.Type != "at" {
			continue
		}
		if m, ok := seg.Data.(map[string]any); ok {
			if id, ok := m["target_id"].(string); ok && strings.TrimSpace(id) != "" {
				out = append(out, strings.TrimSpace(id))
			}
		}
	}
	// 同时补充 Telegram raw.tg.entities 中的 text_mention user_id / username
	if len(msg.Raw) > 0 {
		var raw struct {
			TG struct {
				Entities []struct {
					UserID   int64  `json:"user_id"`
					Username string `json:"username"`
				} `json:"entities"`
			} `json:"tg"`
		}
		if json.Unmarshal(msg.Raw, &raw) == nil {
			for _, ent := range raw.TG.Entities {
				if ent.Username != "" {
					out = append(out, "@"+ent.Username)
				}
				if ent.UserID > 0 {
					out = append(out, fmt.Sprintf("%d", ent.UserID))
				}
			}
		}
	}
	return out
}

func extractChatTitle(msg *lwplugin.Message) string {
	if msg == nil || len(msg.Raw) == 0 {
		return ""
	}
	var raw map[string]any
	if json.Unmarshal(msg.Raw, &raw) != nil || raw == nil {
		return ""
	}
	// 1. Telegram 专用字段
	if tg, ok := raw["tg"].(map[string]any); ok {
		if t := stringFromMap(tg, "chat_title", "title"); t != "" {
			return t
		}
	}
	// 2. 通用外部平台驱动常见字段（QQ / 飞书 / 钉钉 / Discord 等）
	if t := stringFromMap(raw, "chat_title", "group_name", "room_name", "channel_name", "chat_name", "title"); t != "" {
		return t
	}
	for _, nestedKey := range []string{"chat", "group", "channel", "room"} {
		if sub, ok := raw[nestedKey].(map[string]any); ok {
			if t := stringFromMap(sub, "title", "name", "group_name", "chat_title"); t != "" {
				return t
			}
		}
	}
	return ""
}

func extractSender(msg *lwplugin.Message) (string, string) {
	if msg == nil {
		return "", ""
	}
	id := msg.From.ID
	name := msg.From.ID
	if len(msg.Raw) == 0 {
		return id, name
	}
	var raw map[string]any
	if json.Unmarshal(msg.Raw, &raw) != nil || raw == nil {
		return id, name
	}
	// 1. Telegram 专用字段
	if tg, ok := raw["tg"].(map[string]any); ok {
		if uid := int64FromMap(tg, "user_id"); uid > 0 {
			id = fmt.Sprintf("%d", uid)
		}
		if uname := stringFromMap(tg, "username"); uname != "" {
			name = "@" + strings.TrimPrefix(uname, "@")
			return id, name
		}
		if dname := stringFromMap(tg, "display_name"); dname != "" {
			return id, dname
		}
	}
	// 2. 通用外部平台驱动字段（QQ / 飞书 / 钉钉 / Discord 等）
	if n := stringFromMap(raw, "sender_name", "sender_nickname", "nickname", "display_name", "user_name", "username"); n != "" {
		return id, n
	}
	for _, nestedKey := range []string{"sender", "user", "author", "from"} {
		if sub, ok := raw[nestedKey].(map[string]any); ok {
			if id == "" {
				if subID := stringFromMap(sub, "id", "user_id", "open_id", "uin"); subID != "" {
					id = subID
				} else if numID := int64FromMap(sub, "id", "user_id", "uin"); numID > 0 {
					id = fmt.Sprintf("%d", numID)
				}
			}
			// 优先群名片 card / 昵称 nickname / display_name / name / username
			if n := stringFromMap(sub, "card", "nickname", "display_name", "name", "global_name", "username"); n != "" {
				return id, n
			}
		}
	}
	return id, name
}

func extractSenderIsBot(msg *lwplugin.Message) bool {
	if msg == nil || len(msg.Raw) == 0 {
		return false
	}
	var raw map[string]any
	if json.Unmarshal(msg.Raw, &raw) != nil || raw == nil {
		return false
	}
	if tg, ok := raw["tg"].(map[string]any); ok {
		if boolFromMap(tg, "is_bot") {
			return true
		}
	}
	if boolFromMap(raw, "is_bot", "sender_is_bot", "from_bot") {
		return true
	}
	for _, nestedKey := range []string{"sender", "user", "author", "from"} {
		if sub, ok := raw[nestedKey].(map[string]any); ok {
			if boolFromMap(sub, "is_bot", "bot") {
				return true
			}
		}
	}
	return false
}

func boolFromMap(m map[string]any, keys ...string) bool {
	if m == nil {
		return false
	}
	for _, k := range keys {
		if b, ok := m[k].(bool); ok && b {
			return true
		}
	}
	return false
}

// shouldExcludeMessage 判断消息是否命中规则的排除过滤条件（命令 / 机器人 / 排除文本）。
func shouldExcludeMessage(rule config.MonitorRule, text, contentSummary string, senderIsBot bool) bool {
	if rule.IgnoreBots && senderIsBot {
		return true
	}
	trimmed := strings.TrimSpace(text)
	if rule.IgnoreCommands && strings.HasPrefix(trimmed, "/") {
		return true
	}
	if len(rule.ExcludeKeywords) == 0 {
		return false
	}
	lowerText := strings.ToLower(trimmed)
	lowerSummary := strings.ToLower(strings.TrimSpace(contentSummary))
	for _, rawKw := range rule.ExcludeKeywords {
		kw := strings.TrimSpace(rawKw)
		if kw == "" {
			continue
		}
		if strings.HasPrefix(kw, "^") {
			prefix := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(kw, "^")))
			if prefix != "" && (strings.HasPrefix(lowerText, prefix) || strings.HasPrefix(lowerSummary, prefix)) {
				return true
			}
			continue
		}
		lk := strings.ToLower(kw)
		if (lowerText != "" && strings.Contains(lowerText, lk)) || (lowerSummary != "" && strings.Contains(lowerSummary, lk)) {
			return true
		}
	}
	return false
}

func renderTemplate(tpl string, vars map[string]string) string {
	if strings.TrimSpace(tpl) == "" {
		tpl = "🔔 群消息转发通知\n" +
			"规则：{rule}\n" +
			"来源：[{platform}] {group}\n" +
			"触发：{target} · 类型：{msg_type}\n" +
			"发送者：{sender}\n" +
			"内容：{content}\n" +
			"时间：{time}"
	}
	out := tpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}

func platformLabel(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "wechat":
		return "微信"
	case "telegram":
		return "Telegram"
	case "qq":
		return "QQ"
	case "feishu":
		return "飞书"
	case "dingtalk":
		return "钉钉"
	case "discord":
		return "Discord"
	case "slack":
		return "Slack"
	default:
		return p
	}
}

var msgTypeNames = map[string]string{
	"text":  "文字",
	"image": "图片",
	"video": "视频",
	"voice": "语音",
	"file":  "文件",
	"link":  "链接",
	"other": "其他",
}

type wechatRawMeta struct {
	MsgType int
	NewMsgID string
	Content string // 原始 XML 或正文

	// 图片
	ImgAesKey   string
	ImgMidURL   string
	ImgBigURL   string
	ImgMD5      string
	ImgLength   int64
	ImgHDLength int64

	// 视频
	VideoAesKey  string
	VideoURL     string
	VideoMD5     string
	VideoLength  int64
	VideoPlaySec int

	// 语音
	VoiceLen       int64
	VoiceLengthMS  int
	VoiceFormat    int
	VoiceBufID     string
	VoiceClientID  string

	// 表情 (msg_type == 47)
	EmojiMD5    string
	EmojiLen    int64
	EmojiCDNURL string

	// AppMsg (msg_type == 49)
	AppType       int // 6=文件, 5=链接, 57=引用消息
	AppTitle      string
	AppURL        string
	AppAttachURL  string
	AppAesKey     string
	AppTotalLen   int64
	AppMD5        string
	AppFileExt    string
}

func parseWechatRawMeta(msg *lwplugin.Message, platform string) *wechatRawMeta {
	if strings.ToLower(strings.TrimSpace(platform)) != "wechat" || len(msg.Raw) == 0 {
		return nil
	}
	var raw struct {
		MsgType  int             `json:"msg_type"`
		NewMsgID json.RawMessage `json:"new_msg_id"`
		Content  string          `json:"content"`
	}
	if json.Unmarshal(msg.Raw, &raw) != nil {
		return nil
	}
	meta := &wechatRawMeta{
		MsgType:  raw.MsgType,
		NewMsgID: strings.Trim(strings.TrimSpace(string(raw.NewMsgID)), `"`),
		Content:  strings.TrimSpace(raw.Content),
	}
	if meta.Content == "" || !strings.HasPrefix(meta.Content, "<") {
		return meta
	}

	switch meta.MsgType {
	case 3: // 图片
		var doc struct {
			Img struct {
				AesKey   string `xml:"aeskey,attr"`
				MidURL   string `xml:"cdnmidimgurl,attr"`
				BigURL   string `xml:"cdnbigimgurl,attr"`
				MD5      string `xml:"md5,attr"`
				Length   string `xml:"length,attr"`
				HDLength string `xml:"hdlength,attr"`
			} `xml:"img"`
		}
		if xml.Unmarshal([]byte(meta.Content), &doc) == nil {
			meta.ImgAesKey = strings.TrimSpace(doc.Img.AesKey)
			meta.ImgMidURL = strings.TrimSpace(doc.Img.MidURL)
			meta.ImgBigURL = strings.TrimSpace(doc.Img.BigURL)
			meta.ImgMD5 = strings.TrimSpace(doc.Img.MD5)
			meta.ImgLength = parseInt64(doc.Img.Length)
			meta.ImgHDLength = parseInt64(doc.Img.HDLength)
		}
	case 43: // 视频
		var doc struct {
			Video struct {
				AesKey     string `xml:"aeskey,attr"`
				VideoURL   string `xml:"cdnvideourl,attr"`
				MD5        string `xml:"md5,attr"`
				Length     string `xml:"length,attr"`
				PlayLength string `xml:"playlength,attr"`
			} `xml:"videomsg"`
		}
		if xml.Unmarshal([]byte(meta.Content), &doc) == nil {
			meta.VideoAesKey = strings.TrimSpace(doc.Video.AesKey)
			meta.VideoURL = strings.TrimSpace(doc.Video.VideoURL)
			meta.VideoMD5 = strings.TrimSpace(doc.Video.MD5)
			meta.VideoLength = parseInt64(doc.Video.Length)
			meta.VideoPlaySec = int(parseInt64(doc.Video.PlayLength))
		}
	case 34: // 语音
		var doc struct {
			Voice struct {
				Length      string `xml:"length,attr"`
				VoiceLength string `xml:"voicelength,attr"`
				VoiceFormat string `xml:"voiceformat,attr"`
				BufID       string `xml:"bufid,attr"`
				ClientMsgID string `xml:"clientmsgid,attr"`
			} `xml:"voicemsg"`
		}
		if xml.Unmarshal([]byte(meta.Content), &doc) == nil {
			meta.VoiceLen = parseInt64(doc.Voice.Length)
			meta.VoiceLengthMS = int(parseInt64(doc.Voice.VoiceLength))
			meta.VoiceFormat = int(parseInt64(doc.Voice.VoiceFormat))
			meta.VoiceBufID = strings.TrimSpace(doc.Voice.BufID)
			meta.VoiceClientID = strings.TrimSpace(doc.Voice.ClientMsgID)
		}
	case 47: // 微信自定义表情
		var doc struct {
			Emoji struct {
				MD5    string `xml:"md5,attr"`
				Len    string `xml:"len,attr"`
				CDNURL string `xml:"cdnurl,attr"`
			} `xml:"emoji"`
		}
		if xml.Unmarshal([]byte(meta.Content), &doc) == nil {
			meta.EmojiMD5 = strings.TrimSpace(doc.Emoji.MD5)
			meta.EmojiLen = parseInt64(doc.Emoji.Len)
			meta.EmojiCDNURL = strings.TrimSpace(doc.Emoji.CDNURL)
		}
	case 49: // AppMsg：文件 / 链接 / 引用回复等
		var doc struct {
			AppMsg struct {
				Title     string `xml:"title"`
				Type      string `xml:"type"`
				URL       string `xml:"url"`
				MD5       string `xml:"md5"`
				AppAttach struct {
					TotalLen     string `xml:"totallen"`
					CdnAttachURL string `xml:"cdnattachurl"`
					AesKey       string `xml:"aeskey"`
					FileExt      string `xml:"fileext"`
				} `xml:"appattach"`
			} `xml:"appmsg"`
		}
		if xml.Unmarshal([]byte(meta.Content), &doc) == nil {
			meta.AppType = int(parseInt64(doc.AppMsg.Type))
			meta.AppTitle = strings.TrimSpace(doc.AppMsg.Title)
			meta.AppURL = strings.TrimSpace(doc.AppMsg.URL)
			meta.AppMD5 = strings.TrimSpace(doc.AppMsg.MD5)
			meta.AppTotalLen = parseInt64(doc.AppMsg.AppAttach.TotalLen)
			meta.AppAttachURL = strings.TrimSpace(doc.AppMsg.AppAttach.CdnAttachURL)
			meta.AppAesKey = strings.TrimSpace(doc.AppMsg.AppAttach.AesKey)
			meta.AppFileExt = strings.TrimSpace(doc.AppMsg.AppAttach.FileExt)
		}
	}
	return meta
}

func parseInt64(s string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return v
}

// detectMessageTypes 识别消息包含的标准类型列表（去重且保序）。
func detectMessageTypes(msg *lwplugin.Message, wxMeta *wechatRawMeta) []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}

	// 微信 msg_type=49 优先按 AppMsg 子类型精确分类（文件 type=6，引用回复 type=57）
	if wxMeta != nil && wxMeta.MsgType == 49 {
		switch wxMeta.AppType {
		case 6:
			add("file")
			return out
		case 57:
			add("text")
			return out
		case 5:
			add("link")
			return out
		}
	}

	for _, seg := range msg.Segments {
		switch strings.ToLower(strings.TrimSpace(seg.Type)) {
		case "text", "at":
			if strings.TrimSpace(msg.Text) != "" || seg.Type == "at" {
				add("text")
			}
		case "image":
			add("image")
		case "video":
			add("video")
		case "voice":
			add("voice")
		case "file":
			m, _ := seg.Data.(map[string]any)
			if isTelegramMP4Document(msg.Platform, m) {
				add("video")
			} else {
				add("file")
			}
		case "link":
			add("link")
		case "location", "contact", "quote", "custom":
			add("other")
		default:
			if seg.Type != "" {
				add("other")
			}
		}
	}
	if len(out) == 0 {
		if strings.TrimSpace(msg.Text) != "" {
			add("text")
		} else {
			add("other")
		}
	}
	return out
}

// matchesMessageTypes 判断消息是否满足规则允许的类型（规则留空表示全部放行）。
func matchesMessageTypes(allowed, actual []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range actual {
		if containsCI(allowed, a) {
			return true
		}
	}
	return false
}

func formatMessageTypesLabel(types []string) string {
	if len(types) == 0 {
		return "文字"
	}
	labels := make([]string, 0, len(types))
	for _, t := range types {
		if name, ok := msgTypeNames[t]; ok {
			labels = append(labels, name)
		} else {
			labels = append(labels, t)
		}
	}
	return strings.Join(labels, "+")
}

// buildContentSummary 为文字及非文字消息生成可读的转发摘要，避免图片/文件等消息出现空内容或原始 XML 泄漏。
func buildContentSummary(msg *lwplugin.Message, types []string, wxMeta *wechatRawMeta) string {
	if wxMeta != nil && wxMeta.MsgType == 49 {
		switch wxMeta.AppType {
		case 6:
			name := firstNonEmpty(wxMeta.AppTitle, "未命名文件")
			if wxMeta.AppTotalLen > 0 {
				return fmt.Sprintf("[文件: %s (%s)]", name, formatHumanBytes(wxMeta.AppTotalLen))
			}
			return fmt.Sprintf("[文件: %s]", name)
		case 57:
			if wxMeta.AppTitle != "" {
				return wxMeta.AppTitle
			}
		case 5:
			title := firstNonEmpty(wxMeta.AppTitle, wxMeta.AppURL)
			if title != "" {
				return fmt.Sprintf("[链接: %s]", title)
			}
			return "[链接]"
		}
	}

	var tags []string
	hasTextSeg := false
	for _, seg := range msg.Segments {
		m, _ := seg.Data.(map[string]any)
		switch strings.ToLower(strings.TrimSpace(seg.Type)) {
		case "text", "at":
			hasTextSeg = true
		case "image":
			tags = append(tags, "[图片]")
		case "video":
			if wxMeta != nil && wxMeta.VideoLength > 0 {
				if wxMeta.VideoPlaySec > 0 {
					tags = append(tags, fmt.Sprintf("[视频 %ds, %s]", wxMeta.VideoPlaySec, formatHumanBytes(wxMeta.VideoLength)))
				} else {
					tags = append(tags, fmt.Sprintf("[视频 %s]", formatHumanBytes(wxMeta.VideoLength)))
				}
			} else {
				tags = append(tags, "[视频]")
			}
		case "voice":
			if wxMeta != nil && wxMeta.VoiceLengthMS > 0 {
				sec := (wxMeta.VoiceLengthMS + 500) / 1000
				if sec < 1 {
					sec = 1
				}
				tags = append(tags, fmt.Sprintf("[语音 %ds]", sec))
			} else {
				tags = append(tags, "[语音]")
			}
		case "file":
			name := stringFromMap(m, "name", "fileName", "filename")
			size := int64FromMap(m, "size", "file_size")
			label := "文件"
			if isTelegramMP4Document(msg.Platform, m) {
				label = "视频"
			}
			if name != "" && size > 0 {
				tags = append(tags, fmt.Sprintf("[%s: %s (%s)]", label, name, formatHumanBytes(size)))
			} else if name != "" {
				tags = append(tags, fmt.Sprintf("[%s: %s]", label, name))
			} else {
				tags = append(tags, fmt.Sprintf("[%s]", label))
			}
		case "link":
			title := stringFromMap(m, "title", "url")
			if strings.HasPrefix(title, "<") {
				title = ""
			}
			if title != "" {
				tags = append(tags, fmt.Sprintf("[链接: %s]", title))
			} else {
				tags = append(tags, "[链接]")
			}
		case "location":
			label := stringFromMap(m, "label", "title", "address")
			if label != "" {
				tags = append(tags, fmt.Sprintf("[位置: %s]", label))
			} else {
				tags = append(tags, "[位置]")
			}
		case "contact":
			name := stringFromMap(m, "name", "nickname")
			if name != "" {
				tags = append(tags, fmt.Sprintf("[名片: %s]", name))
			} else {
				tags = append(tags, "[名片]")
			}
		case "custom":
			kind := strings.ToLower(strings.TrimSpace(stringFromMap(m, "custom_type", "type")))
			extra, _ := m["extra"].(map[string]any)
			switch kind {
			case "sticker", "emoji", "face", "mface", "market_face":
				emoji := stringFromMap(extra, "emoji", "summary", "text")
				label := "表情"
				if kind == "sticker" {
					label = "贴纸"
				}
				if emoji != "" {
					tags = append(tags, fmt.Sprintf("[%s %s]", label, emoji))
				} else {
					tags = append(tags, fmt.Sprintf("[%s]", label))
				}
			case "forward", "multiforward", "multi_forward":
				tags = append(tags, "[合并转发]")
			case "json", "ark", "card", "miniapp", "app":
				if title := stringFromMap(extra, "title", "prompt"); title != "" {
					tags = append(tags, fmt.Sprintf("[卡片: %s]", title))
				} else {
					tags = append(tags, "[卡片]")
				}
			case "poke", "nudge":
				tags = append(tags, "[戳一戳]")
			case "red_packet", "hongbao", "wallet":
				tags = append(tags, "[红包]")
			case "music":
				tags = append(tags, "[音乐]")
			default:
				if kind != "" {
					tags = append(tags, fmt.Sprintf("[%s]", kind))
				} else {
					tags = append(tags, "[表情/自定义消息]")
				}
			}
		}
	}
	text := cleanRawCaption(msg)
	if len(tags) == 0 {
		if text != "" {
			return text
		}
		return "[" + formatMessageTypesLabel(types) + "]"
	}
	prefix := strings.Join(tags, " ")
	if text == "" || text == prefix || (!hasTextSeg && isAutoMediaPlaceholder(text)) {
		return prefix
	}
	return prefix + " " + text
}

// extractMediaPayload 提取可原样转发的媒体载荷（支持图片、视频、文件、语音，以及微信 XML 直转与跨平台下载参数）。
func extractMediaPayload(msg *lwplugin.Message, platform, accountID, chatID string, wxMeta *wechatRawMeta) *notifier.MediaPayload {
	plat := strings.ToLower(strings.TrimSpace(platform))
	isGroup := strings.EqualFold(msg.ChatType, "group") || strings.HasSuffix(chatID, "@chatroom")

	// 1. 微信原生媒体（支持同平台 XML 直转 + 跨平台按需下载）
	if plat == "wechat" && wxMeta != nil {
		switch wxMeta.MsgType {
		case 3: // 图片
			size := wxMeta.ImgHDLength
			if size <= 0 {
				size = wxMeta.ImgLength
			}
			if wxMeta.Content != "" || wxMeta.ImgMidURL != "" || wxMeta.ImgBigURL != "" {
				return &notifier.MediaPayload{
					Kind:            "image",
					SourcePlatform:  "wechat",
					SourceAccountID: accountID,
					SourceChatID:    chatID,
					IsGroup:         isGroup,
					Filename:        "image.jpg",
					MimeType:        "image/jpeg",
					FileSize:        size,
					WechatXML:       wxMeta.Content,
					WxFileID:        wxMeta.ImgMidURL,
					WxBigFileID:     wxMeta.ImgBigURL,
					WxAesKey:        wxMeta.ImgAesKey,
					WxMD5:           wxMeta.ImgMD5,
					WxRawSize:       size,
				}
			}
		case 43: // 视频
			if wxMeta.Content != "" || wxMeta.VideoURL != "" {
				return &notifier.MediaPayload{
					Kind:            "video",
					SourcePlatform:  "wechat",
					SourceAccountID: accountID,
					SourceChatID:    chatID,
					IsGroup:         isGroup,
					Filename:        "video.mp4",
					MimeType:        "video/mp4",
					FileSize:        wxMeta.VideoLength,
					WechatXML:       wxMeta.Content,
					WxFileID:        wxMeta.VideoURL,
					WxAesKey:        wxMeta.VideoAesKey,
					WxMD5:           wxMeta.VideoMD5,
					WxRawSize:       wxMeta.VideoLength,
				}
			}
		case 49: // AppMsg 文件 (type=6)
			if wxMeta.AppType == 6 && (wxMeta.Content != "" || wxMeta.AppAttachURL != "") {
				name := firstNonEmpty(wxMeta.AppTitle, "file")
				if wxMeta.AppFileExt != "" && !strings.Contains(name, ".") {
					name += "." + strings.TrimPrefix(wxMeta.AppFileExt, ".")
				}
				return &notifier.MediaPayload{
					Kind:            "file",
					SourcePlatform:  "wechat",
					SourceAccountID: accountID,
					SourceChatID:    chatID,
					IsGroup:         isGroup,
					Filename:        name,
					FileSize:        wxMeta.AppTotalLen,
					WechatXML:       wxMeta.Content,
					WxFileID:        wxMeta.AppAttachURL,
					WxAesKey:        wxMeta.AppAesKey,
					WxMD5:           wxMeta.AppMD5,
					WxRawSize:       wxMeta.AppTotalLen,
				}
			}
		case 34: // 语音
			if wxMeta.NewMsgID != "" {
				return &notifier.MediaPayload{
					Kind:            "voice",
					SourcePlatform:  "wechat",
					SourceAccountID: accountID,
					SourceChatID:    chatID,
					IsGroup:         isGroup,
					Filename:        "voice.silk",
					MimeType:        "audio/silk",
					FileSize:        wxMeta.VoiceLen,
					WechatXML:       wxMeta.Content,
					WxNewMsgID:      wxMeta.NewMsgID,
					WxVoiceBufID:    wxMeta.VoiceBufID,
					WxVoiceClientID: wxMeta.VoiceClientID,
					WxVoiceLen:      wxMeta.VoiceLen,
					WxVoiceFormat:   wxMeta.VoiceFormat,
					WxVoiceTimeMS:   wxMeta.VoiceLengthMS,
				}
			}
		case 47: // 微信自定义表情（同平台凭 MD5+len 免上传直转，跨平台可通过 cdnurl 下载）
			if (wxMeta.EmojiMD5 != "" && wxMeta.EmojiLen > 0) || wxMeta.EmojiCDNURL != "" {
				return &notifier.MediaPayload{
					Kind:            "sticker",
					SourcePlatform:  "wechat",
					SourceAccountID: accountID,
					SourceChatID:    chatID,
					IsGroup:         isGroup,
					URL:             wxMeta.EmojiCDNURL,
					Filename:        "emoji.gif",
					FileSize:        wxMeta.EmojiLen,
					WechatXML:       wxMeta.Content,
					WxEmojiMD5:      wxMeta.EmojiMD5,
					WxEmojiLen:      wxMeta.EmojiLen,
				}
			}
		}
	}

	// 2. Telegram / QQ / 飞书等通用消息段（图片 / 视频 / 文件 / 语音 / 贴纸表情）
	for _, seg := range msg.Segments {
		kind := strings.ToLower(strings.TrimSpace(seg.Type))
		m, ok := seg.Data.(map[string]any)
		if !ok {
			continue
		}
		if kind == "custom" {
			ctype := strings.ToLower(strings.TrimSpace(stringFromMap(m, "custom_type", "type")))
			if ctype != "sticker" && ctype != "emoji" && ctype != "mface" && ctype != "market_face" {
				continue
			}
			extra, _ := m["extra"].(map[string]any)
			fileID := firstNonEmpty(stringFromMap(extra, "file_id", "fileId"), stringFromMap(m, "file_id", "fileId"))
			mediaURL := firstNonEmpty(stringFromMap(extra, "url", "image_url", "cdnurl"), stringFromMap(m, "url"))
			b64 := firstNonEmpty(stringFromMap(extra, "base64"), stringFromMap(m, "base64"))
			if fileID == "" && mediaURL == "" && b64 == "" {
				continue
			}
			p := &notifier.MediaPayload{
				Kind:            "sticker",
				SourcePlatform:  plat,
				SourceAccountID: accountID,
				SourceChatID:    chatID,
				IsGroup:         isGroup,
				Base64:          b64,
				URL:             mediaURL,
				Emoji:           stringFromMap(extra, "emoji", "summary"),
				Filename:        stringFromMap(extra, "filename", "fileName", "name"),
				MimeType:        stringFromMap(extra, "mime_type", "mimeType"),
			}
			if plat == "telegram" {
				p.TGFileID = fileID
			}
			return p
		}
		switch kind {
		case "image", "video", "file", "voice":
		default:
			continue
		}
		payload := &notifier.MediaPayload{
			Kind:            kind,
			SourcePlatform:  plat,
			SourceAccountID: accountID,
			SourceChatID:    chatID,
			IsGroup:         isGroup,
			Filename:        stringFromMap(m, "filename", "fileName", "name"),
			MimeType:        stringFromMap(m, "mime_type", "mimeType"),
			FileSize:        int64FromMap(m, "size", "file_size", "fileSize"),
		}
		// Telegram 无声 GIF / 动图会被 Bot API 放入 document 字段（.mp4），标记 IsAnimation 以便发微信时优先转成动图表情
		if plat == "telegram" && kind == "file" && isMP4File(payload.Filename, payload.MimeType) {
			payload.Kind = "video"
			payload.IsAnimation = true
			if payload.MimeType == "" {
				payload.MimeType = "video/mp4"
			}
		}
		if src, ok := m["source"].(map[string]any); ok {
			payload.Base64 = stringFromMap(src, "base64")
			payload.URL = stringFromMap(src, "url")
			if plat == "telegram" {
				payload.TGFileID = stringFromMap(src, "file_id", "fileId")
			}
		}
		if payload.Base64 == "" {
			payload.Base64 = stringFromMap(m, "base64")
		}
		if payload.URL == "" {
			payload.URL = stringFromMap(m, "url")
		}
		if plat == "telegram" && payload.TGFileID == "" {
			payload.TGFileID = stringFromMap(m, "file_id", "fileId", "id")
		}
		if payload.Base64 != "" || payload.URL != "" || payload.TGFileID != "" {
			return payload
		}
	}
	return nil
}

func int64FromMap(m map[string]any, keys ...string) int64 {
	if m == nil {
		return 0
	}
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			if v > 0 {
				return int64(v)
			}
		case int64:
			if v > 0 {
				return v
			}
		case int:
			if v > 0 {
				return int64(v)
			}
		case json.Number:
			if n, err := v.Int64(); err == nil && n > 0 {
				return n
			}
		case string:
			if n := parseInt64(v); n > 0 {
				return n
			}
		}
	}
	return 0
}

func cleanRawCaption(msg *lwplugin.Message) string {
	if msg == nil {
		return ""
	}
	// 优先从显式 text 段提取原始正文：
	// Telegram 消息的 msg.Text 由宿主拼接所有段生成，会把附带的 At 段（如 "@snakehyc"）
	// 再次拼在末尾变成 "@@snakehyc"；直接读取 text 段即可得到干净原文。
	var textParts []string
	hasTextSeg := false
	for _, seg := range msg.Segments {
		st := strings.ToLower(strings.TrimSpace(seg.Type))
		if st == "text" {
			hasTextSeg = true
			if m, ok := seg.Data.(map[string]any); ok {
				if s := strings.TrimSpace(stringFromMap(m, "text")); s != "" {
					textParts = append(textParts, s)
				}
			}
		}
	}
	if len(textParts) > 0 {
		joined := strings.TrimSpace(strings.Join(textParts, "\n"))
		if strings.HasPrefix(joined, "<msg>") || strings.HasPrefix(joined, "<?xml") {
			return ""
		}
		return joined
	}

	t := strings.TrimSpace(msg.Text)
	if t == "" || strings.HasPrefix(t, "<msg>") || strings.HasPrefix(t, "<?xml") {
		return ""
	}
	if !hasTextSeg && isAutoMediaPlaceholder(t) {
		return ""
	}
	return t
}

func isTelegramMP4Document(platform string, m map[string]any) bool {
	if !strings.EqualFold(strings.TrimSpace(platform), "telegram") || m == nil {
		return false
	}
	filename := stringFromMap(m, "filename", "fileName", "name")
	mimeType := stringFromMap(m, "mime_type", "mimeType")
	return isMP4File(filename, mimeType)
}

func isMP4File(filename, mimeType string) bool {
	if strings.EqualFold(strings.TrimSpace(mimeType), "video/mp4") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(filename)), ".mp4")
}

func isAutoMediaPlaceholder(text string) bool {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "[") || !strings.HasSuffix(t, "]") {
		return false
	}
	for _, p := range []string{"[文件", "[图片", "[视频", "[语音", "[贴纸", "[表情"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

func formatHumanBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * 1024
	)
	if n >= mb {
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	}
	if n >= kb {
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	}
	return fmt.Sprintf("%d B", n)
}

func stringFromMap(m map[string]any, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func matchList(allowed []string, actual string) bool {
	if len(allowed) == 0 {
		return true
	}
	return containsCI(allowed, actual)
}

func containsCI(list []string, target string) bool {
	t := strings.ToLower(strings.TrimSpace(target))
	if t == "" {
		return false
	}
	for _, item := range list {
		if strings.ToLower(strings.TrimSpace(item)) == t {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
