PLUGIN_ID = "wechat_business_example"
PLUGIN_TITLE = "微信业务 API 示例"
PLUGIN_VERSION = "4.0.0"
PLUGIN_DESCRIPTION = "v2：统一提交、等待与异步回执；!wx-members 查询群成员"
local plugin = require("lw.plugin")
local L = require("L")
local json = require("json")

function send_voice(account, to, base64, voice_type, milliseconds)
    return plugin.wechat(account):call("msg.send_voice", {toWxid=to, base64=base64, type=voice_type, voiceTime=milliseconds})
end
function send_video(account, to, video, cover, seconds)
    return plugin.wechat(account):call("msg.send_video_message", {toWxid=to, base64=video, imageBase64=cover, playLength=seconds})
end
function set_group_notice(account, room, content)
    return plugin.wechat(account):call("group.set_announcement", {chatRoomId=room, content=content})
end
function download_image(account, file_no, aes_key)
    return plugin.wechat(account):call("cdn.download_image", {fileNo=file_no, fileAesKey=aes_key})
end
function post_moments(account, body)
    return plugin.wechat(account):submit("sns.post", body)
end
plugin.on("wechat.message.received", function(event)
    local message=event.message
    if L.is_self(event.account_id,message) then return end
    message.event_id=event.event_id
    if message.text=="!wx-methods" then
        local methods=plugin.wechat(event.account_id):methods()
        L.reply(event.account_id,message,"微信业务方法数量："..#methods.business)
    elseif message.text=="!wx-members" and message.is_group then
        local result=plugin.wechat(event.account_id):call("group.get_chat_room_member_detail", {chatRoomId=message.peer_id}, {idempotency_key="members:"..event.event_id})
        L.reply(event.account_id,message,json.encode(result))
    end
end)
plugin.on("wechat.call.updated", function(event)
    if event.result.status=="failed" or event.result.status=="unknown" then
        require("log").warn(json.encode(event.result))
    end
end)
