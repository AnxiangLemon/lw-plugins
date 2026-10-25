--[[
  demo — 插件写法示例（仅保留 ping/pong）

  指令：!ping → 回复 pong 🏓
  过滤职责（自己消息 / 系统消息 / 非文本等）已由 message_filter 插件承担。

  handle 返回值：
    · return / nil  — 本插件不处理，继续插件链
    · 普通事件回调不会自动终止其他插件
]]

PLUGIN_ID          = "demo"
PLUGIN_TITLE       = "Demo"
PLUGIN_VERSION     = "4.0.2"
PLUGIN_DESCRIPTION = "示例插件：!ping 回复 pong"
PLUGIN_AUTHOR      = "艾柯希华"
PLUGIN_ICON        = "🧪"
PLUGIN_LICENSE     = "MIT"

local L = require("L")
local plugin = require("lw.plugin")

local function handle(account_id, message)
    -- 自己消息不回复（最小防护）
    if L.is_self(account_id, message) then
        return
    end

    local text = (message.text or ""):gsub("^%s+", ""):gsub("%s+$", "")
    if string.lower(text) == "!ping" then
        L.reply(account_id, message, "pong 🏓")
        return
    end
end

local function on_message(event)
    local message = event.message
    message.event_id = event.event_id
    return handle(event.account_id, message)
end

plugin.on("wechat.message.received", on_message)
-- Older Desktop builds do not support generic events or plugin.reply yet.
if type(plugin.reply) == "function" then
    plugin.on("message.received", on_message)
end
