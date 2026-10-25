local plugin = require("lw.plugin")

local L = {}

L.VERSION = "8.0.1"

-- 视为「纯文本消息」允许的 segment 类型（text / quote / at）
local TEXT_SEGMENT_TYPES = {
    text = true,
    quote = true,
    at = true,
}

-- ── 内部：微信文本过滤 ──────────────────────────────────────

local function trim(s)
    return (s or ""):gsub("^%s+", ""):gsub("%s+$", "")
end

--- 是否整段都是 [表情] / (表情) / （表情）
local function is_wechat_bracket_emoji_only(text)
    local t = trim(text)
    if t == "" then
        return false
    end
    local rest = t:gsub("%[.-%]", ""):gsub("%(.-%)", ""):gsub("（.-）", "")
    return trim(rest) == ""
end

--- 微信原始 msg_type（非微信返回 nil）
local function wechat_msg_type(message)
    if (message.platform or "wechat") ~= "wechat" then
        return nil
    end
    if type(message.msg_type) == "number" then
        return message.msg_type
    end
    local raw = message.raw
    if type(raw) == "table" and type(raw.msg_type) == "number" then
        return raw.msg_type
    end
    return nil
end

local function looks_like_wechat_emoji_xml(text)
    local t = trim(text)
    return t:find("<emoji", 1, true) ~= nil or t:find("<msg><emoji", 1, true) ~= nil
end

-- ── 消息字段 ────────────────────────────────────────────────

--- 发送者 ID（TG 优先 raw.tg.user_id；微信群消息解析 chatroom:\nwxid 格式）
function L.sender_id(message)
    if not message then return "" end
    if (message.platform or "") == "telegram" then
        local raw = message.raw
        if type(raw) == "table" and type(raw.tg) == "table" and raw.tg.user_id ~= nil then
            return tostring(raw.tg.user_id)
        end
    end
    local from = (message.from or {}).id or ""
    -- 微信/通用：处理 chatroom_id:wxid 或 chatroom_id:\nwxid 复合格式（群消息常见）
    if (message.platform or "") ~= "telegram" then
        local colon = from:find(":")
        if colon then
            local after = from:sub(colon + 1)
                :gsub("^[\n\r]+", "")
                :gsub("%s+", "")
            if after ~= "" then
                return after
            end
        end
    end
    return from
end

--- 是否「可当文本处理」：排除图片/表情/贴纸等非文本消息
function L.is_text_only(message)
    if not message then return false end

    -- 微信：仅 msg_type == 1 为文本
    local wx_type = wechat_msg_type(message)
    if wx_type and wx_type ~= 1 then
        return false
    end

    local segs = message.segments
    if type(segs) ~= "table" or #segs == 0 then
        local text = trim(message.text)
        if text == "" then return false end
        if text:find("^%[") then return false end
        if looks_like_wechat_emoji_xml(text) then return false end
        if is_wechat_bracket_emoji_only(text) then return false end
        return true
    end

    local has_text = false
    for i = 1, #segs do
        local seg = segs[i]
        if type(seg) == "table" then
            local seg_type = seg.type or "text"
            if seg_type == "text" then
                local body = trim((seg.data or {}).text)
                if body ~= "" then
                    if looks_like_wechat_emoji_xml(body) then
                        return false
                    end
                    if is_wechat_bracket_emoji_only(body) then
                        return false
                    end
                    has_text = true
                end
            elseif TEXT_SEGMENT_TYPES[seg_type] then
                -- quote / at：允许，但不算正文
            elseif seg_type == "custom" and (seg.custom_type == "emoji" or seg.custom_type == "sticker") then
                return false
            else
                return false
            end
        end
    end
    return has_text
end

--- 微信群 ID（xxx@chatroom）；非群返回 nil
function L.chatroom_id(message)
    if not message then return nil end
    local from = (message.from or {}).id or ""
    local to = (message.to or {}).id or ""
    if from:find("@chatroom") then return from end
    if to:find("@chatroom") then return to end
    return nil
end

function L.is_group(message)
    if message and message.is_group ~= nil then
        return message.is_group
    end
    return L.chatroom_id(message) ~= nil
end

function L.is_private(message)
    return not L.is_group(message)
end

--- 会话对端 ID（路由 / session 用）
--- TG 群 = chat_id，TG 私聊 = user_id；微信群 = chatroom，私聊 = sender
function L.bridge_session_peer(message)
    if not message then return "" end
    if (message.platform or "") == "telegram" then
        local raw = message.raw
        if type(raw) == "table" and type(raw.tg) == "table" then
            if L.is_group(message) then
                if raw.tg.chat_id ~= nil then
                    return tostring(raw.tg.chat_id)
                end
            elseif raw.tg.user_id ~= nil then
                return tostring(raw.tg.user_id)
            end
        end
        return (message.to or {}).id or ""
    end
    local chat_room_id = L.chatroom_id(message)
    if chat_room_id then
        return chat_room_id
    end
    return L.sender_id(message)
end

--- account_id:peer，用于本地会话键
function L.session_key(account_id, message)
    if not message then
        return (account_id or "") .. ":"
    end
    return account_id .. ":" .. L.bridge_session_peer(message)
end

--- 是否机器人自己发出的消息（TG 恒为 false）
function L.is_self(account_id, message)
    if not message then return false end
    if (message.platform or "") == "telegram" then
        return false
    end
    return account_id ~= "" and (message.from or {}).id == account_id
end

--- 是否系统消息（微信 weixin；TG 恒为 false）
function L.is_system(message)
    if not message then return false end
    if (message.platform or "wechat") == "telegram" then
        return false
    end
    return (message.from or {}).id == "weixin"
end


-- All sending helpers return the same durable result as wx:call / wx:send.
function L.reply(account_id, message, text)
    if message.platform == "telegram" then
        return plugin.reply({
            event_id = message.event_id,
            account_id = account_id,
            message = message,
        }, text)
    end
    message._reply_index = (message._reply_index or 0) + 1
    local opts = {}
    if message.event_id then opts.idempotency_key = "reply:" .. message.event_id .. ":" .. message._reply_index end
    return plugin.wechat(account_id):send(message.peer_id, {{type="text", data={text=text}}}, opts)
end
function L.business(account_id, method, params, options)
    return plugin.wechat(account_id):call(method, params, options)
end
return L
