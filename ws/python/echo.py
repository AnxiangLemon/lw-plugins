"""最小示例：把收到的消息原样回一句。

运行前先在应用「插件 → WS 插件」里启动服务，然后：

    python ws/python/echo.py
"""

import logging
import os

from lwplugin import Context, Event, Plugin

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
PREFIX = os.getenv("LWPLUGIN_ECHO_PREFIX", "echo: ")


async def on_message(event: Event, ctx: Context) -> None:
    if not event.text or event.message.get("direction") == "outbound":
        return
    await ctx.reply(event, f"{PREFIX}{event.text}")


if __name__ == "__main__":
    Plugin(
        "demo.echo",
        title="Echo 示例",
        version="4.0.0",
        description="收到消息后原样回复，用于验证 WS 插件链路",
    ).on_message(on_message).run()
