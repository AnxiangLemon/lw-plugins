package manager

import (
	"encoding/base64"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// nonce 是每次 HTML 文档的样式授权，不能复用固定值，也不能扩展脚本执行权限。
func TestFrontendHTMLUsesIndependentStyleNonce(t *testing.T) {
	m, _ := testManager(t, &fakeHost{})
	h := m.Handler()
	pattern := regexp.MustCompile(`<meta name="csp-nonce" content="([A-Za-z0-9+/]+)">`)
	seen := map[string]bool{}
	for _, path := range []string{"/", "/index.html", "/"} {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8766"+path, nil)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("页面请求失败: %s, %d", path, response.Code)
		}
		match := pattern.FindStringSubmatch(response.Body.String())
		if len(match) != 2 {
			t.Fatal("页面缺少 nonce meta")
		}
		nonce := match[1]
		decoded, err := base64.RawStdEncoding.DecodeString(nonce)
		if err != nil || len(decoded) != 18 || seen[nonce] {
			t.Fatal("nonce 长度无效或跨文档重复")
		}
		seen[nonce] = true
		csp := response.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "style-src 'self' 'nonce-"+nonce+"'") || !strings.Contains(csp, "script-src 'self';") || strings.Contains(csp, "unsafe-inline") {
			t.Fatal("CSP 与文档 nonce 不匹配或意外放开内联脚本")
		}
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatal("HTML 安全响应头缺失")
		}
	}
}

// nonce 仅应用于 HTML，JS/CSS/SVG 仍使用原来的同源静态资源策略。
func TestFrontendAssetsKeepSameOriginPolicy(t *testing.T) {
	m, _ := testManager(t, &fakeHost{})
	h := m.Handler()
	count := 0
	err := fs.WalkDir(assets, "web", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || path == "web/index.html" {
			return nil
		}
		if !strings.HasSuffix(path, ".css") && !strings.HasSuffix(path, ".js") && !strings.HasSuffix(path, ".svg") {
			return nil
		}
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8766/"+strings.TrimPrefix(path, "web/"), nil))
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Fatalf("静态资源不可用: %s", path)
		}
		if response.Header().Get("Content-Security-Policy") != localContentSecurityPolicy {
			t.Fatalf("静态资源策略被改变: %s", path)
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 2 {
		t.Fatal("缺少可服务的前端静态资源")
	}
}

func TestFrontendNonceDoesNotRelaxAPIOriginChecks(t *testing.T) {
	m, _ := testManager(t, &fakeHost{})
	h := m.Handler()
	for _, test := range []struct{ path, host, origin, contentType, header string }{
		{"/", "evil.test:8766", "", "", ""},
		{"/index.html", "127.0.0.1:8766", "https://evil.test", "", ""},
		{"/api/config", "127.0.0.1:8766", "https://evil.test", "application/json", "1"},
		{"/api/config", "127.0.0.1:8766", "", "application/json", ""},
		{"/api/config", "127.0.0.1:8766", "", "text/plain", "1"},
	} {
		method := http.MethodGet
		if test.path == "/api/config" {
			method = http.MethodPost
		}
		request := httptest.NewRequest(method, "http://"+test.host+test.path, strings.NewReader(`{}`))
		request.Header.Set("Origin", test.origin)
		request.Header.Set("Content-Type", test.contentType)
		request.Header.Set("X-Plugin-Request", test.header)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("放行了非法请求: %s %s", method, test.path)
		}
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8766/api/status", nil))
	if response.Code != http.StatusOK || response.Header().Get("Content-Security-Policy") != localContentSecurityPolicy {
		t.Fatal("业务 API 的同源策略被改变")
	}
}
