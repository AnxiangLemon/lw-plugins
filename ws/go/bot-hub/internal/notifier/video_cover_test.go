package notifier

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExtractVideoCoverBase64(t *testing.T) {
	// 1. 无效数据应自动降级返回默认 1x1 封面
	if got := extractVideoCoverBase64(context.Background(), "not-valid-base64!!!"); got != fallbackVideoCoverBase64 {
		t.Fatalf("expected fallback cover for invalid base64, got len=%d", len(got))
	}

	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("本机未安装 ffmpeg，跳过真实抽帧测试")
	}

	// 2. 用 ffmpeg 生成一个 320x240 的测试 MP4
	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "sample.mp4")
	cmd := exec.Command(
		ffmpegPath,
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-f", "lavfi",
		"-i", "color=c=blue:s=320x240:d=0.4",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		mp4Path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate test mp4 failed: %v (%s)", err, string(out))
	}
	mp4Bytes, err := os.ReadFile(mp4Path)
	if err != nil {
		t.Fatalf("read test mp4 failed: %v", err)
	}
	videoB64 := base64.StdEncoding.EncodeToString(mp4Bytes)

	media := &MediaPayload{Kind: "video"}
	coverB64 := media.videoCoverBase64(context.Background(), videoB64)
	if coverB64 == "" || coverB64 == fallbackVideoCoverBase64 {
		t.Fatalf("expected real extracted cover, got fallback or empty")
	}

	coverBytes, err := base64.StdEncoding.DecodeString(coverB64)
	if err != nil {
		t.Fatalf("decode cover base64 failed: %v", err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(coverBytes))
	if err != nil {
		t.Fatalf("extracted cover is not valid jpeg: %v", err)
	}
	if cfg.Width != 320 || cfg.Height != 240 {
		t.Fatalf("unexpected cover dimensions: %dx%d", cfg.Width, cfg.Height)
	}

	// 3. 验证同一条 MediaPayload 复用封面缓存
	if second := media.videoCoverBase64(context.Background(), "invalid-on-second-call"); second != coverB64 {
		t.Fatalf("expected cached cover to be reused")
	}
}
