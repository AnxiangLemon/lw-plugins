--[[
  message_filter — 消息过滤插件

  用途：
    · 拦截自己发出的消息、微信系统消息、非文本消息、微信表情/XML 等噪音
    · 按发送者、会话、关键词、Lua pattern 过滤
    · 命中过滤时显式 plugin.stop()，终止事件分发

  常用 ID：
    · 微信群：xxx@chatroom
    · 微信私聊：wxid_xxx
    · Telegram 群：chat_id（通常是负数）
    · Telegram 私聊：user_id
]]

PLUGIN_ID          = "message_filter"
PLUGIN_TITLE       = "消息过滤"
PLUGIN_VERSION     = "4.0.0"
PLUGIN_DESCRIPTION = "按消息类型、微信噪音、发送者、会话和关键词拦截消息，防止后续插件处理"
PLUGIN_AUTHOR      = "艾柯希华"
PLUGIN_ICON        = "🧹"
PLUGIN_LICENSE     = "MIT"
PLUGIN_SETTINGS_PANEL = "true"

PLUGIN_SETTINGS_SCHEMA = [[
[
  {
    "key": "only_wechat",
    "label": "仅过滤微信",
    "type": "switch",
    "default": false,
    "help": "开启后只处理微信消息；Telegram 等其它平台消息直接放过"
  },
  {
    "key": "stop_self",
    "label": "过滤自己发出的消息",
    "type": "switch",
    "default": true,
    "help": "避免机器人消息再次进入插件链"
  },
  {
    "key": "stop_non_text",
    "label": "过滤非文本消息",
    "type": "switch",
    "default": true,
    "help": "图片、语音、视频、文件、贴纸、微信表情 XML 等不再进入后续插件"
  },
  {
    "key": "blocked_senders",
    "label": "发送者黑名单",
    "type": "tags",
    "default": [],
    "help": "填写 wxid / user_id；命中后拦截"
  },
  {
    "key": "blocked_peers",
    "label": "会话黑名单",
    "type": "tags",
    "default": [],
    "help": "填写群 ID 或私聊 peer_id；微信群通常是 xxx@chatroom"
  },
  {
    "key": "blocked_keywords",
    "label": "关键词黑名单",
    "type": "tags",
    "default": [],
    "help": "正文包含任一关键词即拦截"
  },
  {
    "key": "blocked_patterns",
    "label": "Lua Pattern 黑名单",
    "type": "tags",
    "default": [],
    "help": "按 Lua string.find pattern 匹配；例如 ^广告、http[s]?://"
  }
]
]]

local L = require("L")
local plugin = require("lw.plugin")
local log = require("log")
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
    return {
        only_wechat = settings.get("only_wechat") == true,
        stop_self = settings.get("stop_self") ~= false,
        stop_system = true,
        stop_non_text = settings.get("stop_non_text") ~= false,
        blocked_senders = as_list(settings.get("blocked_senders")),
        blocked_peers = as_list(settings.get("blocked_peers")),
        blocked_keywords = as_list(settings.get("blocked_keywords")),
        blocked_patterns = as_list(settings.get("blocked_patterns")),
    }
end

local function contains(list, value)
    value = tostring(value or "")
    if value == "" then
        return false
    end
    for i = 1, #list do
        if tostring(list[i] or "") == value then
            return true
        end
    end
    return false
end

local function text_contains_any(text, words)
    text = tostring(text or "")
    if text == "" then
        return false
    end
    for i = 1, #words do
        local word = tostring(words[i] or "")
        if word ~= "" and text:find(word, 1, true) then
            return true
        end
    end
    return false
end

local function text_matches_any(text, patterns)
    text = tostring(text or "")
    if text == "" then
        return false
    end
    for i = 1, #patterns do
        local pattern = tostring(patterns[i] or "")
        if pattern ~= "" then
            local ok, matched = pcall(function()
                return text:find(pattern) ~= nil
            end)
            if ok and matched then
                return true
            elseif not ok then
                log.warn("message_filter invalid pattern: " .. pattern)
            end
        end
    end
    return false
end

local function peer_id(message)
    return L.bridge_session_peer(message)
end

local function block()
    return plugin.stop("message_filter rule matched")
end

function test_settings(s)
    if type(s) ~= "table" then
        error("settings must be a table")
    end
    if s.blocked_patterns ~= nil and type(s.blocked_patterns) == "table" then
        for i = 1, #s.blocked_patterns do
            local p = tostring(s.blocked_patterns[i] or "")
            if p ~= "" then
                local ok = pcall(function() return ("test"):find(p) end)
                if not ok then
                    error("invalid Lua pattern: " .. p)
                end
            end
        end
    end
end

local function handle(account_id, message)
    local c = cfg()
    if c.only_wechat and (message.platform or "") ~= "wechat" then
        return
    end

    if c.stop_self and L.is_self(account_id, message) then
        return block()
    end
    if c.stop_system and L.is_system(message) then
        return block()
    end
    if c.stop_non_text and not L.is_text_only(message) then
        return block()
    end
    if contains(c.blocked_senders, L.sender_id(message)) then
        return block()
    end
    if contains(c.blocked_peers, peer_id(message)) then
        return block()
    end

    local text = trim(message.text or "")
    if text_contains_any(text, c.blocked_keywords) then
        return block()
    end
    if text_matches_any(text, c.blocked_patterns) then
        return block()
    end
end

plugin.filter("wechat.message.received", function(event)
    local message = event.message
    message.event_id = event.event_id
    return handle(event.account_id, message)
end)
