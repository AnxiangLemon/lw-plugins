package notifier

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	ffmpegCoverTimeout  = 8 * time.Second
	maxVideoCoverBytes  = 512 * 1024
)

// extractVideoCoverBase64 使用本机 ffmpeg 抽取视频第一帧作为 JPEG 封面（Base64）。
// 若本机未安装 ffmpeg、抽帧超时或失败，则自动降级返回 fallbackVideoCoverBase64。
func extractVideoCoverBase64(ctx context.Context, videoBase64 string) string {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fallbackVideoCoverBase64
	}

	rawB64 := strings.TrimSpace(videoBase64)
	if idx := strings.Index(rawB64, ";base64,"); idx > 0 {
		rawB64 = rawB64[idx+len(";base64,"):]
	}
	if rawB64 == "" {
		return fallbackVideoCoverBase64
	}
	videoBytes, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil || len(videoBytes) == 0 {
		return fallbackVideoCoverBase64
	}

	tmpFile, err := os.CreateTemp("", "bothub-video-cover-*.mp4")
	if err != nil {
		return fallbackVideoCoverBase64
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(videoBytes); err != nil {
		_ = tmpFile.Close()
		return fallbackVideoCoverBase64
	}
	if err := tmpFile.Close(); err != nil {
		return fallbackVideoCoverBase64
	}

	runCtx, cancel := context.WithTimeout(ctx, ffmpegCoverTimeout)
	defer cancel()

	cmd := exec.CommandContext(
		runCtx,
		ffmpegPath,
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", tmpPath,
		"-frames:v", "1",
		"-vf", "scale='min(480,iw)':-2",
		"-q:v", "4",
		"-f", "image2",
		"-vcodec", "mjpeg",
		"pipe:1",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return fallbackVideoCoverBase64
	}

	jpegBytes := out.Bytes()
	if len(jpegBytes) < 4 || len(jpegBytes) > maxVideoCoverBytes {
		return fallbackVideoCoverBase64
	}
	// 校验 JPEG SOI 魔数 (0xFF 0xD8)
	if jpegBytes[0] != 0xFF || jpegBytes[1] != 0xD8 {
		return fallbackVideoCoverBase64
	}
	return base64.StdEncoding.EncodeToString(jpegBytes)
}

func (m *MediaPayload) videoCoverBase64(ctx context.Context, videoBase64 string) string {
	if m == nil {
		return extractVideoCoverBase64(ctx, videoBase64)
	}
	m.cacheMu.Lock()
	if m.coverExtracted {
		cover := m.cachedCover
		m.cacheMu.Unlock()
		return cover
	}
	m.cacheMu.Unlock()

	cover := extractVideoCoverBase64(ctx, videoBase64)

	m.cacheMu.Lock()
	m.coverExtracted = true
	m.cachedCover = cover
	m.cacheMu.Unlock()
	return cover
}
