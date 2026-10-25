"""进阶示例：命令路由 + API 调用 + 后台任务。

演示命令路由、历史查询和独立定时任务：
1. 长耗时处理不阻塞消息链（这里用 asyncio.sleep 模拟）；
2. 可以查历史消息、查群成员等宿主 API；
3. 可以自己起后台定时任务，主动发消息。

    python ws/python/assistant.py
"""

import asyncio
import logging
import os
import time

from lwplugin import (
    ACCOUNT_OFFLINE,
    ACCOUNT_ONLINE,
    Context,
    Event,
    Plugin,
    PluginError,
)

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
LOGGER = logging.getLogger("assistant")
BROADCAST_PEER = os.getenv("LWPLUGIN_BROADCAST_PEER", "")
BROADCAST_ACCOUNT = os.getenv("LWPLUGIN_BROADCAST_ACCOUNT", "")

plugin = Plugin(
    "demo.assistant",
    title="助手示例",
    version="4.0.0",
    description="命令路由 / 历史检索 / 定时播报",
    events=["message.received", "account.online", "account.offline"],
    # 只关心以 / 开头的消息，过滤在宿主侧完成，能省掉大量无用推送
    event_filter={"text_prefix": ["/"]},
)


@plugin.on_message()
async def on_command(event: Event, ctx: Context) -> None:
    if event.message.get("direction") == "outbound": return
    command, _, rest = event.text.partition(" ")
    rest = rest.strip()

    if command == "/ping":
        await ctx.reply(event, "pong")

    elif command == "/slow":
        # 普通事件回调等待期间，其他插件继续运行
        await asyncio.sleep(5)
        await ctx.reply(event, "5 秒后的回复，期间宿主毫无阻塞")

    elif command == "/history":
        rows = await ctx.api(
            "db.query_messages",
            {"account_id": event.account_id, "peer": event.peer, "limit": 5},
        )
        if not rows:
            await ctx.reply(event, "这个会话还没有历史记录")
            return
        lines = [
            f"{r.get('from_wxid') or '?'}: {r.get('content') or ''}" for r in rows
        ]
        await ctx.reply(event, "最近 5 条:\n" + "\n".join(lines))

    elif command == "/search":
        if not rest:
            await ctx.reply(event, "用法: /search 关键词")
            return
        rows = await ctx.api(
            "db.query_messages",
            {"account_id": event.account_id, "keyword": rest, "limit": 10},
        )
        await ctx.reply(event, f"命中 {len(rows)} 条包含「{rest}」的消息")

    elif command == "/accounts":
        accounts = await ctx.api("accounts.list")
        lines = [
            f"{a['platform']} / {a['display_name'] or a['account_id']} "
            f"{'在线' if a['online'] else '离线'}"
            for a in accounts
        ]
        await ctx.reply(event, "\n".join(lines) or "还没有配置账号")

    elif command == "/whoami":
        await ctx.reply(
            event,
            f"平台={event.platform} 账号={event.account_id}\n"
            f"会话={event.peer} 群聊={event.is_group}\n"
            f"发送者={event.sender_id}",
        )


@plugin.on(ACCOUNT_ONLINE)
@plugin.on(ACCOUNT_OFFLINE)
async def on_account_status(event: Event, ctx: Context) -> None:
    LOGGER.info(
        "账号 %s/%s %s %s",
        event.platform,
        event.account_id,
        "上线" if event.is_online else "下线",
        event.status.get("reason") or "",
    )


async def broadcast_loop() -> None:
    """每小时向配置的会话播报一次。设置为空时空转。"""
    while True:
        await asyncio.sleep(3600)
        peer = BROADCAST_PEER
        account = BROADCAST_ACCOUNT
        if not peer or not account:
            continue
        try:
            await plugin.send(account, peer, f"整点播报 {time.strftime('%H:%M')}")
        except PluginError as exc:
            LOGGER.warning("播报被宿主拒绝: %s", exc)
        except Exception:  # noqa: BLE001
            LOGGER.exception("播报失败")


async def main() -> None:
    asyncio.create_task(broadcast_loop())
    await plugin.run_async()


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        pass
