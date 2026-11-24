package notifier

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/config"
)

const (
	defaultMaxMediaBytes int64 = 10 * 1024 * 1024
	hardMaxMediaBytes    int64 = 32 * 1024 * 1024
	cdnDNSTTL                  = 10 * time.Minute
)

// 1x1 黑色合法 JPEG（仅在跨平台发视频到微信且缺少真实封面时兜底使用）。
const fallbackVideoCoverBase64 = "/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/wAALCAABAAEBAREA/8QAFAABAAAAAAAAAAAAAAAAAAAACf/EABQQAQAAAAAAAAAAAAAAAAAAAAD/2gAIAQEAAD8AVN//2Q=="

// MediaOptions 控制单次转发的媒体策略。
type MediaOptions struct {
	ForwardMedia bool   // 是否尝试转发原媒体
	MaxBytes     int64  // 跨平台（需下载/上传）媒体大小上限；<=0 时回退默认 10MB
	FallbackText string // 当 body 为空（纯原样转发模式）且媒体发送失败或超限时的兜底文本
}

func (o MediaOptions) effectiveMaxBytes() int64 {
	if o.MaxBytes <= 0 {
		return defaultMaxMediaBytes
	}
	if o.MaxBytes > hardMaxMediaBytes {
		return hardMaxMediaBytes
	}
	return o.MaxBytes
}

// MediaPayload 表示可随转发消息原样投递的媒体载荷（支持图片/视频/文件/语音/贴纸）。
type MediaPayload struct {
	Kind            string // "image" | "video" | "file" | "voice" | "sticker"
	SourcePlatform  string // "wechat" | "telegram"
	SourceAccountID string // 源账号 ID（微信 wxid 或 TG bot_id）
	SourceChatID    string // 源会话 ID（微信群语音下载使用）
	IsGroup         bool   // 源会话是否群聊

	Base64   string // 内联 Base64（不含 data: 前缀）
	URL      string // 公网 URL（可选）
	TGFileID string // Telegram file_id（仅源为 TG 时有值）
	Filename string
	MimeType string
	Emoji    string // 贴纸对应的 emoji（如 😄），用于动态矢量贴纸降级
	FileSize int64  // 已知文件字节数（0 表示未知）

	// 微信原生 XML 与 CDN/语音下载元数据
	WechatXML       string
	WxFileID        string // 图片中图/普通 CDN ID、视频/文件 CDN fileId
	WxBigFileID     string // 图片原图 cdnbigimgurl（优先尝试）
	WxAesKey        string
	WxMD5           string
	WxRawSize       int64
	WxNewMsgID      string
	WxVoiceBufID    string
	WxVoiceClientID string
	WxVoiceLen      int64
	WxVoiceFormat   int
	WxVoiceTimeMS   int

	// 同一条消息转发至多个跨平台目标时的下载缓存
	cacheMu        sync.Mutex
	downloaded     bool
	cachedBase64   string
	cachedMime     string
	cachedFilename string
	cachedSize     int64
	coverExtracted bool
	cachedCover    string
}

// KnownSize 返回元数据中已知的文件字节数（未知时返回 0）。
func (m *MediaPayload) KnownSize() int64 {
	if m == nil {
		return 0
	}
	if m.FileSize > 0 {
		return m.FileSize
	}
	if m.WxRawSize > 0 {
		return m.WxRawSize
	}
	if m.WxVoiceLen > 0 {
		return m.WxVoiceLen
	}
	if m.Base64 != "" {
		return approxBase64Bytes(m.Base64)
	}
	return 0
}

type mediaTooLargeError struct {
	actual int64
	limit  int64
}

func (e *mediaTooLargeError) Error() string {
	return fmt.Sprintf("媒体大小 %s 超过限制 %s", formatBytes(e.actual), formatBytes(e.limit))
}

// Dispatcher 统一消息转发投递器：通过宿主 WS 连接向微信或 Telegram 发送文本及媒体消息。
type Dispatcher struct {
	mu     sync.RWMutex
	client *lwplugin.Client

	dnsMu    sync.Mutex
	dnsReady map[string]time.Time
}

func New() *Dispatcher {
	return &Dispatcher{
		dnsReady: make(map[string]time.Time),
	}
}

func (d *Dispatcher) SetClient(c *lwplugin.Client) {
	d.mu.Lock()
	d.client = c
	d.mu.Unlock()
}

func (d *Dispatcher) getClient() *lwplugin.Client {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.client
}

// Send 向单个目标投递文本通知（支持微信与 Telegram）。
func (d *Dispatcher) Send(ctx context.Context, target config.NotifyTarget, body string) error {
	return d.SendWithMediaOptions(ctx, target, body, nil, MediaOptions{ForwardMedia: false})
}

// SendWithMediaOptions 按指定媒体策略向单个目标投递文本与原媒体；失败或超限时自动降级为纯文本。
func (d *Dispatcher) SendWithMediaOptions(
	ctx context.Context,
	target config.NotifyTarget,
	body string,
	media *MediaPayload,
	opts MediaOptions,
) error {
	c := d.getClient()
	if c == nil {
		return errors.New("WS 客户端尚未连接宿主")
	}

	activeMedia := media
	if !opts.ForwardMedia {
		activeMedia = nil
	}
	maxBytes := opts.effectiveMaxBytes()
	fallbackBody := firstNonEmpty(body, opts.FallbackText)

	switch strings.ToLower(strings.TrimSpace(target.Type)) {
	case "telegram":
		if target.AccountID == "" || target.To == "" {
			return errors.New("Telegram 转发目标缺少发送账号或目标群/用户 ID")
		}
		if activeMedia != nil {
			err := d.sendMediaToTelegram(ctx, c, target, body, activeMedia, maxBytes)
			if err == nil {
				return nil
			}
			var tooLarge *mediaTooLargeError
			if errors.As(err, &tooLarge) {
				fallbackBody = appendTooLargeNotice(fallbackBody, activeMedia.Kind, tooLarge.actual, tooLarge.limit)
			}
		}
		if strings.TrimSpace(fallbackBody) == "" {
			return nil
		}
		_, err := c.Telegram(target.AccountID).SendMessage(ctx, target.To, fallbackBody, nil)
		return err

	case "wechat":
		if target.AccountID == "" || target.To == "" {
			return errors.New("微信转发目标缺少发送账号或接收目标 ID")
		}
		// 1. 微信 → 微信同平台：图片/视频/文件直接通过 CDN XML 转发，零上传且不受跨平台大小限制
		if activeMedia != nil && canWechatXMLForward(activeMedia) {
			if strings.TrimSpace(body) != "" {
				if err := sendWechatText(ctx, c, target.AccountID, target.To, body); err != nil {
					return err
				}
			}
			if err := d.forwardWechatXML(ctx, c, target, activeMedia); err != nil {
				// 若纯原样转发无前置文本且 XML 转发失败，补发兜底摘要
				if strings.TrimSpace(body) == "" && strings.TrimSpace(fallbackBody) != "" {
					return sendWechatText(ctx, c, target.AccountID, target.To, fallbackBody)
				}
			}
			return nil
		}

		// 2. 跨平台（TG → 微信）或微信语音：先校验大小并准备字节，再决定是否附带超限提示
		if activeMedia != nil {
			b64, filename, mimeType, err := d.prepareMediaBytes(ctx, c, activeMedia, maxBytes)
			if err != nil {
				var tooLarge *mediaTooLargeError
				if errors.As(err, &tooLarge) {
					fallbackBody = appendTooLargeNotice(fallbackBody, activeMedia.Kind, tooLarge.actual, tooLarge.limit)
				}
				if strings.TrimSpace(fallbackBody) == "" {
					return err
				}
				return sendWechatText(ctx, c, target.AccountID, target.To, fallbackBody)
			}
			if strings.TrimSpace(body) != "" {
				if err := sendWechatText(ctx, c, target.AccountID, target.To, body); err != nil {
					return err
				}
			}
			if b64 != "" {
				if err := d.sendDownloadedMediaToWechat(ctx, c, target, activeMedia, b64, filename, mimeType); err != nil {
					// 纯原样转发且媒体发微信失败（例如 .tgs 动态贴纸），用 emoji 或兜底文本补发
					if strings.TrimSpace(body) == "" {
						alt := firstNonEmpty(activeMedia.Emoji, fallbackBody)
						if alt != "" {
							return sendWechatText(ctx, c, target.AccountID, target.To, alt)
						}
					}
				}
			}
			return nil
		}

		if strings.TrimSpace(fallbackBody) == "" {
			return nil
		}
		return sendWechatText(ctx, c, target.AccountID, target.To, fallbackBody)

	default:
		return fmt.Errorf("不支持的转发通道类型: %s（仅支持 wechat / telegram）", target.Type)
	}
}

func canWechatXMLForward(media *MediaPayload) bool {
	if media == nil || media.SourcePlatform != "wechat" || strings.TrimSpace(media.WechatXML) == "" {
		return false
	}
	switch media.Kind {
	case "image", "video", "file":
		return true
	default:
		return false
	}
}

func (d *Dispatcher) forwardWechatXML(ctx context.Context, c *lwplugin.Client, target config.NotifyTarget, media *MediaPayload) error {
	var method string
	switch media.Kind {
	case "image":
		method = "msg.send_cdn_img"
	case "video":
		method = "msg.send_cdn_video"
	case "file":
		method = "msg.send_cdn_file"
	default:
		return errors.New("该媒体类型不支持微信 XML 直转")
	}
	res, err := c.Wechat(target.AccountID).Call(ctx, method, map[string]any{
		"toWxid":  target.To,
		"content": media.WechatXML,
	}, lwplugin.Options{Key: lwplugin.NewKey()})
	if err != nil {
		return err
	}
	if res.Status == "failed" {
		if res.Error != nil {
			return fmt.Errorf("%s: %s", res.Error.Code, res.Error.Message)
		}
		return fmt.Errorf("微信 XML 转发失败 (%s)", method)
	}
	return nil
}

func (d *Dispatcher) sendMediaToTelegram(
	ctx context.Context,
	c *lwplugin.Client,
	target config.NotifyTarget,
	caption string,
	media *MediaPayload,
	maxBytes int64,
) error {
	var payload any
	effectiveKind := media.Kind

	switch {
	case media.TGFileID != "" && media.SourceAccountID == target.AccountID:
		// 同 Bot 内直接复用 file_id，零带宽开销、不限大小
		payload = media.TGFileID
	case media.URL != "" && media.SourcePlatform != "wechat" && media.TGFileID == "" && media.Base64 == "":
		payload = media.URL
	default:
		b64, filename, mimeType, err := d.prepareMediaBytes(ctx, c, media, maxBytes)
		if err != nil {
			return err
		}
		if b64 == "" {
			return errors.New("无可发送的媒体数据")
		}
		if filename == "" {
			filename = defaultFilenameForKind(media.Kind)
		}
		// 微信语音是 silk 格式，Telegram sendVoice 仅支持 ogg/opus，改为 sendDocument 保真送达
		if effectiveKind == "voice" && !isTelegramPlayableVoice(filename, mimeType) {
			effectiveKind = "file"
		}
		payload = lwplugin.TelegramUpload{
			Base64:   b64,
			Filename: filename,
			MimeType: mimeType,
		}
	}

	sendOne := func(extra map[string]any) error {
		tg := c.Telegram(target.AccountID)
		switch effectiveKind {
		case "image":
			_, err := tg.SendPhoto(ctx, target.To, payload, extra)
			return err
		case "video":
			params := cloneMap(extra)
			params["chat_id"] = target.To
			params["video"] = payload
			var raw json.RawMessage
			return tg.Call(ctx, "sendVideo", params, &raw)
		case "voice":
			_, err := tg.SendVoice(ctx, target.To, payload, extra)
			return err
		case "sticker":
			// sendSticker 不接受 caption 参数
			params := map[string]any{
				"chat_id": target.To,
				"sticker": payload,
			}
			var raw json.RawMessage
			return tg.Call(ctx, "sendSticker", params, &raw)
		default:
			_, err := tg.SendDocument(ctx, target.To, payload, extra)
			return err
		}
	}

	// sticker 不支持 caption；若用户开启了模板通知（caption 非空）则先发贴纸再补发文本
	if effectiveKind == "sticker" {
		if err := sendOne(nil); err != nil {
			return err
		}
		if strings.TrimSpace(caption) != "" {
			_, err := c.Telegram(target.AccountID).SendMessage(ctx, target.To, caption, nil)
			return err
		}
		return nil
	}

	// Telegram caption 上限 1024 字符
	if len([]rune(caption)) <= 1000 {
		extra := map[string]any{}
		if caption != "" {
			extra["caption"] = caption
		}
		return sendOne(extra)
	}
	if err := sendOne(nil); err != nil {
		return err
	}
	_, err := c.Telegram(target.AccountID).SendMessage(ctx, target.To, caption, nil)
	return err
}

func (d *Dispatcher) sendDownloadedMediaToWechat(
	ctx context.Context,
	c *lwplugin.Client,
	target config.NotifyTarget,
	media *MediaPayload,
	b64 string,
	filename string,
	mimeType string,
) error {
	if filename == "" {
		filename = defaultFilenameForKind(media.Kind)
	}
	var seg lwplugin.Segment
	switch media.Kind {
	case "image":
		seg = lwplugin.Image(b64)
	case "sticker":
		lowerName := strings.ToLower(strings.TrimSpace(filename))
		lowerMime := strings.ToLower(strings.TrimSpace(mimeType))
		magic := sniffBase64MediaKind(b64)
		switch {
		case magic == "gzip" || strings.HasSuffix(lowerName, ".tgs") || strings.Contains(lowerMime, "tgsticker"):
			// Lottie 矢量动态贴纸（gzip 压缩 JSON）微信无法直接渲染，返回错误触发 emoji / 摘要降级
			return errors.New("Telegram .tgs 矢量贴纸不支持直接转为微信图片")
		case magic == "image":
			// 静态 .webp / .png / .jpg / .gif 贴纸，宿主 lw-cdn 支持直接作为微信图片发送
			seg = lwplugin.Image(b64)
		case magic == "webm" || strings.HasSuffix(lowerName, ".webm") || strings.Contains(lowerMime, "video/"):
			if !strings.Contains(filename, ".") {
				filename = "sticker.webm"
			}
			seg = lwplugin.File(b64, filename)
		default:
			seg = lwplugin.Image(b64)
		}
	case "video":
		seg = lwplugin.Video(b64, media.videoCoverBase64(ctx, b64))
	case "voice":
		if media.SourcePlatform == "wechat" {
			vType := media.WxVoiceFormat
			if vType <= 0 {
				vType = 4
			}
			vTime := media.WxVoiceTimeMS
			if vTime <= 0 {
				vTime = 1000
			}
			seg = lwplugin.Voice(b64, vType, vTime)
		} else {
			// TG 语音为 ogg，微信不支持直接按语音播放，以原文件发送保留完整音频
			seg = lwplugin.File(b64, filename)
		}
	default:
		// Telegram 无声 GIF / 动图可能以无名 document 下发，下载后若确认是 .mp4 则按微信视频发送
		if media.SourcePlatform == "telegram" && isWechatPlayableMP4(filename, mimeType) {
			seg = lwplugin.Video(b64, media.videoCoverBase64(ctx, b64))
		} else {
			seg = lwplugin.File(b64, filename)
		}
	}

	res, err := c.Wechat(target.AccountID).Send(ctx, target.To, []lwplugin.Segment{seg}, lwplugin.Options{
		Key: lwplugin.NewKey(),
	})
	if err != nil {
		return err
	}
	if res.Status == "failed" {
		if res.Error != nil {
			return fmt.Errorf("%s: %s", res.Error.Code, res.Error.Message)
		}
		return errors.New("微信发送媒体失败")
	}
	return nil
}

// prepareMediaBytes 获取媒体原始 Base64（带跨平台大小拦截与单消息多目标缓存）。
func (d *Dispatcher) prepareMediaBytes(
	ctx context.Context,
	c *lwplugin.Client,
	media *MediaPayload,
	maxBytes int64,
) (string, string, string, error) {
	if media == nil {
		return "", "", "", errors.New("空媒体载荷")
	}

	// 事前大小检查：元数据已知超限时直接拦截，绝不发起下载
	if known := media.KnownSize(); known > 0 && known > maxBytes {
		return "", "", "", &mediaTooLargeError{actual: known, limit: maxBytes}
	}

	media.cacheMu.Lock()
	defer media.cacheMu.Unlock()

	if media.downloaded {
		if media.cachedSize > maxBytes {
			return "", "", "", &mediaTooLargeError{actual: media.cachedSize, limit: maxBytes}
		}
		return media.cachedBase64, media.cachedFilename, media.cachedMime, nil
	}

	// 1. 已有内联 Base64
	if media.Base64 != "" {
		size := approxBase64Bytes(media.Base64)
		media.downloaded = true
		media.cachedBase64 = media.Base64
		media.cachedFilename = firstNonEmpty(media.Filename, defaultFilenameForKind(media.Kind))
		media.cachedMime = media.MimeType
		media.cachedSize = size
		if size > maxBytes {
			return "", "", "", &mediaTooLargeError{actual: size, limit: maxBytes}
		}
		return media.cachedBase64, media.cachedFilename, media.cachedMime, nil
	}

	// 2. 来源为 Telegram：通过源 Bot 下载
	if media.TGFileID != "" && media.SourceAccountID != "" {
		dl, err := c.Telegram(media.SourceAccountID).DownloadFile(ctx, media.TGFileID)
		if err != nil || dl.Base64 == "" {
			if err == nil {
				err = errors.New("下载源 TG 媒体返回空内容")
			}
			return "", "", "", err
		}
		size := dl.FileSize
		if size <= 0 {
			size = approxBase64Bytes(dl.Base64)
		}
		name := firstNonEmpty(media.Filename, baseNameFromPath(dl.FilePath), defaultFilenameForKind(media.Kind))
		mime := firstNonEmpty(dl.MimeType, media.MimeType)
		media.downloaded = true
		media.cachedBase64 = dl.Base64
		media.cachedFilename = name
		media.cachedMime = mime
		media.cachedSize = size
		if size > maxBytes {
			return "", "", "", &mediaTooLargeError{actual: size, limit: maxBytes}
		}
		return media.cachedBase64, media.cachedFilename, media.cachedMime, nil
	}

	// 3. 来源为微信：按媒体类型调用 CDN / 语音下载接口
	if media.SourcePlatform == "wechat" && media.SourceAccountID != "" {
		b64, size, err := d.downloadWechatMedia(ctx, c, media)
		if err != nil {
			return "", "", "", err
		}
		if size <= 0 {
			size = approxBase64Bytes(b64)
		}
		name := firstNonEmpty(media.Filename, defaultFilenameForKind(media.Kind))
		media.downloaded = true
		media.cachedBase64 = b64
		media.cachedFilename = name
		media.cachedMime = media.MimeType
		media.cachedSize = size
		if size > maxBytes {
			return "", "", "", &mediaTooLargeError{actual: size, limit: maxBytes}
		}
		return media.cachedBase64, media.cachedFilename, media.cachedMime, nil
	}

	return "", "", "", errors.New("缺少可下载的媒体来源信息")
}

func (d *Dispatcher) ensureWechatCDNDNS(ctx context.Context, c *lwplugin.Client, accountID string) {
	d.dnsMu.Lock()
	if t, ok := d.dnsReady[accountID]; ok && time.Since(t) < cdnDNSTTL {
		d.dnsMu.Unlock()
		return
	}
	d.dnsMu.Unlock()

	res, err := c.Wechat(accountID).Call(ctx, "cdn.get_dns", map[string]any{}, lwplugin.Options{Key: lwplugin.NewKey()})
	if err == nil && res.Status != "failed" {
		d.dnsMu.Lock()
		d.dnsReady[accountID] = time.Now()
		d.dnsMu.Unlock()
	}
}

func (d *Dispatcher) downloadWechatMedia(ctx context.Context, c *lwplugin.Client, media *MediaPayload) (string, int64, error) {
	wxChatType := 0
	if media.IsGroup {
		wxChatType = 1
	}

	switch media.Kind {
	case "image":
		if media.WxAesKey == "" || (media.WxFileID == "" && media.WxBigFileID == "") {
			return "", 0, errors.New("微信图片缺少 CDN 下载参数")
		}
		d.ensureWechatCDNDNS(ctx, c, media.SourceAccountID)
		// 优先下载原图（fileType=1），无原图或失败时回退中图（fileType=2）
		type attempt struct {
			fileNo   string
			fileType int
		}
		var attempts []attempt
		if media.WxBigFileID != "" {
			attempts = append(attempts, attempt{fileNo: media.WxBigFileID, fileType: 1})
		}
		if media.WxFileID != "" {
			attempts = append(attempts, attempt{fileNo: media.WxFileID, fileType: 2})
		}
		var lastErr error
		for _, a := range attempts {
			res, err := c.Wechat(media.SourceAccountID).Call(ctx, "cdn.download_image", map[string]any{
				"fileNo":     a.fileNo,
				"fileAesKey": media.WxAesKey,
				"fileType":   a.fileType,
				"wxChatType": wxChatType,
			}, lwplugin.Options{Key: lwplugin.NewKey()})
			if err != nil {
				lastErr = err
				continue
			}
			if res.Status == "failed" {
				if res.Error != nil {
					lastErr = fmt.Errorf("%s: %s", res.Error.Code, res.Error.Message)
				}
				continue
			}
			if b64, size := extractDownloadedBase64(res.Data); b64 != "" {
				return b64, size, nil
			}
		}
		if lastErr == nil {
			lastErr = errors.New("下载微信图片返回空数据")
		}
		return "", 0, lastErr

	case "video", "file":
		if media.WxFileID == "" || media.WxAesKey == "" || media.WxRawSize <= 0 {
			return "", 0, errors.New("微信视频/文件缺少 CDN 下载参数")
		}
		d.ensureWechatCDNDNS(ctx, c, media.SourceAccountID)
		params := map[string]any{
			"fileId":     media.WxFileID,
			"aesKey":     media.WxAesKey,
			"mediaType":  media.Kind,
			"rawSize":    media.WxRawSize,
			"wxChatType": wxChatType,
		}
		if media.WxMD5 != "" {
			params["md5"] = media.WxMD5
		}
		res, err := c.Wechat(media.SourceAccountID).Call(ctx, "cdn.download_media", params, lwplugin.Options{Key: lwplugin.NewKey()})
		if err != nil {
			return "", 0, err
		}
		if res.Status == "failed" {
			if res.Error != nil {
				return "", 0, fmt.Errorf("%s: %s", res.Error.Code, res.Error.Message)
			}
			return "", 0, errors.New("下载微信媒体失败")
		}
		b64, size := extractDownloadedBase64(res.Data)
		if b64 == "" {
			return "", 0, errors.New("下载微信媒体返回空数据")
		}
		return b64, size, nil

	case "voice":
		if media.WxNewMsgID == "" {
			return "", 0, errors.New("微信语音缺少 newMsgId")
		}
		params := map[string]any{
			"newMsgId": media.WxNewMsgID,
		}
		if media.IsGroup && media.SourceChatID != "" {
			params["chatRoomId"] = media.SourceChatID
		}
		if media.WxVoiceBufID != "" {
			params["masterBufId"] = media.WxVoiceBufID
		}
		if media.WxVoiceClientID != "" {
			params["clientMsgId"] = media.WxVoiceClientID
		}
		if media.WxVoiceLen > 0 {
			params["totalLen"] = media.WxVoiceLen
		}
		res, err := c.Wechat(media.SourceAccountID).Call(ctx, "msg.download_voice", params, lwplugin.Options{Key: lwplugin.NewKey()})
		if err != nil {
			return "", 0, err
		}
		if res.Status == "failed" {
			if res.Error != nil {
				return "", 0, fmt.Errorf("%s: %s", res.Error.Code, res.Error.Message)
			}
			return "", 0, errors.New("下载微信语音失败")
		}
		b64, size := extractDownloadedBase64(res.Data)
		if b64 == "" {
			return "", 0, errors.New("下载微信语音返回空数据")
		}
		return b64, size, nil
	}

	return "", 0, errors.New("不支持的微信媒体类型")
}

func extractDownloadedBase64(raw json.RawMessage) (string, int64) {
	if len(raw) == 0 {
		return "", 0
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", 0
	}
	// 兼容可能外包一层 data 对象的情况
	if inner, ok := m["data"].(map[string]any); ok {
		m = inner
	}
	var b64 string
	for _, k := range []string{"base64", "image", "imgBase64", "data"} {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			b64 = strings.TrimSpace(s)
			break
		}
	}
	if idx := strings.Index(b64, ";base64,"); idx > 0 {
		b64 = b64[idx+len(";base64,"):]
	}
	var size int64
	if v, ok := m["size"].(float64); ok && v > 0 {
		size = int64(v)
	}
	return b64, size
}

func sendWechatText(ctx context.Context, c *lwplugin.Client, accountID, to, body string) error {
	res, err := c.Wechat(accountID).SendText(ctx, to, body, lwplugin.Options{
		Key: lwplugin.NewKey(),
	})
	if err != nil {
		return err
	}
	if res.Status == "failed" {
		if res.Error != nil {
			return fmt.Errorf("微信发送失败 (%s): %s", res.Error.Code, res.Error.Message)
		}
		return errors.New("微信发送失败")
	}
	return nil
}

func appendTooLargeNotice(body, kind string, actual, limit int64) string {
	label := mediaKindLabel(kind)
	notice := fmt.Sprintf("⚠️ 原%s（%s）超过 %s 跨平台大小限制，已仅转发文字摘要", label, formatBytes(actual), formatBytes(limit))
	if strings.TrimSpace(body) == "" {
		return notice
	}
	return body + "\n" + notice
}

func mediaKindLabel(kind string) string {
	switch kind {
	case "image":
		return "图片"
	case "video":
		return "视频"
	case "voice":
		return "语音"
	case "file":
		return "文件"
	case "sticker":
		return "贴纸"
	default:
		return "媒体"
	}
}

func defaultFilenameForKind(kind string) string {
	switch kind {
	case "image":
		return "image.jpg"
	case "video":
		return "video.mp4"
	case "voice":
		return "voice.silk"
	case "sticker":
		return "sticker.webp"
	default:
		return "file.bin"
	}
}

func isWechatPlayableMP4(filename, mimeType string) bool {
	if strings.EqualFold(strings.TrimSpace(mimeType), "video/mp4") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(filename)), ".mp4")
}

func isTelegramPlayableVoice(filename, mimeType string) bool {
	m := strings.ToLower(strings.TrimSpace(mimeType))
	if strings.Contains(m, "ogg") || strings.Contains(m, "opus") {
		return true
	}
	n := strings.ToLower(strings.TrimSpace(filename))
	return strings.HasSuffix(n, ".ogg") || strings.HasSuffix(n, ".oga") || strings.HasSuffix(n, ".opus")
}

func sniffBase64MediaKind(b64 string) string {
	s := strings.TrimSpace(b64)
	if idx := strings.Index(s, ";base64,"); idx > 0 {
		s = s[idx+len(";base64,"):]
	}
	if len(s) > 64 {
		s = s[:64]
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
	}
	if err != nil || len(raw) < 4 {
		return ""
	}
	// GZIP (.tgs Lottie 矢量动图)
	if raw[0] == 0x1F && raw[1] == 0x8B {
		return "gzip"
	}
	// WebM / Matroska (.webm 视频贴纸)
	if raw[0] == 0x1A && raw[1] == 0x45 && raw[2] == 0xDF && raw[3] == 0xA3 {
		return "webm"
	}
	// JPEG / PNG / GIF / WebP
	if raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF {
		return "image"
	}
	if raw[0] == 0x89 && raw[1] == 0x50 && raw[2] == 0x4E && raw[3] == 0x47 {
		return "image"
	}
	if raw[0] == 'G' && raw[1] == 'I' && raw[2] == 'F' && raw[3] == '8' {
		return "image"
	}
	if len(raw) >= 12 && string(raw[0:4]) == "RIFF" && string(raw[8:12]) == "WEBP" {
		return "image"
	}
	return ""
}

func approxBase64Bytes(b64 string) int64 {
	s := strings.TrimSpace(b64)
	if idx := strings.Index(s, ";base64,"); idx > 0 {
		s = s[idx+len(";base64,"):]
	}
	if len(s) == 0 {
		return 0
	}
	if dec, err := base64.StdEncoding.DecodeString(s); err == nil {
		return int64(len(dec))
	}
	return int64(len(s)) * 3 / 4
}

func formatBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * 1024
	)
	if n >= mb {
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	}
	if n >= kb {
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	}
	return fmt.Sprintf("%d B", n)
}

func baseNameFromPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 && i+1 < len(p) {
		return p[i+1:]
	}
	return p
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func cloneMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src)+4)
	for k, v := range src {
		out[k] = v
	}
	return out
}
