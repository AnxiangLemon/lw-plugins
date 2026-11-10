package manager

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

//go:embed web/*
var assets embed.FS

const localContentSecurityPolicy = "default-src 'self'; connect-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'"

// Handler 提供本机设置和审计页面，浏览器不接触宿主 token。
// 除监听回环地址外仍校验 Host/Origin，避免其他网站通过本机端口修改配置。
func (m *Manager) Handler() http.Handler {
	mux := http.NewServeMux()
	m.dashboardRoutes(mux)
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"connected": m.host.Connected(), "connection_error": m.ConnectionError(), "config": m.Config(), "groups": m.Status(), "directory": m.DirectoryStatuses()})
	})
	mux.HandleFunc("GET /api/accounts", func(w http.ResponseWriter, r *http.Request) {
		var rows []struct {
			ID       string `json:"account_id"`
			Name     string `json:"display_name"`
			Platform string `json:"platform"`
			Online   bool   `json:"online"`
		}
		if err := m.host.Request(r.Context(), "accounts.list", map[string]any{}, &rows); err != nil {
			httpError(w, err)
			return
		}
		// 白名单投影，只向页面提供账号显示信息。
		out := []map[string]any{}
		for _, a := range rows {
			if a.Platform == "wechat" {
				out = append(out, map[string]any{"account_id": a.ID, "display_name": a.Name, "online": a.Online})
			}
		}
		writeJSON(w, out)
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		rows, err := m.store.SearchEvents(r.URL.Query().Get("account_id"), r.URL.Query().Get("room_id"), r.URL.Query().Get("q"), r.URL.Query().Get("kind"), n)
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, rows)
	})
	mux.HandleFunc("GET /api/members", func(w http.ResponseWriter, r *http.Request) {
		s, err := m.store.Members(r.URL.Query().Get("account_id"), r.URL.Query().Get("room_id"))
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, s)
	})
	mux.HandleFunc("POST /api/config", func(w http.ResponseWriter, r *http.Request) {
		var c Config
		if err := readJSON(w, r, &c); err != nil {
			httpError(w, err)
			return
		}
		if err := m.SetConfig(c); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/recheck", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AccountID string `json:"account_id"`
			RoomID    string `json:"room_id"`
			Reset     bool   `json:"reset"`
		}
		if err := readJSON(w, r, &body); err != nil {
			httpError(w, err)
			return
		}
		if err := m.Recheck(body.AccountID, body.RoomID, body.Reset); err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, map[string]bool{"queued": true})
	})
	mux.HandleFunc("POST /api/receipt", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID int64 `json:"id"`
		}
		if err := readJSON(w, r, &body); err != nil {
			httpError(w, err)
			return
		}
		out, err := m.Receipt(r.Context(), body.ID)
		if err != nil {
			httpError(w, err)
			return
		}
		writeJSON(w, out)
	})
	sub, _ := fs.Sub(assets, "web")
	mux.Handle("GET /", http.FileServer(http.FS(sub)))
	mux.HandleFunc("GET /{$}", serveFrontendIndex)
	mux.HandleFunc("GET /index.html", serveFrontendIndex)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "仅允许本机访问", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "不允许跨来源访问", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost && (!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || r.Header.Get("X-Plugin-Request") != "1") {
			http.Error(w, "需要同源 JSON 请求", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", localContentSecurityPolicy)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

// serveFrontendIndex 仅为内嵌入口注入一次性样式 nonce。
// Radix 的滚动锁定等组件会创建 style 元素，前端从 meta 读取 nonce 后传给组件库。
// 每次文档请求使用独立随机值；外部脚本仍只允许同源，不开放 unsafe-inline。
// API 与其他静态资源沿用原始 CSP，业务请求的 Host、Origin 和 JSON 校验保持不变。
func serveFrontendIndex(w http.ResponseWriter, r *http.Request) {
	index, err := assets.ReadFile("web/index.html")
	if err != nil || !strings.Contains(string(index), "</head>") {
		http.Error(w, "内嵌页面不可用，请重新构建前端资源", http.StatusInternalServerError)
		return
	}
	var random [18]byte
	if _, err = rand.Read(random[:]); err != nil {
		http.Error(w, "生成页面安全参数失败", http.StatusInternalServerError)
		return
	}
	nonce := base64.RawStdEncoding.EncodeToString(random[:])
	csp := strings.Replace(localContentSecurityPolicy, "style-src 'self'", "style-src 'self' 'nonce-"+nonce+"'", 1)
	w.Header().Set("Content-Security-Policy", csp)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	meta := "<meta name=\"csp-nonce\" content=\"" + nonce + "\">\n</head>"
	_, _ = io.WriteString(w, strings.Replace(string(index), "</head>", meta, 1))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}
func httpError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	writeJSON(w, map[string]string{"error": err.Error()})
}
func readJSON(w http.ResponseWriter, r *http.Request, out any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("请求只能包含一个 JSON 对象")
	}
	return nil
}

// ListenAddress 将本地设置页限制在回环地址；服务器部署可用 SSH 端口转发查看。
func ListenAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("设置页面必须监听回环 IP，例如 127.0.0.1:8766")
	}
	return nil
}
