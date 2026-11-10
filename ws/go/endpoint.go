package lwplugin

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

// Endpoint 是宿主插件服务的地址。Token 仅用于 WS 握手，不是微信访问凭据。
type Endpoint struct {
	URL   string
	Token string
}

// ResolveEndpoint 按“显式配置 > 环境变量 > 发现文件”取得连接信息。
// 使用发现文件时，每次重连都会重新读取，允许宿主重置 token 或更换动态端口。
func ResolveEndpoint(address, token, file string) (Endpoint, error) {
	if address == "" {
		address = os.Getenv("LW_PLUGIN_URL")
	}
	if token == "" {
		token = os.Getenv("LW_PLUGIN_TOKEN")
	}
	if file == "" {
		file = os.Getenv("LW_PLUGIN_ENDPOINT_FILE")
	}
	if address != "" && token == "" {
		if u, err := url.Parse(address); err == nil {
			token = u.Query().Get("token")
		}
	}
	if address != "" && token != "" {
		return checkedEndpoint(address, token)
	}
	var candidates []string
	if file != "" {
		candidates = []string{file}
	} else {
		// 双击启动、快捷方式、服务启动的工作目录可能不同。先找程序旁边的文件，
		// 让客户把 plugin-endpoint.json 与可执行文件放在一起后直接运行。
		executable, _ := os.Executable()
		dir, err := os.Getwd()
		if err != nil {
			return Endpoint{}, err
		}
		candidates = endpointCandidates(executable, dir)
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var found struct {
			Host  string `json:"host"`
			Port  int    `json:"port"`
			Token string `json:"token"`
		}
		if json.Unmarshal(data, &found) != nil || found.Port < 1 || found.Port > 65535 {
			continue
		}
		if found.Host == "" {
			found.Host = "127.0.0.1"
		}
		u := address
		if u == "" {
			u = "ws://" + net.JoinHostPort(found.Host, strconv.Itoa(found.Port)) + "/"
		}
		t := token
		if t == "" {
			t = found.Token
		}
		return checkedEndpoint(u, t)
	}
	return Endpoint{}, errors.New("未找到插件端点；请把 plugin-endpoint.json 放到程序同目录，或用 -endpoint-file / LW_PLUGIN_ENDPOINT_FILE 指定路径")
}

// endpointCandidates 与实际文件读取分开，便于验证“启动目录不同也能找到旁边文件”。
func endpointCandidates(executable, dir string) []string {
	var paths []string
	if executable != "" {
		base := filepath.Dir(executable)
		paths = append(paths, filepath.Join(base, "plugin-endpoint.json"), filepath.Join(base, "config", "plugin-endpoint.json"))
	}
	for {
		paths = append(paths, filepath.Join(dir, "plugin-endpoint.json"), filepath.Join(dir, "config", "plugin-endpoint.json"))
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return paths
}

func checkedEndpoint(address, token string) (Endpoint, error) {
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" || (u.Scheme != "ws" && u.Scheme != "wss") || u.User != nil || u.Fragment != "" {
		return Endpoint{}, errors.New("插件地址必须是有效的 ws:// 或 wss:// 地址")
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return Endpoint{}, errors.New("插件端口必须在 1..65535 范围内")
		}
	}
	// 兼容已有带 token 的 URL，但实际握手改走 Header，避免 URL 错误日志泄漏。
	q := u.Query()
	if token == "" {
		token = q.Get("token")
	}
	q.Del("token")
	u.RawQuery = q.Encode()
	if token == "" {
		return Endpoint{}, errors.New("插件连接 token 为空")
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return Endpoint{URL: u.String(), Token: token}, nil
}
