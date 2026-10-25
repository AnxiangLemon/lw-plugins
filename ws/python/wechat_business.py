"""Run with the local lwplugin package installed; commands only query by default.

    python ws/python/wechat_business.py

Install the SDK and configure endpoint discovery as described in README.md.
Media inputs must already have the format required by lwsapi.
"""
from lwplugin import Plugin

plugin = Plugin("wechat.business.example", title="微信业务 API 示例", version="4.0.0")


async def send_voice(ctx, account_id, to, base64, voice_type, voice_time):
    return await ctx.wechat(account_id).call("msg.send_voice", {
        "toWxid": to, "base64": base64,
        "type": voice_type, "voiceTime": voice_time,
    })


async def send_video(ctx, account_id, to, video_base64, cover_base64, seconds):
    return await ctx.wechat(account_id).call("msg.send_video_message", {
        "toWxid": to, "base64": video_base64,
        "imageBase64": cover_base64, "playLength": seconds,
    })


async def set_group_notice(ctx, account_id, chat_room_id, content):
    return await ctx.wechat(account_id).call("group.set_announcement", {
        "chatRoomId": chat_room_id, "content": content,
    })


async def download_image(ctx, account_id, file_no, aes_key):
    await ctx.wechat(account_id).call("cdn.get_dns")
    return await ctx.wechat(account_id).call("cdn.download_image", {
        "fileNo": file_no, "fileAesKey": aes_key,
    })


async def post_moments(ctx, account_id, body):
    return await ctx.wechat(account_id).call("sns.post", body)


async def follow_official(ctx, account_id, app_id):
    return await ctx.wechat(account_id).call("official.follow", {"appId": app_id})


@plugin.on("wechat.message.received")
async def on_message(event, ctx):
    if event.platform != "wechat" or event.message.get("direction") == "outbound":
        return
    if event.text.strip() == "!wx-ws-methods":
        methods = await ctx.wechat(event.account_id).methods()
        await ctx.reply(event, f"微信业务方法数量：{len(methods['business'])}")


if __name__ == "__main__":
    plugin.run()
