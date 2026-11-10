// Package manager 实现群成员核对，不把服务端的刷新提示解释为进退群事实。
package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Group 表示一个明确关注的普通微信群。默认始终写审计，发消息单独控制。
type Group struct {
	AccountID     string `json:"account_id"`
	RoomID        string `json:"room_id"`
	Label         string `json:"label"`
	Enabled       bool   `json:"enabled"`
	NotifyJoin    bool   `json:"notify_join"`
	NotifyLeave   bool   `json:"notify_leave"`
	JoinTemplate  string `json:"join_template"`
	LeaveTemplate string `json:"leave_template"`
}

func (g Group) key() string { return g.AccountID + "\x00" + g.RoomID }

// Config 只存业务设置，连接 token 通过宿主发现文件或环境变量提供。
// PollSeconds=0 关闭定时核对；开启后也只处理用户关注的群，不遍历所有群。
type Config struct {
	DebounceMS         int     `json:"debounce_ms"`
	CooldownSeconds    int     `json:"cooldown_seconds"`
	WeakRecheckSeconds int     `json:"weak_recheck_seconds"`
	PollSeconds        int     `json:"poll_seconds"`
	RetentionDays      int     `json:"retention_days"`
	Groups             []Group `json:"groups"`
}

func DefaultConfig() Config {
	return Config{DebounceMS: 1000, CooldownSeconds: 30, WeakRecheckSeconds: 60, RetentionDays: 30, Groups: []Group{}}
}
func (c Config) Validate() error {
	if c.DebounceMS < 100 || c.DebounceMS > 30000 || c.CooldownSeconds < 1 || c.CooldownSeconds > 3600 {
		return errors.New("去抖需为 100..30000 毫秒，核对间隔需为 1..3600 秒")
	}
	if c.WeakRecheckSeconds < 0 || c.WeakRecheckSeconds > 86400 || (c.PollSeconds != 0 && (c.PollSeconds < 60 || c.PollSeconds > 86400)) {
		return errors.New("定时核对需关闭或设为 60..86400 秒，弱提示间隔不能为负")
	}
	if c.RetentionDays < 1 || c.RetentionDays > 3650 || len(c.Groups) > 100 {
		return errors.New("日志保留需为 1..3650 天，最多关注 100 个群")
	}
	seen := map[string]bool{}
	for _, g := range c.Groups {
		if g.AccountID == "" || len(g.AccountID) > 128 || strings.ContainsAny(g.AccountID, " \t\r\n\x00") {
			return errors.New("账号 ID 不能为空或包含空白")
		}
		if !strings.HasSuffix(g.RoomID, "@chatroom") || len(g.RoomID) <= len("@chatroom") || strings.ContainsAny(g.RoomID, " \t\r\n\x00,") {
			return fmt.Errorf("%s：成员监控只支持普通微信群 @chatroom", g.RoomID)
		}
		if len(g.Label) > 256 || len(g.JoinTemplate) > 2000 || len(g.LeaveTemplate) > 2000 {
			return errors.New("群名称或消息模板过长")
		}
		if seen[g.key()] {
			return errors.New("同一账号下不能重复关注同一个群")
		}
		seen[g.key()] = true
	}
	return nil
}
func LoadConfig(path string) (Config, error) {
	c := DefaultConfig()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, SaveConfig(path, c)
	}
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return c, c.Validate()
}

// SaveConfig 先完整校验再原子替换，磁盘失败时不改运行中的配置。
func SaveConfig(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(b, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
