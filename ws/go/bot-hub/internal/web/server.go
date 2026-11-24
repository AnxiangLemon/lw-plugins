package web

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/blacklist"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/config"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/monitor"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/notifier"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/store"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/tgadmin"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/wxadmin"
)

//go:embed static/*
var staticFS embed.FS

// Server 本地 Web 管理面板（单二进制内嵌静态资源，零外部运行依赖）。
type Server struct {
	cfgMgr   *config.Manager
	store    *store.Store
	bl       *blacklist.Store
	monitor  *monitor.Engine
	notifier *notifier.Dispatcher
	wxAdmin  *wxadmin.Module
	tgAdmin  *tgadmin.Module
	srv      *http.Server
}

func New(
	listenAddr string,
	cfgMgr *config.Manager,
	st *store.Store,
	bl *blacklist.Store,
	mon *monitor.Engine,
	disp *notifier.Dispatcher,
	wxMod *wxadmin.Module,
	tgMod *tgadmin.Module,
) *Server {
	s := &Server{
		cfgMgr:   cfgMgr,
		store:    st,
		bl:       bl,
		monitor:  mon,
		notifier: disp,
		wxAdmin:  wxMod,
		tgAdmin:  tgMod,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/logs/clear", s.handleClearLogs)
	mux.HandleFunc("/api/cooldowns/clear", s.handleClearCooldowns)
	mux.HandleFunc("/api/test-notify", s.handleTestNotify)
	mux.HandleFunc("/api/blacklist", s.handleBlacklist)
	mux.HandleFunc("/api/blacklist/add", s.handleBlacklistAdd)
	mux.HandleFunc("/api/blacklist/remove", s.handleBlacklistRemove)
	mux.HandleFunc("/api/wechat/discover", s.handleWxDiscover)
	mux.HandleFunc("/api/wechat/check", s.handleWxCheck)
	mux.HandleFunc("/api/wechat/members", s.handleWxMembers)
	mux.HandleFunc("/api/wechat/action", s.handleWxAction)
	mux.HandleFunc("/api/wechat/changes/clear", s.handleClearWxChanges)
	mux.HandleFunc("/api/telegram/check-perms", s.handleTgCheckPerms)
	mux.HandleFunc("/api/telegram/action", s.handleTgAction)
	mux.HandleFunc("/api/avatar/", s.handleAvatar)

	sub, _ := fs.Sub(staticFS, "static")
	fileServer := http.FileServer(http.FS(sub))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		fileServer.ServeHTTP(w, r)
	}))

	s.srv = &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

func (s *Server) Start() error {
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stats":        s.store.Stats(),
		"accounts":     s.store.Accounts(),
		"chats":        s.store.Chats(),
		"cooldowns":    s.monitor.ActiveCooldowns(),
		"logs":         s.store.Logs("all", 25),
		"wechat_state": s.wxAdmin.StateSnapshot(),
		"tg_state":     s.tgAdmin.StateSnapshot(),
		"blacklist":    s.bl.List("all"),
	})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.cfgMgr.Get())
	case http.MethodPut, http.MethodPost:
		var next config.Config
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&next); err != nil {
			writeErr(w, http.StatusBadRequest, "配置 JSON 解析失败: "+err.Error())
			return
		}
		for _, r := range next.WechatRooms {
			rid := strings.TrimSpace(r.RoomID)
			if !strings.HasSuffix(rid, "@chatroom") || len(rid) <= len("@chatroom") {
				writeErr(w, http.StatusBadRequest, "微信群 ID 格式无效，必须以 @chatroom 结尾: "+r.RoomID)
				return
			}
		}
		seenTg := make(map[string]bool, len(next.TelegramChats))
		for _, tc := range next.TelegramChats {
			cid := strings.TrimSpace(tc.ChatID)
			if cid == "" {
				writeErr(w, http.StatusBadRequest, "Telegram 群 ID 不能为空")
				return
			}
			if cid != "*" {
				if v, err := strconv.ParseInt(cid, 10, 64); err != nil || v >= 0 {
					writeErr(w, http.StatusBadRequest, "Telegram 群 ID 格式无效，应为 * 或以 - 开头的群数字 ID（如 -1001234567890）: "+tc.ChatID)
					return
				}
			}
			if seenTg[cid] {
				writeErr(w, http.StatusBadRequest, "存在重复的 Telegram 群策略: "+cid)
				return
			}
			seenTg[cid] = true
		}
		if err := s.cfgMgr.Update(next); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存配置失败: "+err.Error())
			return
		}
		s.store.AddLog(store.LogEntry{
			Category: "system",
			Level:    "info",
			Summary:  "通过 Web 面板更新了配置",
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	cat := r.URL.Query().Get("category")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"logs": s.store.Logs(cat, limit),
	})
}

func (s *Server) handleClearLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Category string `json:"category"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req)
	if err := s.store.ClearLogs(req.Category); err != nil {
		writeErr(w, http.StatusInternalServerError, "清空日志失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleClearWxChanges(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.wxAdmin.ClearChanges()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleClearCooldowns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.monitor.ClearCooldowns()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTestNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Target config.NotifyTarget `json:"target"`
		Body   string              `json:"body"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	if req.Body == "" {
		req.Body = "这是一条来自 LW Bot Hub 的测试通知（" + time.Now().Format("15:04:05") + "）"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s.notifier.Send(ctx, req.Target, req.Body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleWxDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		AccountID string `json:"account_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数无效")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	rooms, err := s.wxAdmin.DiscoverRooms(ctx, req.AccountID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

func (s *Server) handleWxCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		AccountID string `json:"account_id"`
		RoomID    string `json:"room_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数无效")
		return
	}
	snap, err := s.wxAdmin.CheckRoom(r.Context(), req.AccountID, req.RoomID, true)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleWxMembers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	acc := r.URL.Query().Get("account_id")
	room := r.URL.Query().Get("room_id")
	refresh := r.URL.Query().Get("refresh") == "1"
	if !refresh {
		if snap, ok := s.wxAdmin.SnapshotOf(acc, room); ok {
			writeJSON(w, http.StatusOK, snap)
			return
		}
	}
	snap, err := s.wxAdmin.CheckRoom(r.Context(), acc, room, true)
	if err != nil {
		if fallback, ok := s.wxAdmin.SnapshotOf(acc, room); ok {
			writeJSON(w, http.StatusOK, fallback)
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleWxAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		AccountID string `json:"account_id"`
		RoomID    string `json:"room_id"`
		Action    string `json:"action"`
		Value     string `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数无效")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.wxAdmin.RoomAction(ctx, req.AccountID, req.RoomID, req.Action, req.Value); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTgCheckPerms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		AccountID string `json:"account_id"`
		ChatID    string `json:"chat_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数无效")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	st, err := s.tgAdmin.CheckBotPermissions(ctx, req.AccountID, req.ChatID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleTgAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		AccountID string `json:"account_id"`
		ChatID    string `json:"chat_id"`
		UserID    int64  `json:"user_id"`
		Action    string `json:"action"`
		Minutes   int    `json:"minutes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数无效")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s.tgAdmin.WebAction(ctx, req.AccountID, req.ChatID, req.UserID, req.Action, req.Minutes); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleBlacklist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	platform := r.URL.Query().Get("platform")
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": s.bl.List(platform),
	})
}

func (s *Server) handleBlacklistAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Platform     string `json:"platform"`
		UserID       string `json:"user_id"`
		UserName     string `json:"user_name"`
		Reason       string `json:"reason"`
		KickExisting *bool  `json:"kick_existing"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数无效")
		return
	}
	platform := strings.ToLower(strings.TrimSpace(req.Platform))
	userID := strings.TrimSpace(req.UserID)
	if platform == "" {
		writeErr(w, http.StatusBadRequest, "请选择目标平台")
		return
	}
	if userID == "" {
		writeErr(w, http.StatusBadRequest, "请填写用户 ID")
		return
	}
	if platform == "telegram" {
		if uid, err := strconv.ParseInt(userID, 10, 64); err != nil || uid <= 0 {
			writeErr(w, http.StatusBadRequest, "Telegram 用户 ID 必须是正整数")
			return
		}
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "管理员手动加入全局黑名单"
	}
	entry, _ := s.bl.Add(blacklist.Entry{
		Platform: platform,
		UserID:   userID,
		UserName: strings.TrimSpace(req.UserName),
		Reason:   reason,
	})

	kicked := 0
	kickNow := req.KickExisting == nil || *req.KickExisting
	if kickNow {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		switch platform {
		case "telegram":
			if uid, err := strconv.ParseInt(userID, 10, 64); err == nil && uid > 0 {
				kicked = s.tgAdmin.BanUserAcrossKnownChats(ctx, uid)
			}
		case "wechat":
			kicked = s.wxAdmin.KickUserAcrossManagedRooms(ctx, userID)
		}
	}

	cat := "system"
	switch platform {
	case "telegram":
		cat = "tg_admin"
	case "wechat":
		cat = "wx_admin"
	}
	s.store.IncAdminAction()
	s.store.AddLog(store.LogEntry{
		Category: cat, Platform: platform,
		Summary: "已加入全平台黑名单：" + entry.UserName + " (" + entry.UserID + ")",
		Detail:  reason,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "entry": entry, "kicked": kicked})
}

func (s *Server) handleBlacklistRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Platform string `json:"platform"`
		UserID   string `json:"user_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "参数无效")
		return
	}
	platform := strings.ToLower(strings.TrimSpace(req.Platform))
	userID := strings.TrimSpace(req.UserID)
	if platform == "" || userID == "" {
		writeErr(w, http.StatusBadRequest, "缺少平台或用户 ID")
		return
	}
	entry, existed := s.bl.Remove(platform, userID)
	if platform == "telegram" {
		if uid, err := strconv.ParseInt(userID, 10, 64); err == nil && uid > 0 {
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			s.tgAdmin.UnbanUserAcrossChats(ctx, uid, entry.BannedChats)
		}
	}
	if existed {
		cat := "system"
		switch platform {
		case "telegram":
			cat = "tg_admin"
		case "wechat":
			cat = "wx_admin"
		}
		s.store.IncAdminAction()
		s.store.AddLog(store.LogEntry{
			Category: cat, Platform: platform,
			Summary: "已移出全平台黑名单：" + entry.UserName + " (" + entry.UserID + ")",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/avatar/")
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=604800")
	http.ServeFile(w, r, filepath.Join(s.wxAdmin.AvatarDir(), name))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}
