--[[
  archive_clone — 对接 LW Archive 外部分身

  流程：入站文本 → 按路由选 clone_id → POST /api/v1/chat → 回复
  重置：发送 reset_command（默认 /reset）→ DELETE /api/v1/sessions/{id}

  路由优先级：
    1. routes.accounts[account_id] 下的 groups / private / default_clone_id
    2. 全局 routes.groups / routes.private
    3. routes.default / routes.default_clone_id / 设置项 default_clone_id

  peer_id 来源：
    · Telegram：群 chat_id（负数）、私聊 user_id（正数）
    · 微信：群 xxx@chatroom、私聊 wxid
    · 可在「消息」页悬停会话，或查 config/messages.db

  普通事件回调；发送成功也继续分发给其他插件
]]

PLUGIN_ID          = "archive_clone"
PLUGIN_TITLE       = "LW Archive 分身"
PLUGIN_VERSION     = "4.0.0"
PLUGIN_DESCRIPTION = "通过 HTTP 调用 LW Archive /api/v1/chat，支持路由与清除会话"
PLUGIN_AUTHOR      = "艾柯希华"
PLUGIN_ICON        = "🪞"
PLUGIN_LICENSE     = "MIT"
PLUGIN_SETTINGS_PANEL = "true"

PLUGIN_SETTINGS_SCHEMA = [[
[
  {
    "key": "endpoint",
    "label": "Archive 地址",
    "type": "text",
    "default": "http://127.0.0.1:7788",
    "help": "LW Archive 根地址，不要带 /api/v1"
  },
  {
    "key": "api_token",
    "label": "API Token",
    "type": "text",
    "default": "",
    "help": "与 Archive config.toml [api].token 一致；留空表示不鉴权"
  },
  {
    "key": "default_clone_id",
    "label": "默认分身 ID",
    "type": "text",
    "default": "",
    "help": "路由未命中时使用的分身 ID（在 Archive 分身列表复制）"
  },
  {
    "key": "routes",
    "label": "路由 JSON",
    "type": "json",
    "default": {},
    "placeholder": "{\n  \"accounts\": {\n    \"7123456789\": {\n      \"default_clone_id\": \"c_该机器人默认\",\n      \"groups\": { \"-5425627522\": \"c_某群\" },\n      \"private\": { \"2050886933\": \"c_某人\" }\n    }\n  },\n  \"groups\": {},\n  \"private\": {}\n}",
    "help": "peer_id → 分身 ID。可按机器人分账号：accounts 的 key 为 Bot 数字 ID（账号卡片「ID …」），其下可设 default_clone_id / groups / private；未写 accounts 时用全局 groups / private"
  },
  {
    "key": "only_at_me",
    "label": "群聊仅 @ 时回复",
    "type": "switch",
    "default": true
  },
  {
    "key": "reset_command",
    "label": "重置会话指令",
    "type": "text",
    "default": "/reset",
    "help": "发送后清除 Archive 侧该 session 的 AI 上下文"
  },
  {
    "key": "http_timeout_secs",
    "label": "HTTP 超时（秒）",
    "type": "number",
    "default": 25,
    "help": "应小于插件链总超时（约 30s）"
  }
]
]]

local L = require("L")
local log = require("log")
local plugin = require("lw.plugin")
local json = require("json")
local http = require("http")
local settings = require("settings")

-- ── 配置与 HTTP ─────────────────────────────────────────────

local function cfg()
    return settings.all()
end

local function trim(s)
    return (s or ""):gsub("^%s+", ""):gsub("%s+$", "")
end

local function api_base(c)
    return trim(c.endpoint or "http://127.0.0.1:7788"):gsub("/+$", "")
end

local function auth_headers(c)
    local h = { ["Content-Type"] = "application/json" }
    local token = trim(c.api_token)
    if token ~= "" then
        h["Authorization"] = "Bearer " .. token
        h["X-Archive-Token"] = token
    end
    return h
end

--- 超时限制在 [5, 28]，避免拖垮插件链
local function http_opts(c)
    local t = tonumber(c.http_timeout_secs) or 25
    if t < 5 then t = 5 end
    if t > 28 then t = 28 end
    return { headers = auth_headers(c), timeout_secs = t }
end

local function url_encode(s)
    return (s:gsub("([^%w%-%.%_%~])", function(c)
        return string.format("%%%02X", string.byte(c))
    end))
end

-- ── 会话标识 ────────────────────────────────────────────────

--- 路由用 peer：TG 用 bridge_session_peer；微信群用 chatroom，私聊用 from/to
local function peer_id(message)
    if (message.platform or "") == "telegram" then
        return L.bridge_session_peer(message)
    end
    if L.is_group(message) then
        return L.chatroom_id(message) or (message.to or {}).id or (message.from or {}).id or ""
    end
    return (message.from or {}).id or (message.to or {}).id or ""
end

--- Archive session：lwcli:{account}:{platform}:{peer}:{clone}
local function session_id(account_id, message, clone_id)
    local platform = message.platform or "wechat"
    return string.format(
        "lwcli:%s:%s:%s:%s",
        account_id,
        platform,
        peer_id(message),
        clone_id
    )
end

-- ── 群 @ 判定（only_at_me）──────────────────────────────────

local function normalize_at_target(target)
    return trim(tostring(target or "")):lower()
end

local function group_has_at_segment(message)
    local segs = message.segments
    if type(segs) ~= "table" then
        return false
    end
    for i = 1, #segs do
        local seg = segs[i]
        if type(seg) == "table" and seg.type == "at" then
            return true
        end
    end
    return false
end

local function tg_bot_username(message)
    local raw = message.raw
    if type(raw) ~= "table" or type(raw.tg) ~= "table" then
        return ""
    end
    return trim(raw.tg.bot_username or "")
end

--- 群消息是否 @ 了本机器人（segment.at 或正文 @username / 任意 @）
local function mentions_this_bot(message)
    if group_has_at_segment(message) then
        local bot_name = normalize_at_target(tg_bot_username(message))
        if bot_name == "" then
            return true
        end
        local segs = message.segments
        for i = 1, #segs do
            local seg = segs[i]
            if type(seg) == "table" and seg.type == "at" then
                local target = normalize_at_target((seg.data or {}).target_id)
                if target == ("@" .. bot_name) or target == bot_name then
                    return true
                end
            end
        end
    end

    local content = trim(message.text or ""):lower()
    if content == "" then
        return false
    end
    local bot_name = normalize_at_target(tg_bot_username(message))
    if bot_name ~= "" and content:find("@" .. bot_name, 1, true) then
        return true
    end
    -- 无 bot_username 时：正文含 @ 即视为提及
    if content:find("@", 1, true) then
        return true
    end
    return false
end

-- ── 分身路由 ────────────────────────────────────────────────

--- accounts 下按 account_id 查找（兼容 tg_ 前缀有无）
local function account_routes(c, account_id)
    local routes = c.routes
    if type(routes) ~= "table" then
        return nil
    end
    local accounts = routes.accounts
    if type(accounts) ~= "table" then
        return nil
    end

    local scoped = accounts[account_id]
    if type(scoped) == "table" then
        return scoped
    end

    local with_prefix = "tg_" .. tostring(account_id or "")
    scoped = accounts[with_prefix]
    if type(scoped) == "table" then
        return scoped
    end

    if tostring(account_id or ""):match("^tg_") then
        scoped = accounts[tostring(account_id):sub(4)]
        if type(scoped) == "table" then
            return scoped
        end
    end
    return nil
end

--- 在单个 scope（账号级或全局）内解析 clone_id
local function clone_from_scope(scope, message, pid)
    if type(scope) ~= "table" then
        return ""
    end
    if L.is_group(message) then
        local groups = scope.groups
        if type(groups) == "table" and groups[pid] then
            return trim(tostring(groups[pid]))
        end
    else
        local private = scope.private
        if type(private) == "table" and private[pid] then
            return trim(tostring(private[pid]))
        end
    end
    local def = scope.default_clone_id or scope.default
    if def ~= nil and trim(tostring(def)) ~= "" then
        return trim(tostring(def))
    end
    return ""
end

--- 账号路由优先；若配置了 accounts 但未命中则不回落全局（避免串号）
local function resolve_clone_id(account_id, message, c)
    local routes = c.routes
    if type(routes) ~= "table" then
        routes = {}
    end
    local pid = peer_id(message)

    local scoped = account_routes(c, account_id)
    if type(scoped) == "table" then
        return clone_from_scope(scoped, message, pid)
    end

    if L.is_group(message) then
        local groups = routes.groups
        if type(groups) == "table" and groups[pid] then
            return trim(tostring(groups[pid]))
        end
    else
        local private = routes.private
        if type(private) == "table" and private[pid] then
            return trim(tostring(private[pid]))
        end
    end

    local def = routes.default or routes.default_clone_id or c.default_clone_id
    return trim(tostring(def or ""))
end

-- ── Archive 响应 ────────────────────────────────────────────

--- 解析 chat 响应并逐段 reply；成功返回 true
local function reply_from_body(account_id, message, body_str)
    local data = json.decode(body_str or "{}")
    if type(data) ~= "table" then
        return false
    end
    local reply = data.reply
    if type(reply) ~= "table" then
        return false
    end

    local segments = reply.segments
    if type(segments) == "table" and #segments > 0 then
        for i = 1, #segments do
            local seg = segments[i]
            if type(seg) == "table" and seg.type == "text" then
                local text = trim(seg.text)
                if text ~= "" then
                    L.reply(account_id, message, text)
                end
            end
        end
        return true
    end

    local text = trim(reply.text)
    if text ~= "" then
        L.reply(account_id, message, text)
        return true
    end
    return false
end

local function debug_inbound_message(account_id, message)
    local parts = {}
    local segs = message.segments
    if type(segs) == "table" then
        for i = 1, #segs do
            local seg = segs[i]
            if type(seg) == "table" then
                local kind = tostring(seg.type or "?")
                local detail = seg.text or seg.target_id or seg.custom_type or ""
                parts[#parts + 1] = kind .. ":" .. tostring(detail):sub(1, 48)
            end
        end
    end
    local raw = message.raw
    local tg = type(raw) == "table" and raw.tg or {}
    log.debug(string.format(
        "archive_clone inbound account=%s routing_peer=%s from=%s to=%s is_group=%s tg_user_id=%s tg_chat_id=%s tg_username=%s segment_count=%d segments=[%s] content=%s",
        tostring(account_id),
        peer_id(message),
        tostring((message.from or {}).id),
        tostring((message.to or {}).id),
        tostring(L.is_group(message)),
        tostring(tg.user_id or ""),
        tostring(tg.chat_id or ""),
        tostring(tg.username or ""),
        type(segs) == "table" and #segs or 0,
        table.concat(parts, " | "),
        trim(message.text or ""):sub(1, 120)
    ))
    if type(raw) == "table" and raw.segments_debug then
        log.debug("archive_clone segments_debug=" .. json.encode(raw.segments_debug))
    end
    if type(tg) == "table" and tg.entities then
        log.debug("archive_clone tg.entities=" .. json.encode(tg.entities))
    end
end

-- ── handle ──────────────────────────────────────────────────

local function handle(account_id, message)
    local c = cfg()
    debug_inbound_message(account_id, message)

    if L.is_self(account_id, message) then
        return
    end
    if L.is_system(message) then
        return
    end
    if not L.is_text_only(message) then
        return
    end

    local text = trim(message.text)
    if text == "" then
        return
    end

    if c.only_at_me == true and L.is_group(message) and not mentions_this_bot(message) then
        return
    end

    local clone_id = resolve_clone_id(account_id, message, c)
    if clone_id == "" then
        return
    end

    local sid = session_id(account_id, message, clone_id)
    local sid_enc = url_encode(sid)
    local base = api_base(c)
    local opts = http_opts(c)

    -- 重置会话上下文
    local reset_cmd = trim(c.reset_command or "/reset")
    if reset_cmd ~= "" and text == reset_cmd then
        local url = base .. "/api/v1/sessions/" .. sid_enc
        local res = http.request({
            url = url,
            method = "DELETE",
            headers = opts.headers,
            timeout_secs = opts.timeout_secs,
        })
        if res.ok and (res.status or 0) == 200 then
            L.reply(account_id, message, "已清除本会话 AI 上下文")
        else
            log.warn("archive session reset failed: " .. tostring(res.error or res.body))
            L.reply(account_id, message, "清除会话失败，请确认 LW Archive 已启动")
        end
        return false
    end

    -- 正常对话
    local payload = json.encode({
        clone_id = clone_id,
        session_id = sid,
        message = { role = "user", content = text },
        context = {
            platform = message.platform,
            account_id = account_id,
            peer_id = peer_id(message),
            sender_id = (message.from or {}).id,
            message_id = message.msg_id,
        },
    })

    local url = base .. "/api/v1/chat"
    local res = http.post(url, payload, opts)
    if not res.ok or (res.status or 0) ~= 200 then
        log.warn("archive chat failed status=" .. tostring(res.status) .. " err=" .. tostring(res.error or res.body))
        return
    end

    if not reply_from_body(account_id, message, res.body) then
        log.warn("archive chat: empty reply")
        return
    end
    return false
end

plugin.on("wechat.message.received", function(event)
    local message = event.message
    message.event_id = event.event_id
    return handle(event.account_id, message)
end)
