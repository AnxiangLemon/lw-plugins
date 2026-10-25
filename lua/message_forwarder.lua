--[[
  message_forwarder — 消息转发插件

  用途：
    · 将某个群 / 某个人的消息自动转发到某个人 / 某个群
    · 支持多规则、多目标、按账号隔离、按发送者/关键词过滤

  rules JSON 示例：
  [
    {
      "name": "微信群A -> 文件传输助手",
      "source_peers": ["123456@chatroom"],
      "targets": ["filehelper"]
    },
    {
      "name": "某人 -> 监控群",
      "source_senders": ["wxid_xxx"],
      "targets": [
        { "to": "987654@chatroom" }
      ]
    },
    {
      "name": "跨账号转发",
      "account_id": "wxid_source_bot",
      "source_peers": ["wxid_friend"],
      "targets": [
        { "account_id": "wxid_target_bot", "to": "target@chatroom" }
      ]
    }
  ]

  规则字段：
    · enabled: false 可临时关闭
    · account_id: 仅匹配指定接收账号；不填表示任意账号
    · source_peers: 来源会话 ID 列表；群聊填群 ID，私聊填对方 ID
    · source_senders: 来源发送者 ID 列表；群里可精确到某成员
    · chat_types: ["group"] / ["private"]；不填不限制
    · keywords: 正文包含任一关键词才转发；不填不限制
    · exclude_keywords: 正文包含任一关键词则跳过
    · targets: ["wxid_or_chatroom"] 或 [{ "account_id": "...", "to": "..." }]
    · 转发内容即为消息原文，不加前缀/来源信息
]]

PLUGIN_ID          = "message_forwarder"
PLUGIN_TITLE       = "消息转发"
PLUGIN_VERSION     = "4.0.0"
PLUGIN_DESCRIPTION = "按来源会话、发送者、关键词将消息自动转发到指定好友或群"
PLUGIN_AUTHOR      = "艾柯希华"
PLUGIN_ICON        = "📨"
PLUGIN_LICENSE     = "MIT"
PLUGIN_SETTINGS_PANEL = "true"

PLUGIN_SETTINGS_SCHEMA = [[
[
  {
    "key": "rules",
    "label": "转发规则 JSON",
    "type": "json",
    "default": [],
    "placeholder": "[\n  {\n    \"name\": \"微信群 -> 文件传输助手\",\n    \"platforms\": [\"wechat\"],\n    \"chat_types\": [\"group\"],\n    \"source_peers\": [\"123456@chatroom\"],\n    \"targets\": [\"filehelper\"]\n  },\n  {\n    \"name\": \"某人私聊 -> 监控群\",\n    \"source_senders\": [\"wxid_xxx\"],\n    \"chat_types\": [\"private\"],\n    \"targets\": [\"987654@chatroom\"]\n  }\n]",
    "help": "source_peers 填来源群/私聊 peer_id；source_senders 填发送者 ID；targets 填目标 wxid、群 ID、Telegram chat_id，或 {account_id,to}"
  },
  {
    "key": "only_text",
    "label": "仅转发文本消息",
    "type": "switch",
    "default": true,
    "help": "开启后图片、表情、文件等非文本消息不转发"
  }
]
]]

local L = require("L")
local log = require("log")
local plugin = require("lw.plugin")
local settings = require("settings")

local function trim(s)
    return (s or ""):gsub("^%s+", ""):gsub("%s+$", "")
end

local function as_list(v)
    if type(v) == "table" then
        return v
    end
    return {}
end

local function cfg()
    local rules = settings.get("rules")
    if type(rules) ~= "table" then
        rules = {}
    end
    return {
        rules = rules,
        only_text = settings.get("only_text") ~= false,
    }
end

local function list_empty(list)
    return type(list) ~= "table" or #list == 0
end

local function contains(list, value)
    if list_empty(list) then
        return false
    end
    value = tostring(value or "")
    for i = 1, #list do
        if tostring(list[i] or "") == value then
            return true
        end
    end
    return false
end

local function contains_any_text(text, list)
    if list_empty(list) then
        return false
    end
    text = tostring(text or "")
    for i = 1, #list do
        local needle = tostring(list[i] or "")
        if needle ~= "" and text:find(needle, 1, true) then
            return true
        end
    end
    return false
end

local function peer_id(message)
    return L.bridge_session_peer(message)
end

local function chat_type(message)
    if L.is_group(message) then
        return "group"
    end
    return "private"
end

local function matches_rule(account_id, message, rule)
    if type(rule) ~= "table" then
        return false
    end
    if rule.enabled == false then
        return false
    end
    if rule.account_id ~= nil and tostring(rule.account_id) ~= tostring(account_id) then
        return false
    end
    if not list_empty(rule.platforms) and not contains(rule.platforms, message.platform or "") then
        return false
    end
    if not list_empty(rule.chat_types) and not contains(rule.chat_types, chat_type(message)) then
        return false
    end
    if not list_empty(rule.source_peers) and not contains(rule.source_peers, peer_id(message)) then
        return false
    end
    if not list_empty(rule.source_senders) and not contains(rule.source_senders, L.sender_id(message)) then
        return false
    end

    local text = trim(message.text or "")
    if not list_empty(rule.keywords) and not contains_any_text(text, rule.keywords) then
        return false
    end
    if contains_any_text(text, rule.exclude_keywords) then
        return false
    end
    return true
end

local function build_forward_text(message)
    -- 转发内容即消息原文，不加前缀和来源信息
    return trim(message.text or "")
end

local function normalize_targets(rule)
    local targets = rule.targets
    if type(targets) ~= "table" then
        return {}
    end
    return targets
end

local current_event_id = ""
local function send_to_target(default_account_id, target, text)
    local target_account = default_account_id
    local to = ""

    if type(target) == "table" then
        target_account = tostring(target.account_id or default_account_id)
        to = tostring(target.to or target.peer or target.id or "")
    else
        to = tostring(target or "")
    end

    to = trim(to)
    if to == "" then
        return false, "empty target"
    end

    local result = plugin.wechat(target_account):send(to, {{type="text", data={text=text}}}, {idempotency_key="forward:" .. current_event_id .. ":" .. target_account .. ":" .. to})
    return result.status == "succeeded", result.error
end

local function forward_by_rule(account_id, message, rule)
    local text = build_forward_text(message)
    if trim(text) == "" then
        return 0
    end

    local count = 0
    local targets = normalize_targets(rule)
    for i = 1, #targets do
        local ok, err = pcall(function()
            local sent, send_err = send_to_target(account_id, targets[i], text)
            if not sent then
                error(send_err)
            end
        end)
        if ok then
            count = count + 1
        else
            log.warn("message_forwarder send failed rule=" .. tostring(rule.name or i) .. " err=" .. tostring(err))
        end
    end
    return count
end

function test_settings(s)
    if type(s) ~= "table" then
        error("settings must be a table")
    end
    if s.rules ~= nil and type(s.rules) ~= "table" then
        error("rules must be a JSON array")
    end
end

-- 最近转发过的消息内容（防 A→B→A 循环互转）。沙箱禁用了 os，无法取墙钟；
-- 用消息自身的 create_time 做 TTL 基准。key 含会话+内容，避免误伤不同会话的相同文本。
local recent_forwarded = {}
local RECENT_TTL_SECS = 300
local RECENT_MAX = 64

local function recent_key(message)
    return tostring(message.platform or "") .. ":" .. peer_id(message) .. ":" .. tostring(trim(message.text or ""))
end

local function is_recently_forwarded(key, ts)
    local rec = recent_forwarded[key]
    return rec ~= nil and (ts - rec) < RECENT_TTL_SECS
end

local function record_recent(key, ts)
    recent_forwarded[key] = ts
    local n = 0
    for _ in pairs(recent_forwarded) do
        n = n + 1
    end
    if n > RECENT_MAX then
        recent_forwarded = {}
    end
end

local function handle(account_id, message)
    local c = cfg()
    if L.is_self(account_id, message) or L.is_system(message) then
        return
    end
    if c.only_text and not L.is_text_only(message) then
        return
    end

    local text = trim(message.text or "")
    if text == "" then
        return
    end

    local ts = tonumber(message.timestamp or 0) or 0
    local key = recent_key(message)
    if ts > 0 and is_recently_forwarded(key, ts) then
        return
    end

    local total = 0
    for i = 1, #c.rules do
        local rule = c.rules[i]
        if matches_rule(account_id, message, rule) then
            local n = forward_by_rule(account_id, message, rule)
            total = total + n
        end
    end

    if total > 0 then
        if ts > 0 then
            record_recent(key, ts)
        end
    end
end

plugin.on("wechat.message.received", function(event)
    local message = event.message
    message.event_id = event.event_id
    current_event_id = event.event_id
    return handle(event.account_id, message)
end)
