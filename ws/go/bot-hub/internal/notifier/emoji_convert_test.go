package notifier

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEmojiHelpersAndHTTPMediaDownload(t *testing.T) {
	// 1. extractEmojiUploadMeta 兼容平铺与 data 包裹两种回执
	md5a, lena := extractEmojiUploadMeta(json.RawMessage(`{"completed":true,"md5":"abcd1234","totalLen":12345}`))
	if md5a != "abcd1234" || lena != 12345 {
		t.Fatalf("unexpected upload meta: md5=%q len=%d", md5a, lena)
	}
	md5b, lenb := extractEmojiUploadMeta(json.RawMessage(`{"data":{"md5":"ef5678","total_len":9876}}`))
	if md5b != "ef5678" || lenb != 9876 {
		t.Fatalf("unexpected wrapped upload meta: md5=%q len=%d", md5b, lenb)
	}

	// 2. GIF 魔数识别与微信表情免上传直转判断
	gifHeader := base64.StdEncoding.EncodeToString([]byte("GIF89a\x01\x00\x01\x00"))
	if !isGIFData(gifHeader, "unknown.bin", "") {
		t.Fatal("expected GIF89a header to be recognized as gif")
	}
	if !canWechatEmojiForward(&MediaPayload{
		Kind:           "sticker",
		SourcePlatform: "wechat",
		WxEmojiMD5:     "abcd1234",
		WxEmojiLen:     2048,
	}) {
		t.Fatal("expected wechat sticker with md5+len to allow zero-upload forward")
	}

	// 3. 通用 HTTP 媒体下载与大小限流拦截
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.gif":
			w.Header().Set("Content-Type", "image/gif")
			_, _ = w.Write([]byte("GIF89a1234567890"))
		case "/big.bin":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(make([]byte, 2048))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	b64, size, name, mimeType, err := downloadHTTPMedia(context.Background(), srv.URL+"/ok.gif", 1024)
	if err != nil || b64 == "" || size != 16 || name != "ok.gif" || mimeType != "image/gif" {
		t.Fatalf("unexpected http download result: size=%d name=%q mime=%q err=%v", size, name, mimeType, err)
	}

	_, _, _, _, err = downloadHTTPMedia(context.Background(), srv.URL+"/big.bin", 1024)
	var tooLarge *mediaTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected mediaTooLargeError for oversized http media, got %v", err)
	}
}

func TestConvertVideoToEmojiGIF(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("本机未安装 ffmpeg，跳过 MP4 转 GIF 表情测试")
	}

	tmpDir := t.TempDir()
	mp4Path := filepath.Join(tmpDir, "anim.mp4")
	cmd := exec.Command(
		ffmpegPath,
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-f", "lavfi",
		"-i", "color=c=green:s=160x120:d=0.5",
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

	media := &MediaPayload{Kind: "video", IsAnimation: true}
	gifB64, ok := media.emojiGIFBase64(context.Background(), videoB64, "mp4")
	if !ok || gifB64 == "" {
		t.Fatal("expected mp4 to be converted to gif emoji")
	}
	if !isGIFData(gifB64, "", "") {
		t.Fatal("converted data does not have valid GIF magic header")
	}
	// 验证缓存复用
	if cached, ok2 := media.emojiGIFBase64(context.Background(), "invalid", "mp4"); !ok2 || cached != gifB64 {
		t.Fatal("expected cached emoji gif to be reused")
	}
}
