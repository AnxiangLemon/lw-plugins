"""微信 v2 业务封装：按账号提交、查询原回执，不自动重发业务。"""
from contextvars import ContextVar
import asyncio
from os import PathLike
from typing import Union
from uuid import uuid4
from .events import image_file_segment

source_event = ContextVar("lw_source_event", default="")

def new_key():
    """生成随机幂等键；一个业务动作生成一次，超时和重连后保存原键。"""
    return uuid4().hex


class Result(dict):
    """兼容字典和属性访问，保留宿主未来扩展的全部回执字段。"""
    def __getattr__(self, name):
        try: return self[name]
        except KeyError as exc: raise AttributeError(name) from exc

def result(value):
    if isinstance(value, dict): return Result({k: result(v) for k, v in value.items()})
    if isinstance(value, list): return [result(v) for v in value]
    return value

class Calls:
    """微信调用账本，所有查询都绑定到 Wechat 的账号与当前插件。"""
    def __init__(self, wx): self.wx = wx
    async def get(self, call_id):
        return await self.wx._request("wechat.calls.get", call_id=call_id)
    async def list(self, **filters):
        filters["account_id"] = self.wx.account_id
        return await self.wx._request("wechat.calls.list", **filters)
    async def detail(self, call_id):
        return await self.wx._request("wechat.calls.detail", call_id=call_id)
    async def queue(self):
        return await self.wx._request("wechat.calls.queue")
    async def wait(self, call_id, *, wait_timeout_ms=30000):
        return await self.wx._request("wechat.calls.wait", call_id=call_id, options={"wait_timeout_ms": wait_timeout_ms})
    async def find(self, idempotency_key):
        return await self.wx._request("wechat.calls.find", idempotency_key=idempotency_key)

class Wechat:
    """命名业务的薄封装。参数原样传宿主，接口目录由 methods() 动态获取。"""
    def __init__(self, plugin, account_id):
        if not account_id: raise ValueError("account_id is required")
        self.plugin, self.account_id = plugin, account_id
        self.calls = Calls(self)
    async def _request(self, action, **payload):
        return result(await self.plugin.api(action, {"account_id": self.account_id, **payload}))
    def _options(self, options):
        return {"idempotency_key": new_key(), "source_event_id": source_event.get(), **options}
    async def call(self, method, params=None, **options):
        """提交并按宿主策略等待；wait=False 可立即返回，成功须检查 status。"""
        return await self._request("wechat.call", method=method, params=params or {}, options=self._options(options))
    async def submit(self, method, params=None, **options):
        """仅等待入队回执，之后通过 calls 查询；受理不等于业务执行成功。"""
        return await self._request("wechat.submit", method=method, params=params or {}, options=self._options(options))
    async def send(self, to, segments, **options):
        return await self._request("wechat.send", params={"to": to, "segments": list(segments)}, options=self._options(options))
    async def send_text(self, to: str, text: str, **options):
        """文本发送快捷方法，与 Go SendText 对应。"""
        return await self.send(to, [{"type": "text", "data": {"text": text}}], **options)
    async def send_image(self, to: str, path: Union[str, PathLike], **options):
        """发送一张本地图片，结果及幂等规则与 send 完全相同。

        只负责图片编码和提交，不隐式发起其他业务调用；需要时由插件先调用
        cdn.get_dns。文件读取在工作线程中执行，避免阻塞消息接收与事件确认。
        queued/running 只是已受理，最终状态通过 calls.wait 查询。
        """
        segment = await asyncio.to_thread(image_file_segment, path)
        return await self.send(to, [segment], **options)
    async def wait_result(self, receipt, *, timeout=None):
        """对齐 Go Await：只查询原调用直到结束，不提交或重发业务请求。

        unknown/partial/failed 会原样返回，由业务层决定如何核对。
        timeout 是客户端等待秒数；到期抛出 asyncio.TimeoutError，原调用不会取消。
        """
        async def poll():
            current = result(receipt)
            while current.get("status") in {"queued", "running"}:
                call_id = current.get("call_id")
                if not call_id:
                    raise ValueError("queued result is missing call_id")
                current = await self.calls.wait(call_id, wait_timeout_ms=10000)
            return current
        if timeout is not None and timeout <= 0:
            raise ValueError("timeout must be positive")
        return await asyncio.wait_for(poll(), timeout=timeout)
    async def methods(self):
        return await self._request("wechat.methods")
