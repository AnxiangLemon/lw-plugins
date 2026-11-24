package lwplugin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Telegram 将 Telegram Bot API 调用绑定到指定 Bot 账号。
// 与 Wechat 异步持久化队列不同，Telegram 调用直接通过宿主转发并同步返回 Bot API 的 result。
type Telegram struct {
	client    Caller
	AccountID string
}

// NewTelegram 可接受真实 Client 或测试用 Caller。
func NewTelegram(client Caller, account string) Telegram {
	return Telegram{client: client, AccountID: account}
}

// Telegram 返回当前 Bot 账号的 Telegram 调用句柄；不会建立额外连接。
func (c *Client) Telegram(account string) Telegram {
	return Telegram{client: c, AccountID: account}
}

// TelegramUpload 表示随 WS 协议发送的内联文件上传对象（远程安全，不传宿主本地路径）。
type TelegramUpload struct {
	Base64   string `json:"base64"`
	Filename string `json:"filename,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

// TelegramUploadBytes 从内存字节构造内联上传对象。
func TelegramUploadBytes(data []byte, filename, mimeType string) TelegramUpload {
	return TelegramUpload{
		Base64:   base64.StdEncoding.EncodeToString(data),
		Filename: filename,
		MimeType: mimeType,
	}
}

// TelegramUploadFile 读取插件本机文件并编码为内联上传对象（绝不将本地路径发送给宿主）。
func TelegramUploadFile(path, mimeType string) (TelegramUpload, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TelegramUpload{}, err
	}
	return TelegramUploadBytes(data, filepath.Base(path), mimeType), nil
}

// TelegramDownloadedFile 为 telegram.download_file 的返回结果。
type TelegramDownloadedFile struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
	MimeType string `json:"mime_type"`
	Base64   string `json:"base64"`
}

// Bytes 解码下载到的文件原始字节。
func (f TelegramDownloadedFile) Bytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(f.Base64)
}

// Call 调用任意 Telegram Bot API 方法（支持 camelCase 或 snake_case）。
// 参数中任意顶层媒体字段或 media 数组项内的 media/thumbnail 传入 TelegramUpload 时，
// 宿主会自动切换为 multipart/form-data 上传。
func (t Telegram) Call(ctx context.Context, method string, params any, out any) error {
	if strings.TrimSpace(t.AccountID) == "" {
		return errors.New("account_id 不能为空")
	}
	if strings.TrimSpace(method) == "" {
		return errors.New("method 不能为空")
	}
	if params == nil {
		params = map[string]any{}
	}
	req := map[string]any{
		"account_id": t.AccountID,
		"method":     method,
		"params":     params,
	}
	return t.client.Request(ctx, "telegram.call", req, out)
}

// DownloadFile 按 file_id 下载 Telegram 文件（适用于远程 WS 插件）。
func (t Telegram) DownloadFile(ctx context.Context, fileID string) (TelegramDownloadedFile, error) {
	var file TelegramDownloadedFile
	if strings.TrimSpace(t.AccountID) == "" {
		return file, errors.New("account_id 不能为空")
	}
	if strings.TrimSpace(fileID) == "" {
		return file, errors.New("file_id 不能为空")
	}
	err := t.client.Request(ctx, "telegram.download_file", map[string]any{
		"account_id": t.AccountID,
		"file_id":    fileID,
	}, &file)
	return file, err
}

// SendMessage 发送文本消息（extra 可选传 parse_mode、reply_markup、reply_parameters 等官方字段）。
func (t Telegram) SendMessage(ctx context.Context, chatID any, text string, extra map[string]any) (json.RawMessage, error) {
	params := cloneMap(extra)
	params["chat_id"] = chatID
	params["text"] = text
	var raw json.RawMessage
	err := t.Call(ctx, "sendMessage", params, &raw)
	return raw, err
}

// SendPhoto 发送图片（photo 可传 file_id、公网 URL 或 TelegramUpload）。
func (t Telegram) SendPhoto(ctx context.Context, chatID any, photo any, extra map[string]any) (json.RawMessage, error) {
	params := cloneMap(extra)
	params["chat_id"] = chatID
	params["photo"] = photo
	var raw json.RawMessage
	err := t.Call(ctx, "sendPhoto", params, &raw)
	return raw, err
}

// SendDocument 发送文件（document 可传 file_id、公网 URL 或 TelegramUpload）。
func (t Telegram) SendDocument(ctx context.Context, chatID any, document any, extra map[string]any) (json.RawMessage, error) {
	params := cloneMap(extra)
	params["chat_id"] = chatID
	params["document"] = document
	var raw json.RawMessage
	err := t.Call(ctx, "sendDocument", params, &raw)
	return raw, err
}

// SendVoice 发送语音（voice 可传 file_id、公网 URL 或 TelegramUpload）。
func (t Telegram) SendVoice(ctx context.Context, chatID any, voice any, extra map[string]any) (json.RawMessage, error) {
	params := cloneMap(extra)
	params["chat_id"] = chatID
	params["voice"] = voice
	var raw json.RawMessage
	err := t.Call(ctx, "sendVoice", params, &raw)
	return raw, err
}

// AnswerCallbackQuery 应答内联按钮点击，消除按钮加载转圈（可选弹提示或弹窗）。
func (t Telegram) AnswerCallbackQuery(ctx context.Context, callbackQueryID, text string, showAlert bool) error {
	params := map[string]any{
		"callback_query_id": callbackQueryID,
	}
	if text != "" {
		params["text"] = text
	}
	if showAlert {
		params["show_alert"] = true
	}
	return t.Call(ctx, "answerCallbackQuery", params, nil)
}

// EditMessageText 就地修改消息文本与内联键盘。
func (t Telegram) EditMessageText(ctx context.Context, chatID any, messageID int64, text string, extra map[string]any) (json.RawMessage, error) {
	params := cloneMap(extra)
	params["chat_id"] = chatID
	params["message_id"] = messageID
	params["text"] = text
	var raw json.RawMessage
	err := t.Call(ctx, "editMessageText", params, &raw)
	return raw, err
}

// DeleteMessage 删除指定消息。
func (t Telegram) DeleteMessage(ctx context.Context, chatID any, messageID int64) error {
	return t.Call(ctx, "deleteMessage", map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	}, nil)
}

// SetMyCommands 设置左下角斜杠命令菜单。
func (t Telegram) SetMyCommands(ctx context.Context, commands any, extra map[string]any) error {
	params := cloneMap(extra)
	params["commands"] = commands
	return t.Call(ctx, "setMyCommands", params, nil)
}

// RestrictChatMember 限制/解禁群成员权限。
func (t Telegram) RestrictChatMember(ctx context.Context, chatID any, userID int64, permissions map[string]any, untilDate int64) error {
	params := map[string]any{
		"chat_id":     chatID,
		"user_id":     userID,
		"permissions": permissions,
	}
	if untilDate > 0 {
		params["until_date"] = untilDate
	}
	return t.Call(ctx, "restrictChatMember", params, nil)
}

// BanChatMember 移出/封禁群成员。
func (t Telegram) BanChatMember(ctx context.Context, chatID any, userID int64, untilDate int64, revokeMessages bool) error {
	params := map[string]any{
		"chat_id": chatID,
		"user_id": userID,
	}
	if untilDate > 0 {
		params["until_date"] = untilDate
	}
	if revokeMessages {
		params["revoke_messages"] = true
	}
	return t.Call(ctx, "banChatMember", params, nil)
}

// UnbanChatMember 解除封禁（onlyIfBanned=true 时不会误踢仍在群里的成员）。
func (t Telegram) UnbanChatMember(ctx context.Context, chatID any, userID int64, onlyIfBanned bool) error {
	return t.Call(ctx, "unbanChatMember", map[string]any{
		"chat_id":        chatID,
		"user_id":        userID,
		"only_if_banned": onlyIfBanned,
	}, nil)
}

// ApproveChatJoinRequest 同意进群申请。
func (t Telegram) ApproveChatJoinRequest(ctx context.Context, chatID any, userID int64) error {
	return t.Call(ctx, "approveChatJoinRequest", map[string]any{
		"chat_id": chatID,
		"user_id": userID,
	}, nil)
}

// DeclineChatJoinRequest 拒绝进群申请。
func (t Telegram) DeclineChatJoinRequest(ctx context.Context, chatID any, userID int64) error {
	return t.Call(ctx, "declineChatJoinRequest", map[string]any{
		"chat_id": chatID,
		"user_id": userID,
	}, nil)
}

// GetChatAdministrators 获取群管理员列表。
func (t Telegram) GetChatAdministrators(ctx context.Context, chatID any) (json.RawMessage, error) {
	var raw json.RawMessage
	err := t.Call(ctx, "getChatAdministrators", map[string]any{"chat_id": chatID}, &raw)
	return raw, err
}

func cloneMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src)+4)
	for k, v := range src {
		out[k] = v
	}
	return out
}
