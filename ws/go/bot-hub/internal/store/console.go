package store

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	ansiReset   = "\x1b[0m"
	ansiDim     = "\x1b[90m"
	ansiCyan    = "\x1b[36m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiRed     = "\x1b[31m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiBold    = "\x1b[1m"
)

var consoleMu sync.Mutex

// UseColor 判断当前标准输出是否适合输出 ANSI 颜色。
func UseColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func colorize(color, text string, enable bool) string {
	if !enable || color == "" {
		return text
	}
	return color + text + ansiReset
}

// PrintConsoleLine 按统一对齐格式向终端输出单行日志。
func PrintConsoleLine(level, category, scope, summary, detail string) {
	color := UseColor()
	ts := time.Now().Format("15:04:05")

	lvlUpper := strings.ToUpper(strings.TrimSpace(level))
	if lvlUpper == "" {
		lvlUpper = "INFO"
	}
	lvlColor := ansiGreen
	switch lvlUpper {
	case "WARN":
		lvlColor = ansiYellow
	case "ERROR":
		lvlColor = ansiRed
	}
	lvlText := fmt.Sprintf("%-5s", lvlUpper)

	catLabel, catColor := formatCategory(category)

	var sb strings.Builder
	sb.WriteString(colorize(ansiDim, ts, color))
	sb.WriteString("  ")
	sb.WriteString(colorize(lvlColor, lvlText, color))
	sb.WriteString("  ")
	sb.WriteString(colorize(catColor, catLabel, color))
	sb.WriteString("  ")
	if scope != "" {
		sb.WriteString(colorize(ansiDim, "["+scope+"] ", color))
	}
	sb.WriteString(summary)

	// 仅在 warn / error 或显式需要排查时附带截断后的 detail，保持终端清爽
	if detail != "" && (lvlUpper == "WARN" || lvlUpper == "ERROR") {
		d := strings.ReplaceAll(strings.TrimSpace(detail), "\n", " ")
		runes := []rune(d)
		if len(runes) > 140 {
			d = string(runes[:140]) + "..."
		}
		sb.WriteString(colorize(ansiDim, " — "+d, color))
	}
	sb.WriteByte('\n')

	consoleMu.Lock()
	_, _ = os.Stdout.WriteString(sb.String())
	consoleMu.Unlock()
}

func formatCategory(cat string) (string, string) {
	switch cat {
	case "wx_admin":
		return "[微信群管]", ansiGreen
	case "tg_admin":
		return "[TG 群管]", ansiCyan
	case "monitor":
		return "[监控转发]", ansiBlue
	case "cooldown":
		return "[冷却拦截]", ansiYellow
	case "system":
		return "[系统状态]", ansiMagenta
	default:
		return "[运行日志]", ansiDim
	}
}

// PrintStartupBanner 输出清晰、对齐的启动概览横幅。
func PrintStartupBanner(title, version, pluginID, listenAddr, dataDir string, wxRooms, tgChats, rules, groups int) {
	color := UseColor()
	line := strings.Repeat("─", 56)

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(colorize(ansiDim, line, color) + "\n")
	sb.WriteString("  " + colorize(ansiBold+ansiCyan, title, color) + "  " + colorize(ansiDim, "v"+version+" · "+pluginID, color) + "\n")
	sb.WriteString(colorize(ansiDim, line, color) + "\n")
	sb.WriteString(fmt.Sprintf("  %-10s %s\n", colorize(ansiDim, "Web 面板", color), colorize(ansiBold+ansiBlue, "http://"+listenAddr, color)))
	sb.WriteString(fmt.Sprintf("  %-10s %s\n", colorize(ansiDim, "数据目录", color), dataDir))
	sb.WriteString(fmt.Sprintf("  %-10s 微信群 %d · TG 策略 %d · 转发规则 %d · 人员分组 %d\n",
		colorize(ansiDim, "已载配置", color), wxRooms, tgChats, rules, groups))
	sb.WriteString(colorize(ansiDim, line, color) + "\n\n")

	consoleMu.Lock()
	_, _ = os.Stdout.WriteString(sb.String())
	consoleMu.Unlock()
}
