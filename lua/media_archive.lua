PLUGIN_ID = "media_archive"
PLUGIN_TITLE = "媒体归档"
PLUGIN_VERSION = "1.0.0"
PLUGIN_DESCRIPTION = "按日期收录指定微信会话收到的媒体引用。"
PLUGIN_SETTINGS_PANEL = "true"

PLUGIN_SETTINGS_SCHEMA = [[
[
  {
    "key": "server_url",
    "label": "Server 地址",
    "type": "text",
    "default": "http://127.0.0.1:9088",
    "placeholder": "http://127.0.0.1:9088",
    "help": "异机访问请使用 HTTPS。"
  },
  {
    "key": "importer_token",
    "label": "归档令牌",
    "type": "password",
    "placeholder": "粘贴 Server 启动时打印的归档令牌"
  },
  {
    "key": "accounts",
    "label": "接收账号",
    "type": "tags",
    "default": [],
    "placeholder": "接收账号 ID，例如 wxid_receiver",
    "help": "填写接收消息的机器人微信账号 ID。"
  },
  {
    "key": "peers",
    "label": "收录的会话",
    "type": "tags",
    "default": [],
    "placeholder": "例如 12345678901@chatroom 或 wxid_friend",
    "help": "从消息详情复制会话 ID。"
  }
]
]]

local plugin = require("lw.plugin")
local http = require("http")
local json = require("json")
local settings = require("settings")
local log = require("log")

local function includes(list, value)
    if type(list) ~= "table" then return false end
    for _, item in ipairs(list) do
        if item == value then return true end
    end
    return false
end

plugin.on("wechat.message.observed", function(event)
    local message = event.message
    if type(message) ~= "table" or message.direction ~= "inbound" then return end
    local account = event.account_id
    if not includes(settings.get("accounts"), account) then return end
    if not includes(settings.get("peers"), message.peer) then return end

    local raw = message.raw
    if type(raw) ~= "table" or type(raw.content) ~= "string" then return end
    local source = message.source_message_id
    if type(source) ~= "string" or source == "" then
        log.warn("媒体归档跳过缺少稳定 ID 的事件")
        return
    end

    local server = tostring(settings.get("server_url") or ""):gsub("/+$", "")
    local token = settings.get("importer_token")
    if type(token) ~= "string" or token == "" then error("请配置归档令牌", 0) end
    local authority = server:match("^https?://([^/%?#]+)$")
    if not authority or authority:find("@", 1, true) or authority:find("%s") then
        error("请配置有效的 lwscdn Server 地址", 0)
    end
    if server:sub(1, 7) == "http://" then
        local host = authority:match("^([^:]+):?%d*$") or authority:match("^(%[::1%]):?%d*$")
        if host ~= "127.0.0.1" and host ~= "localhost" and host ~= "[::1]" then
            error("远程 lwscdn Server 需要 HTTPS", 0)
        end
    end

    local payload = json.encode({
        account = account, messageId = source, formatVersion = message.format_version,
        messageType = raw.msg_type, xml = raw.content, peer = message.peer,
        sender = message.from.id, timestamp = message.timestamp, group = message.is_group,
        retention = "index_only"
    })
    local response = http.post(server .. "/v1/imports/wechat", payload, {
        timeout_secs = 5,
        headers = {Authorization = "Bearer " .. token, ["Content-Type"] = "application/json"}
    })
    if response.ok and (response.status == 200 or response.status == 202) then
        local decoded, reply = pcall(json.decode, response.body)
        if decoded and type(reply) == "table" and reply.code == response.status
            and type(reply.data) == "table" and type(reply.data.jobId) == "string"
            and reply.data.jobId ~= "" then return end
    end
    if response.ok and (response.status == 400 or response.status == 404) then
        log.warn("媒体归档请求格式被拒绝，消息 " .. source)
        return
    end
    error("媒体归档尚未确认受理，消息 " .. source .. "，HTTP " .. tostring(response.status or "未连接"), 0)
end)
