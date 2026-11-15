"""通过 LW Tools 刷新微信 CDN DNS，再给指定联系人或群聊发送一张原图。"""

import argparse
import asyncio
import base64
import json
import sys
from pathlib import Path

from websockets.exceptions import WebSocketException

from lwplugin import EndpointNotFound, Plugin, PluginError, wechat_segment


# Built-in 160 x 120 PNG so the probe can run without an external image.
SAMPLE_IMAGE = (
    "iVBORw0KGgoAAAANSUhEUgAAAKAAAAB4CAIAAAD6wG44AAABLklEQVR42u3dQQ2AMBBFwVXRoKAC"
    "MIIH3NRKjVQWPffGgSZkM5unYEfAj3JeSlx4AWABFmABFmAB1ivgeo+UHb2lDDBgwIABAwYMGDBg"
    "wIABAwYMGDBgwIABAwYMGDBgwIABAwYMGDBgwIABAwYMGDBgwIABAwa8p+fHBxgwYMCAAQMGDBgw"
    "YMCAAQMGDBgwYMCAAQMGDBgwYMCAAQMGDBgwYMCAAQMGDBgwYMCAAQMGDBgwYMCAAQMGDBgwYMCA"
    "AQMGDBgwYMCAAQMGDBgwYMCAAQMGDBgwYMCAAQMGDBgwYMCAAQMGbJQDMGDAgAEDBgwYMGDAgAED"
    "BgwYMGDAgAEDBgwYMGDAgAEDBgwYMGDAgAEDBgwYMGDAgAEDBvwVsHIEGLAAC7AAC7AAawGeLIut"
    "CqWBykEAAAAASUVORK5CYII="
)
MAX_IMAGE_BYTES = 20 * 1024 * 1024


def emit(phase, **fields):
    print(json.dumps({"phase": phase, **fields}, ensure_ascii=False), flush=True)


async def finish_call(wx, phase, receipt):
    if receipt["status"] in {"queued", "running"}:
        emit(phase, call_id=receipt["call_id"], status=receipt["status"])
        receipt = await wx.wait_result(receipt, timeout=150)

    data = receipt.get("data")
    raw = receipt.get("raw")
    data = data if isinstance(data, dict) else {}
    details = {}
    if phase == "cdn.get_dns":
        details["fetched_at"] = data.get("fetchedAt")
        details["chat_auth_key_present"] = bool(data["authKeyHex"]) if "authKeyHex" in data else None
        zones = data.get("zoneIps")
        details["chat_zone_count"] = len(zones) if isinstance(zones, list) else None
    elif data.get("parts"):
        part = data["parts"][0]
        raw = part.get("raw") or {}
        details["send_method"] = part.get("method")

    emit(
        phase,
        call_id=receipt.get("call_id"),
        status=receipt["status"],
        remote_code=raw.get("code") if isinstance(raw, dict) else None,
        error=receipt.get("error"),
        **details,
    )
    return receipt


async def probe(plugin, account, target, image_base64):
    async with plugin.connect():
        wx = plugin.wechat(account)
        emit("connected", plugin_id=plugin.plugin_id, account_id=account, target=target)

        dns = await finish_call(wx, "cdn.get_dns", await wx.call("cdn.get_dns", {}))
        if dns["status"] != "succeeded":
            return 2

        segment = wechat_segment(
            "msg.send_image_via_cdn",
            {"base64": image_base64, "sendOriginal": True},
        )
        sent = await finish_call(wx, "message.send", await wx.send(target, [segment]))
        return 0 if sent["status"] == "succeeded" else 3


def main():
    parser = argparse.ArgumentParser(
        description=__doc__,
        epilog=(
            '示例：python wechat_cdn_probe.py --account wxid_sender '
            '--target wxid_recipient --image "D:\\Photos\\my photo.png"\n'
            '在终端中同时省略 --target 和 --image 时，会提示输入联系人 ID 和图片路径。'
        ),
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--account", required=True, help="LW Tools 中已登录的微信账号 ID")
    parser.add_argument("--target", help="收图联系人 ID（如 wxid_…）或群 ID（…@chatroom），默认 filehelper")
    parser.add_argument("--image", type=Path, help="本地 JPEG 或 PNG 路径（最大 20 MiB），默认用内置测试图")
    parser.add_argument("--endpoint-file", help="宿主的 plugin-endpoint.json，可用 SDK 的默认发现方式")
    parser.add_argument("--plugin-id", default="example.wechat-cdn-probe", help="宿主允许接入的插件 ID")
    args = parser.parse_args()

    try:
        if args.target is None and args.image is None and sys.stdin.isatty():
            print("收图联系人 ID 或群 ID：", end="", file=sys.stderr, flush=True)
            args.target = input().strip()
            if not args.target:
                raise ValueError("请输入收图联系人 ID 或群 ID")
            print("本地 JPEG 或 PNG 路径：", end="", file=sys.stderr, flush=True)
            image_path = input().strip().strip('"')
            if not image_path:
                raise ValueError("请输入本地图片路径")
            args.image = Path(image_path)

        target = "filehelper" if args.target is None else args.target.strip()
        if not target:
            raise ValueError("请输入收图联系人 ID 或群 ID")

        image_base64 = SAMPLE_IMAGE
        if args.image is not None:
            with args.image.open("rb") as source:
                content = source.read(MAX_IMAGE_BYTES + 1)
            if len(content) > MAX_IMAGE_BYTES:
                raise ValueError("图片超过 20 MiB")
            if not content.startswith((b"\x89PNG\r\n\x1a\n", b"\xff\xd8\xff")):
                raise ValueError("请提供 JPEG 或 PNG 图片")
            image_base64 = base64.b64encode(content).decode("ascii")

        plugin = Plugin(
            args.plugin_id,
            title="微信 CDN 发图诊断",
            events=[],
            endpoint_file=args.endpoint_file,
            reconnect=False,
        )
        return asyncio.run(probe(plugin, args.account, target, image_base64))
    except (EOFError, KeyboardInterrupt):
        return 130
    except asyncio.TimeoutError:
        emit("probe_error", code="wait_timeout", message="等待回执超时，请用已输出的 call_id 查询原调用")
        return 4
    except (EndpointNotFound, PluginError, OSError, ValueError, WebSocketException) as error:
        emit("probe_error", code=getattr(error, "code", type(error).__name__), message=str(error))
        return 4


if __name__ == "__main__":
    raise SystemExit(main())
