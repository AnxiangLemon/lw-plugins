package monitor

import (
	"encoding/json"
	"strings"
	"testing"

	lwplugin "github.com/AnxiangLemon/lw-plugins/ws/go"
	"github.com/AnxiangLemon/lw-plugins/ws/go/bot-hub/internal/config"
)

func TestEvaluateRuleAndCooldownIsolation(t *testing.T) {
	groups := map[string]config.MentionGroup{
		"ops": {
			ID:      "ops",
			Name:    "运维组",
			Members: []string{"@alice", "10086", "wxid_bob"},
		},
	}
	rule := config.MonitorRule{
		ID:              "r1",
		Name:            "值班告警",
		Enabled:         true,
		WatchMentions:   []string{"@boss"},
		WatchGroupIDs:   []string{"ops"},
		Keywords:        []string{"#ops"},
		CooldownSeconds: 300,
		CooldownScope:   "target",
	}

	hits := evaluateRule(rule, groups, []string{"@alice"}, "请 @alice 看一下线上报错")
	if len(hits) != 1 || hits[0] != "组:运维组" {
		t.Fatalf("unexpected group hit: %v", hits)
	}

	hits2 := evaluateRule(rule, groups, nil, "紧急情况 #ops 和 @boss 请看")
	if len(hits2) != 2 {
		t.Fatalf("expected 2 hits, got %v", hits2)
	}

	eng := &Engine{cooldowns: make(map[string]*cooldownState)}
	k1 := buildCooldownKey(rule, "-1001", "组:运维组")
	k2 := buildCooldownKey(rule, "-1002", "组:运维组")
	k3 := buildCooldownKey(rule, "-1001", "@boss")

	if _, ok := eng.checkCooldown(k1, 300); !ok {
		t.Fatal("first trigger must pass")
	}
	if suppressed, ok := eng.checkCooldown(k1, 300); ok || suppressed != 1 {
		t.Fatalf("second trigger in window must be suppressed, ok=%v suppressed=%d", ok, suppressed)
	}
	if _, ok := eng.checkCooldown(k2, 300); !ok {
		t.Fatal("different chat should not be blocked by k1")
	}
	if _, ok := eng.checkCooldown(k3, 300); !ok {
		t.Fatal("different target in same chat should not be blocked by k1")
	}
}

func TestMessageTypesAndMatchAll(t *testing.T) {
	imgMsg := &lwplugin.Message{
		Text: "现场截图",
		Segments: []lwplugin.Segment{
			{Type: "image", Data: map[string]any{"file_id": "AgAC123"}},
			{Type: "text", Data: map[string]any{"text": "现场截图"}},
		},
	}
	types := detectMessageTypes(imgMsg, nil)
	if !matchesMessageTypes([]string{"image"}, types) {
		t.Fatalf("expected image type match, got %v", types)
	}
	if matchesMessageTypes([]string{"file"}, types) {
		t.Fatalf("file filter should not match image+text: %v", types)
	}
	if got := buildContentSummary(imgMsg, types, nil); got != "[图片] 现场截图" {
		t.Fatalf("unexpected summary: %q", got)
	}

	allRule := config.MonitorRule{
		ID:               "r_all",
		Name:             "同平台全量同步",
		Enabled:          true,
		MatchAllMessages: true,
	}
	hits := evaluateRule(allRule, nil, nil, "")
	if len(hits) != 1 || hits[0] != "全部消息" {
		t.Fatalf("expected match all hit, got %v", hits)
	}
}

func TestWechatXMLParsingAndMediaExtraction(t *testing.T) {
	// 1. 微信图片 XML
	imgXML := `<msg><img aeskey="0123456789abcdef0123456789abcdef" cdnmidimgurl="mid_file_no" cdnbigimgurl="big_file_no" length="120000" hdlength="5242880" md5="abc123" /></msg>`
	imgRaw, _ := json.Marshal(map[string]any{
		"msg_type": 3,
		"content":  imgXML,
	})
	wxImg := &lwplugin.Message{
		Platform: "wechat",
		ChatType: "group",
		Segments: []lwplugin.Segment{{Type: "image"}},
		Raw:      imgRaw,
	}
	metaImg := parseWechatRawMeta(wxImg, "wechat")
	mediaImg := extractMediaPayload(wxImg, "wechat", "wxid_bot", "123@chatroom", metaImg)
	if mediaImg == nil || mediaImg.Kind != "image" || mediaImg.WechatXML != imgXML || mediaImg.WxBigFileID != "big_file_no" || mediaImg.FileSize != 5242880 {
		t.Fatalf("unexpected wechat image media: %+v", mediaImg)
	}

	// 2. 微信视频 XML
	videoXML := `<msg><videomsg aeskey="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" cdnvideourl="video_file_id" length="15728640" playlength="18" md5="vidmd5" /></msg>`
	videoRaw, _ := json.Marshal(map[string]any{
		"msg_type": 43,
		"content":  videoXML,
	})
	wxVideo := &lwplugin.Message{
		Platform: "wechat",
		ChatType: "group",
		Segments: []lwplugin.Segment{{Type: "video"}},
		Raw:      videoRaw,
	}
	metaVideo := parseWechatRawMeta(wxVideo, "wechat")
	typesVideo := detectMessageTypes(wxVideo, metaVideo)
	summaryVideo := buildContentSummary(wxVideo, typesVideo, metaVideo)
	if !strings.Contains(summaryVideo, "18s") || !strings.Contains(summaryVideo, "15.0 MB") {
		t.Fatalf("unexpected video summary: %q", summaryVideo)
	}
	mediaVideo := extractMediaPayload(wxVideo, "wechat", "wxid_bot", "123@chatroom", metaVideo)
	if mediaVideo == nil || mediaVideo.Kind != "video" || mediaVideo.WxFileID != "video_file_id" || mediaVideo.WxRawSize != 15728640 {
		t.Fatalf("unexpected wechat video media: %+v", mediaVideo)
	}

	// 3. 微信文件 AppMsg (msg_type=49, type=6)
	fileXML := `<msg><appmsg><title>报价单.pdf</title><type>6</type><md5>filemd5</md5><appattach><totallen>2097152</totallen><cdnattachurl>file_cdn_id</cdnattachurl><aeskey>bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb</aeskey><fileext>pdf</fileext></appattach></appmsg></msg>`
	fileRaw, _ := json.Marshal(map[string]any{
		"msg_type": 49,
		"content":  fileXML,
	})
	wxFile := &lwplugin.Message{
		Platform: "wechat",
		ChatType: "group",
		Segments: []lwplugin.Segment{{Type: "link", Data: map[string]any{"title": fileXML}}},
		Raw:      fileRaw,
	}
	metaFile := parseWechatRawMeta(wxFile, "wechat")
	typesFile := detectMessageTypes(wxFile, metaFile)
	if len(typesFile) != 1 || typesFile[0] != "file" {
		t.Fatalf("expected appmsg type=6 to be classified as file, got %v", typesFile)
	}
	summaryFile := buildContentSummary(wxFile, typesFile, metaFile)
	if summaryFile != "[文件: 报价单.pdf (2.0 MB)]" {
		t.Fatalf("unexpected file summary: %q", summaryFile)
	}
	mediaFile := extractMediaPayload(wxFile, "wechat", "wxid_bot", "123@chatroom", metaFile)
	if mediaFile == nil || mediaFile.Kind != "file" || mediaFile.Filename != "报价单.pdf" || mediaFile.WxFileID != "file_cdn_id" || mediaFile.WechatXML != fileXML {
		t.Fatalf("unexpected wechat file media: %+v", mediaFile)
	}

	// 4. Telegram 贴纸提取与摘要
	tgSticker := &lwplugin.Message{
		Platform: "telegram",
		ChatType: "group",
		Segments: []lwplugin.Segment{
			{
				Type: "custom",
				Data: map[string]any{
					"platform":    "telegram",
					"custom_type": "sticker",
					"extra": map[string]any{
						"file_id": "CAACAgUAAxkBAA",
						"emoji":   "😄",
					},
				},
			},
		},
	}
	typesSticker := detectMessageTypes(tgSticker, nil)
	if got := buildContentSummary(tgSticker, typesSticker, nil); got != "[贴纸 😄]" {
		t.Fatalf("unexpected sticker summary: %q", got)
	}
	mediaSticker := extractMediaPayload(tgSticker, "telegram", "bot_1", "-100123", nil)
	if mediaSticker == nil || mediaSticker.Kind != "sticker" || mediaSticker.TGFileID != "CAACAgUAAxkBAA" || mediaSticker.Emoji != "😄" {
		t.Fatalf("unexpected tg sticker media: %+v", mediaSticker)
	}

	// 5. Telegram 无声 GIF/动图（落入 document/.mp4）在群管家侧自动按视频识别与转发
	tgGifDoc := &lwplugin.Message{
		Platform: "telegram",
		ChatType: "group",
		Text:     "[文件: photo_2022-08-28_18-17-05.mp4]",
		Segments: []lwplugin.Segment{
			{
				Type: "file",
				Data: map[string]any{
					"file_id": "CgACAgUAAxkBAA",
					"name":    "photo_2022-08-28_18-17-05.mp4",
					"size":    float64(1363148),
				},
			},
		},
	}
	typesGif := detectMessageTypes(tgGifDoc, nil)
	if len(typesGif) != 1 || typesGif[0] != "video" {
		t.Fatalf("expected telegram mp4 document to be classified as video, got %v", typesGif)
	}
	if got := buildContentSummary(tgGifDoc, typesGif, nil); got != "[视频: photo_2022-08-28_18-17-05.mp4 (1.3 MB)]" {
		t.Fatalf("unexpected telegram mp4 summary: %q", got)
	}
	if got := cleanRawCaption(tgGifDoc); got != "" {
		t.Fatalf("expected auto media placeholder caption to be cleared, got %q", got)
	}
	mediaGif := extractMediaPayload(tgGifDoc, "telegram", "bot_1", "-100123", nil)
	if mediaGif == nil || mediaGif.Kind != "video" || mediaGif.MimeType != "video/mp4" || mediaGif.TGFileID != "CgACAgUAAxkBAA" {
		t.Fatalf("unexpected telegram mp4 media payload: %+v", mediaGif)
	}

	// 6. Telegram 普通非视频文件保持 file 类型
	tgPdfDoc := &lwplugin.Message{
		Platform: "telegram",
		ChatType: "group",
		Segments: []lwplugin.Segment{
			{
				Type: "file",
				Data: map[string]any{
					"file_id": "BQACAgUAAxkBAA",
					"name":    "report.pdf",
					"size":    float64(204800),
				},
			},
		},
	}
	typesPdf := detectMessageTypes(tgPdfDoc, nil)
	if len(typesPdf) != 1 || typesPdf[0] != "file" {
		t.Fatalf("expected telegram pdf document to remain file, got %v", typesPdf)
	}
	mediaPdf := extractMediaPayload(tgPdfDoc, "telegram", "bot_1", "-100123", nil)
	if mediaPdf == nil || mediaPdf.Kind != "file" {
		t.Fatalf("unexpected telegram pdf media payload: %+v", mediaPdf)
	}
}

func TestShouldExcludeMessage(t *testing.T) {
	rule := config.MonitorRule{
		IgnoreCommands:  true,
		IgnoreBots:      true,
		ExcludeKeywords: []string{"@LemonQuantBot", "Bot Hub", "^!test"},
	}

	if !shouldExcludeMessage(rule, "/warn@LemonQuantBot", "/warn@LemonQuantBot", false) {
		t.Fatal("command starting with / should be excluded")
	}
	if !shouldExcludeMessage(rule, "正常消息", "正常消息", true) {
		t.Fatal("bot sender message should be excluded when IgnoreBots=true")
	}
	if !shouldExcludeMessage(rule, "请看 @lemonquantbot 的回复", "请看 @lemonquantbot 的回复", false) {
		t.Fatal("case-insensitive exclude keyword should match")
	}
	if !shouldExcludeMessage(rule, "!TEST 123", "!TEST 123", false) {
		t.Fatal("prefix exclude keyword (^!test) should match")
	}
	if shouldExcludeMessage(rule, "这不是 !test 开头", "这不是 !test 开头", false) {
		t.Fatal("prefix exclude keyword (^!test) should not match middle of text")
	}
	if shouldExcludeMessage(rule, "大家好，今天正常讨论", "大家好，今天正常讨论", false) {
		t.Fatal("normal message should not be excluded")
	}
}

func TestTelegramMentionDoesNotDuplicateTrailingAt(t *testing.T) {
	// 宿主拼接 msg.Text 时会把 Telegram 附带的 At 段再拼一次（形成末尾 @@snakehyc）
	tgMentionMsg := &lwplugin.Message{
		Platform: "telegram",
		ChatType: "group",
		Text:     "@snakehyc 服务器宕机了 @@snakehyc",
		Segments: []lwplugin.Segment{
			{Type: "text", Data: map[string]any{"text": "@snakehyc 服务器宕机了"}},
			{Type: "at", Data: map[string]any{"target_id": "@snakehyc"}},
		},
	}
	types := detectMessageTypes(tgMentionMsg, nil)
	got := buildContentSummary(tgMentionMsg, types, nil)
	if got != "@snakehyc 服务器宕机了" {
		t.Fatalf("expected clean original text without trailing @@, got %q", got)
	}
}
