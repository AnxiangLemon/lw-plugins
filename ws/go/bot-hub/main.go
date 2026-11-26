package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/blacklist"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/config"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/monitor"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/notifier"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/store"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/tgadmin"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/web"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/wxadmin"
)

const (
	pluginID      = "lw.bot-hub"
	pluginTitle   = "Bot Hub 群管与监控中心"
	pluginVersion = "0.2.0"
)

func main() {
	endpointFile := flag.String("endpoint-file", "", "宿主 plugin-endpoint.json 路径（留空则自动发现或读取环境变量）")
	wsURL := flag.String("url", "", "远程宿主 WS 地址（可选，如 wss://host:26810）")
	wsToken := flag.String("token", "", "远程宿主 WS 令牌（可选）")
	listenAddr := flag.String("listen", "127.0.0.1:8768", "本地 Web 管理面板监听地址")
	dataDir := flag.String("data", "data/bot-hub", "配置与日志存储目录")
	flag.Parse()

	cfgMgr, err := config.Load(*dataDir)
	if err != nil {
		store.PrintConsoleLine("error", "system", "", "加载配置失败", err.Error())
		os.Exit(1)
	}
	st := store.New(*dataDir)
	st.EnableConsoleLog(true)
	bl := blacklist.New(*dataDir)
	disp := notifier.New()
	mon := monitor.New(cfgMgr, st, disp)
	tgMod := tgadmin.New(*dataDir, cfgMgr, st, bl)
	wxMod := wxadmin.New(*dataDir, cfgMgr, st, bl)

	cfgSnap := cfgMgr.Get()
	store.PrintStartupBanner(
		pluginTitle,
		pluginVersion,
		pluginID,
		*listenAddr,
		*dataDir,
		len(cfgSnap.WechatRooms),
		len(cfgSnap.TelegramChats),
		len(cfgSnap.MonitorRules),
		len(cfgSnap.MentionGroups),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go wxMod.StartTicker(ctx)

	webSrv := web.New(*listenAddr, cfgMgr, st, bl, mon, disp, wxMod, tgMod)
	go func() {
		store.PrintConsoleLine("info", "system", "", "Web 管理面板已就绪: http://"+*listenAddr, "")
		if err := webSrv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			store.PrintConsoleLine("error", "system", "", "Web 面板异常退出", err.Error())
		}
	}()

	client, err := lwplugin.New(lwplugin.Config{
		ID:           pluginID,
		Title:        pluginTitle,
		Version:      pluginVersion,
		Description:  "多平台群管理（微信 + Telegram）+ 消息监控与防刷屏转发中心（支持同平台与跨平台）",
		EndpointFile: *endpointFile,
		URL:          *wsURL,
		Token:        *wsToken,
		Events: []string{
			lwplugin.GenericMessageReceived,
			lwplugin.WechatMessageReceived,
			lwplugin.AccountOnline,
			lwplugin.AccountOffline,
			lwplugin.GroupRefreshHint,
			lwplugin.SyncHint,
			lwplugin.TelegramCallbackQuery,
			lwplugin.TelegramChatMember,
			lwplugin.TelegramChatJoinRequest,
			lwplugin.TelegramMyChatMember,
			lwplugin.TelegramEditedMessage,
			lwplugin.TelegramMessageReaction,
		},
		OnConnect: func(cCtx context.Context, c *lwplugin.Client, host lwplugin.HostInfo) error {
			disp.SetClient(c)
			wxMod.SetClient(c)
			tgMod.SetClient(c)
			st.SetConnection(true, host.Version)
			st.AddLog(store.LogEntry{
				Category: "system",
				Level:    "info",
				Summary:  "已连接 LW Tools 宿主（版本 " + host.Version + "）",
			})
			syncAccounts(cCtx, c, st, tgMod, wxMod, cfgMgr)
			return nil
		},
		OnDisconnect: func(err error) {
			st.SetConnection(false, "")
			msg := "已断开与宿主的连接"
			if err != nil {
				msg += ": " + err.Error()
			}
			st.AddLog(store.LogEntry{Category: "system", Level: "warn", Summary: msg})
		},
		OnEvent: func(eCtx context.Context, c *lwplugin.Client, ev lwplugin.Event) error {
			switch ev.Name {
			case lwplugin.AccountOnline:
				st.UpsertAccount(store.AccountInfo{Platform: ev.Account.Platform, ID: ev.Account.ID, Online: true})
				store.PrintConsoleLine("info", "system", ev.Account.Platform, fmt.Sprintf("账号上线: %s", ev.Account.ID), "")
				go fetchAccountProfile(context.Background(), c, st, ev.Account.Platform, ev.Account.ID)
				if ev.Account.Platform == "telegram" {
					go tgMod.RegisterCommands(context.Background(), c, ev.Account.ID)
				}
			case lwplugin.AccountOffline:
				st.UpsertAccount(store.AccountInfo{Platform: ev.Account.Platform, ID: ev.Account.ID, Online: false})
				store.PrintConsoleLine("warn", "system", ev.Account.Platform, fmt.Sprintf("账号离线: %s", ev.Account.ID), "")
			case lwplugin.GroupRefreshHint, lwplugin.SyncHint:
				wxMod.HandleEvent(eCtx, c, ev)
			case lwplugin.GenericMessageReceived, lwplugin.WechatMessageReceived:
				st.IncMessages()
				wxMod.HandleEvent(eCtx, c, ev)
				mon.HandleMessage(eCtx, ev)
				tgMod.HandleEvent(eCtx, c, ev)
			case lwplugin.TelegramCallbackQuery, lwplugin.TelegramChatMember, lwplugin.TelegramMyChatMember, lwplugin.TelegramChatJoinRequest:
				tgMod.HandleEvent(eCtx, c, ev)
			}
			return nil
		},
	})
	if err != nil {
		store.PrintConsoleLine("error", "system", "", "初始化 WS 客户端失败", err.Error())
		os.Exit(1)
	}

	store.PrintConsoleLine("info", "system", "", "正在连接 LW Tools 宿主（若首次接入请在宿主插件页允许连接）...", "")
	go func() {
		if runErr := client.Run(ctx); runErr != nil && !errors.Is(runErr, context.Canceled) {
			store.PrintConsoleLine("error", "system", "", "WS 客户端异常退出", runErr.Error())
		}
	}()

	<-ctx.Done()
	store.PrintConsoleLine("info", "system", "", "收到退出信号，正在优雅关闭 Bot Hub...", "")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = webSrv.Shutdown(shutCtx)
	store.PrintConsoleLine("info", "system", "", "Bot Hub 已安全停止", "")
}

func syncAccounts(
	ctx context.Context,
	c *lwplugin.Client,
	st *store.Store,
	tgMod *tgadmin.Module,
	wxMod *wxadmin.Module,
	cfgMgr *config.Manager,
) {
	var raw json.RawMessage
	if err := c.Request(ctx, "accounts.list", map[string]any{}, &raw); err != nil {
		return
	}
	type rawAcc struct {
		ID          string `json:"id"`
		AccountID   string `json:"account_id"`
		Platform    string `json:"platform"`
		DisplayName string `json:"display_name"`
		Nickname    string `json:"nickname"`
		Avatar      string `json:"avatar"`
		Online      bool   `json:"online"`
	}
	var list []rawAcc
	if err := json.Unmarshal(raw, &list); err != nil {
		var wrapped struct {
			Accounts []rawAcc `json:"accounts"`
		}
		if json.Unmarshal(raw, &wrapped) == nil {
			list = wrapped.Accounts
		}
	}
	accounts := make([]store.AccountInfo, 0, len(list))
	onlineWechat := map[string]bool{}
	for _, item := range list {
		uid := strings.TrimSpace(item.AccountID)
		if uid == "" {
			uid = strings.TrimSpace(item.ID)
		}
		if uid == "" {
			continue
		}
		plat := strings.ToLower(strings.TrimSpace(item.Platform))
		info := store.AccountInfo{
			Platform: plat,
			ID:       uid,
			Online:   item.Online,
		}
		// 微信与 Telegram 的真实昵称/头像通过平台官方接口拉取；其他外部平台驱动（QQ/飞书等）直接采用宿主上报资料。
		if plat != "wechat" && plat != "telegram" {
			info.Nickname = strings.TrimSpace(item.Nickname)
			if info.Nickname == "" {
				info.Nickname = strings.TrimSpace(item.DisplayName)
			}
			info.Avatar = strings.TrimSpace(item.Avatar)
		}
		accounts = append(accounts, info)
		if item.Online {
			go fetchAccountProfile(context.Background(), c, st, plat, uid)
		}
		if plat == "telegram" && item.Online {
			go tgMod.RegisterCommands(context.Background(), c, uid)
		}
		if plat == "wechat" && item.Online {
			onlineWechat[uid] = true
		}
	}
	if len(accounts) > 0 {
		st.SetAccounts(accounts)
		type platCount struct {
			name   string
			online int
			total  int
		}
		order := make([]string, 0, 4)
		counts := make(map[string]*platCount)
		for _, a := range accounts {
			pc, ok := counts[a.Platform]
			if !ok {
				pc = &platCount{name: displayPlatformName(a.Platform)}
				counts[a.Platform] = pc
				order = append(order, a.Platform)
			}
			pc.total++
			if a.Online {
				pc.online++
			}
		}
		parts := make([]string, 0, len(order))
		for _, p := range order {
			pc := counts[p]
			parts = append(parts, fmt.Sprintf("%s %d/%d 在线", pc.name, pc.online, pc.total))
		}
		store.PrintConsoleLine("info", "system", "", "已同步宿主账号："+strings.Join(parts, "，"), "")
	}
	// 重连后仅为尚未建立基线的在线监控群初始化基线；已有持久化缓存的群等待变动提醒或手动核对，减少不必要请求
	for _, r := range cfgMgr.Get().WechatRooms {
		if r.Enabled && onlineWechat[r.AccountID] && !wxMod.HasSnapshot(r.AccountID, r.RoomID) {
			go wxMod.CheckRoom(context.Background(), r.AccountID, r.RoomID, true)
		}
	}
}

// fetchAccountProfile 通过微信 / Telegram 官方接口拉取账号真实昵称与头像。
func displayPlatformName(p string) string {
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
	default:
		if p == "" {
			return "未知平台"
		}
		return p
	}
}

func fetchAccountProfile(parent context.Context, c *lwplugin.Client, st *store.Store, platform, uid string) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()

	switch platform {
	case "wechat":
		nick, avatar := fetchWechatProfile(ctx, c, uid)
		if nick != "" || avatar != "" {
			st.UpdateAccountProfile("wechat", uid, nick, "", avatar)
		}
	case "telegram":
		nick, username, avatar := fetchTelegramProfile(ctx, c, uid)
		if nick != "" || username != "" || avatar != "" {
			st.UpdateAccountProfile("telegram", uid, nick, username, avatar)
		}
	}
}

func fetchWechatProfile(ctx context.Context, c *lwplugin.Client, uid string) (string, string) {
	wx := c.Wechat(uid)
	// 1. 优先调用 user.get_contact_profile 获取本人资料
	res, err := wx.Call(ctx, "user.get_contact_profile", map[string]any{}, lwplugin.Options{Key: lwplugin.NewKey()})
	if err == nil {
		res, err = wx.Await(ctx, res)
	}
	if err == nil && res.Succeeded() && len(res.Data) > 0 {
		var p struct {
			UserInfo struct {
				NickName struct {
					String string `json:"string"`
				} `json:"nickName"`
			} `json:"userInfo"`
			UserInfoExt struct {
				SmallHeadImgUrl string `json:"smallHeadImgUrl"`
				BigHeadImgUrl   string `json:"bigHeadImgUrl"`
			} `json:"userInfoExt"`
		}
		if json.Unmarshal(res.Data, &p) == nil {
			nick := strings.TrimSpace(p.UserInfo.NickName.String)
			avatar := strings.TrimSpace(p.UserInfoExt.SmallHeadImgUrl)
			if avatar == "" {
				avatar = strings.TrimSpace(p.UserInfoExt.BigHeadImgUrl)
			}
			if nick != "" || avatar != "" {
				return nick, avatar
			}
		}
	}
	// 2. 降级尝试 friend.get_contact_detail 查询本人 wxid
	res2, err := wx.Call(ctx, "friend.get_contact_detail", map[string]any{"toWxids": uid}, lwplugin.Options{Key: lwplugin.NewKey()})
	if err == nil {
		res2, err = wx.Await(ctx, res2)
	}
	if err == nil && res2.Succeeded() && len(res2.Data) > 0 {
		var detail struct {
			ContactList []struct {
				NickName struct {
					String string `json:"string"`
				} `json:"nickName"`
				SmallHeadImgUrl string `json:"smallHeadImgUrl"`
				BigHeadImgUrl   string `json:"bigHeadImgUrl"`
			} `json:"contactList"`
		}
		if json.Unmarshal(res2.Data, &detail) == nil && len(detail.ContactList) > 0 {
			item := detail.ContactList[0]
			nick := strings.TrimSpace(item.NickName.String)
			avatar := strings.TrimSpace(item.SmallHeadImgUrl)
			if avatar == "" {
				avatar = strings.TrimSpace(item.BigHeadImgUrl)
			}
			return nick, avatar
		}
	}
	return "", ""
}

func fetchTelegramProfile(ctx context.Context, c *lwplugin.Client, uid string) (string, string, string) {
	tg := c.Telegram(uid)
	var me struct {
		ID        int64  `json:"id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Username  string `json:"username"`
	}
	if err := tg.Call(ctx, "getMe", nil, &me); err != nil {
		return "", "", ""
	}
	nick := strings.TrimSpace(strings.TrimSpace(me.FirstName) + " " + strings.TrimSpace(me.LastName))
	username := strings.TrimSpace(me.Username)
	if nick == "" && username != "" {
		nick = "@" + username
	}
	var avatar string
	if me.ID > 0 {
		var photos struct {
			Photos [][]struct {
				FileID string `json:"file_id"`
			} `json:"photos"`
		}
		if err := tg.Call(ctx, "getUserProfilePhotos", map[string]any{"user_id": me.ID, "limit": 1}, &photos); err == nil {
			if len(photos.Photos) > 0 && len(photos.Photos[0]) > 0 {
				fileID := photos.Photos[0][0].FileID
				if dl, dlErr := tg.DownloadFile(ctx, fileID); dlErr == nil && dl.Base64 != "" {
					mime := dl.MimeType
					if mime == "" {
						mime = "image/jpeg"
					}
					avatar = "data:" + mime + ";base64," + dl.Base64
				}
			}
		}
	}
	return nick, username, avatar
}
