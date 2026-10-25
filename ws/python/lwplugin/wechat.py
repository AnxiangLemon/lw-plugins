"""WeChat v2: durable submission, one result contract for Lua and WS."""
from contextvars import ContextVar
from uuid import uuid4

source_event = ContextVar("lw_source_event", default="")

class Result(dict):
    def __getattr__(self, name):
        try: return self[name]
        except KeyError as exc: raise AttributeError(name) from exc

def result(value):
    if isinstance(value, dict): return Result({k: result(v) for k, v in value.items()})
    if isinstance(value, list): return [result(v) for v in value]
    return value

class Calls:
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
    def __init__(self, plugin, account_id):
        if not account_id: raise ValueError("account_id is required")
        self.plugin, self.account_id = plugin, account_id
        self.calls = Calls(self)
    async def _request(self, action, **payload):
        return result(await self.plugin.api(action, {"account_id": self.account_id, **payload}))
    def _options(self, options):
        return {"idempotency_key": uuid4().hex, "source_event_id": source_event.get(), **options}
    async def call(self, method, params=None, **options):
        return await self._request("wechat.call", method=method, params=params or {}, options=self._options(options))
    async def submit(self, method, params=None, **options):
        return await self._request("wechat.submit", method=method, params=params or {}, options=self._options(options))
    async def send(self, to, segments, **options):
        return await self._request("wechat.send", params={"to": to, "segments": list(segments)}, options=self._options(options))
    async def methods(self):
        return await self._request("wechat.methods")
