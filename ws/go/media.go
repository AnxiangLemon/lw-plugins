package lwplugin

import (
	"encoding/base64"
	"errors"
	"os"
)

// At 提及一个目标；是否支持 @all 等特殊 ID 由当前平台决定。
func At(targetID string) Segment {
	return Segment{Type: "at", Data: map[string]string{"target_id": targetID}}
}

// Image 接受已经编码的原始图片字节，不是文件路径或 URL。
func Image(encoded string) Segment {
	return Segment{Type: "image", Data: map[string]any{"source": map[string]string{"kind": "base64", "base64": encoded}}}
}

// ImageFile 读取插件本机文件，保持原始字节，不压缩或转码，也不发起网络请求。
func ImageFile(path string) (Segment, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Segment{}, err
	}
	if len(content) == 0 {
		return Segment{}, errors.New("图片文件为空")
	}
	return Image(base64.StdEncoding.EncodeToString(content)), nil
}

// Voice 的语音编码类型与时长单位遵循当前服务文档，SDK 不猜测文件格式。
func Voice(encoded string, voiceType, voiceTime int) Segment {
	return Segment{Type: "voice", Data: map[string]any{"base64": encoded, "type": voiceType, "voiceTime": voiceTime}}
}

// Video 传视频及封面 Base64；时长由服务读取，不再提交 playLength。
func Video(encoded, cover string) Segment {
	return Segment{Type: "video", Data: map[string]string{"base64": encoded, "imageBase64": cover}}
}

// File 传文件内容和展示文件名，不能用宿主路径代替内容。
func File(encoded, name string) Segment {
	return Segment{Type: "file", Data: map[string]string{"base64": encoded, "fileName": name}}
}

// WechatSegment 封装高级微信消息方法；方法可用性和参数由宿主校验。
func WechatSegment(method string, params any) Segment {
	if params == nil {
		params = map[string]any{}
	}
	return Segment{Type: "wechat", Data: map[string]any{"method": method, "params": params}}
}
