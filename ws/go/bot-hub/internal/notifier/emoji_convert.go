package notifier

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/config"
)

const (
	ffmpegEmojiTimeout    = 12 * time.Second
	maxWechatEmojiBytes   = 2 * 1024 * 1024 // 微信自定义表情建议控制在 2MB 以内以提高上传与发送成功率
	maxEmojiSourceSeconds = "8"             // 动图转表情最多截取前 8 秒，避免生成超大 GIF
)

// canWechatEmojiForward 判断是否可直接免上传转发微信原生表情（msg_type=47）。
func canWechatEmojiForward(media *MediaPayload) bool {
	if media == nil || media.SourcePlatform != "wechat" || media.Kind != "sticker" {
		return false
	}
	return strings.TrimSpace(media.WxEmojiMD5) != "" && media.WxEmojiLen > 0
}

// forwardWechatEmoji 直接凭原消息中的 md5 与 totalLen 调用 msg.send_emoji 免上传转发微信表情。
func (d *Dispatcher) forwardWechatEmoji(ctx context.Context, c *lwplugin.Client, target config.NotifyTarget, media *MediaPayload) error {
	if !canWechatEmojiForward(media) {
		return errors.New("缺少微信表情 MD5 或长度信息")
	}
	return d.sendWechatEmojiByMD5(ctx, c, target.AccountID, target.To, media.WxEmojiMD5, media.WxEmojiLen)
}

// uploadAndSendWechatEmoji 先调用 emoji.upload 上传表情字节，再凭返回的 md5/totalLen 调用 msg.send_emoji 发送。
func (d *Dispatcher) uploadAndSendWechatEmoji(ctx context.Context, c *lwplugin.Client, target config.NotifyTarget, b64 string) error {
	cleanB64 := stripDataURIPrefix(b64)
	if cleanB64 == "" {
		return errors.New("表情数据为空")
	}
	if size := approxBase64Bytes(cleanB64); size <= 0 || size > maxWechatEmojiBytes {
		return fmt.Errorf("表情大小 %s 超出微信表情上限 %s", formatBytes(size), formatBytes(maxWechatEmojiBytes))
	}

	wx := c.Wechat(target.AccountID)
	res, err := wx.Call(ctx, "emoji.upload", map[string]any{
		"base64": cleanB64,
	}, lwplugin.Options{Key: lwplugin.NewKey()})
	if err == nil && (res.Status == "queued" || res.Status == "running") {
		res, err = wx.Await(ctx, res)
	}
	if err != nil {
		return err
	}
	if res.Status == "failed" {
		if res.Error != nil {
			return fmt.Errorf("emoji.upload 失败 (%s): %s", res.Error.Code, res.Error.Message)
		}
		return errors.New("emoji.upload 失败")
	}

	md5Str, totalLen := extractEmojiUploadMeta(res.Data)
	if md5Str == "" || totalLen <= 0 {
		return errors.New("emoji.upload 未返回有效的 md5 或 totalLen")
	}
	return d.sendWechatEmojiByMD5(ctx, c, target.AccountID, target.To, md5Str, totalLen)
}

func (d *Dispatcher) sendWechatEmojiByMD5(ctx context.Context, c *lwplugin.Client, accountID, to, md5Str string, totalLen int64) error {
	wx := c.Wechat(accountID)
	res, err := wx.Call(ctx, "msg.send_emoji", map[string]any{
		"toWxid":   to,
		"md5":      strings.TrimSpace(md5Str),
		"totalLen": totalLen,
	}, lwplugin.Options{Key: lwplugin.NewKey()})
	if err == nil && (res.Status == "queued" || res.Status == "running") {
		res, err = wx.Await(ctx, res)
	}
	if err != nil {
		return err
	}
	if res.Status == "failed" {
		if res.Error != nil {
			return fmt.Errorf("msg.send_emoji 失败 (%s): %s", res.Error.Code, res.Error.Message)
		}
		return errors.New("msg.send_emoji 失败")
	}
	return nil
}

func extractEmojiUploadMeta(raw json.RawMessage) (string, int64) {
	if len(raw) == 0 {
		return "", 0
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", 0
	}
	if inner, ok := m["data"].(map[string]any); ok {
		m = inner
	}
	md5Str, _ := m["md5"].(string)
	md5Str = strings.TrimSpace(md5Str)

	var totalLen int64
	switch v := m["totalLen"].(type) {
	case float64:
		totalLen = int64(v)
	case int64:
		totalLen = v
	case json.Number:
		if n, err := v.Int64(); err == nil {
			totalLen = n
		}
	}
	if totalLen <= 0 {
		if v, ok := m["total_len"].(float64); ok {
			totalLen = int64(v)
		}
	}
	return md5Str, totalLen
}

// convertVideoToEmojiGIF 使用本机 ffmpeg 将短无声视频（如 TG animation .mp4 / .webm 贴纸）转为压缩 GIF 表情。
// 若未安装 ffmpeg、转码失败或结果超出微信表情大小上限，返回 ("", false)，由调用方降级为视频/文件发送。
func convertVideoToEmojiGIF(ctx context.Context, videoBase64, ext string) (string, bool) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", false
	}
	cleanB64 := stripDataURIPrefix(videoBase64)
	if cleanB64 == "" {
		return "", false
	}
	videoBytes, err := base64.StdEncoding.DecodeString(cleanB64)
	if err != nil || len(videoBytes) == 0 {
		return "", false
	}

	suffix := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if suffix == "" {
		suffix = "mp4"
	}
	tmpFile, err := os.CreateTemp("", "bothub-emoji-*."+suffix)
	if err != nil {
		return "", false
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(videoBytes); err != nil {
		_ = tmpFile.Close()
		return "", false
	}
	if err := tmpFile.Close(); err != nil {
		return "", false
	}

	// 两档清晰度尝试：优先 240px@10fps，若超 2MB 则自动降档到 160px@8fps
	profiles := []string{
		"fps=10,scale='min(240,iw)':-1:flags=lanczos,split[s0][s1];[s0]palettegen=max_colors=128:stats_mode=diff[p];[s1][p]paletteuse=dither=bayer:bayer_scale=3",
		"fps=8,scale='min(160,iw)':-1:flags=lanczos,split[s0][s1];[s0]palettegen=max_colors=64:stats_mode=diff[p];[s1][p]paletteuse=dither=bayer:bayer_scale=3",
	}

	for _, vf := range profiles {
		runCtx, cancel := context.WithTimeout(ctx, ffmpegEmojiTimeout)
		cmd := exec.CommandContext(
			runCtx,
			ffmpegPath,
			"-hide_banner",
			"-loglevel", "error",
			"-y",
			"-t", maxEmojiSourceSeconds,
			"-i", tmpPath,
			"-an",
			"-vf", vf,
			"-f", "gif",
			"pipe:1",
		)
		var out bytes.Buffer
		cmd.Stdout = &out
		runErr := cmd.Run()
		cancel()
		if runErr != nil {
			continue
		}
		gifBytes := out.Bytes()
		if len(gifBytes) >= 6 && len(gifBytes) <= maxWechatEmojiBytes &&
			gifBytes[0] == 'G' && gifBytes[1] == 'I' && gifBytes[2] == 'F' && gifBytes[3] == '8' {
			return base64.StdEncoding.EncodeToString(gifBytes), true
		}
	}
	return "", false
}

func (m *MediaPayload) emojiGIFBase64(ctx context.Context, videoBase64, ext string) (string, bool) {
	if m == nil {
		return convertVideoToEmojiGIF(ctx, videoBase64, ext)
	}
	m.cacheMu.Lock()
	if m.emojiConverted {
		res, ok := m.cachedEmojiGIF, m.cachedEmojiGIF != ""
		m.cacheMu.Unlock()
		return res, ok
	}
	m.cacheMu.Unlock()

	gifB64, ok := convertVideoToEmojiGIF(ctx, videoBase64, ext)

	m.cacheMu.Lock()
	m.emojiConverted = true
	if ok {
		m.cachedEmojiGIF = gifB64
	}
	m.cacheMu.Unlock()
	return gifB64, ok
}

func stripDataURIPrefix(b64 string) string {
	s := strings.TrimSpace(b64)
	if idx := strings.Index(s, ";base64,"); idx > 0 {
		s = s[idx+len(";base64,"):]
	}
	return strings.TrimSpace(s)
}

func isGIFData(b64, filename, mimeType string) bool {
	if strings.EqualFold(strings.TrimSpace(mimeType), "image/gif") ||
		strings.HasSuffix(strings.ToLower(strings.TrimSpace(filename)), ".gif") {
		return true
	}
	s := stripDataURIPrefix(b64)
	if len(s) < 8 {
		return false
	}
	if len(s) > 16 {
		s = s[:16]
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
	}
	return err == nil && len(raw) >= 4 && raw[0] == 'G' && raw[1] == 'I' && raw[2] == 'F' && raw[3] == '8'
}
