"""Owner-scoped queries for the generic message call ledger."""
from .wechat import result


class Calls:
    def __init__(self, plugin, account_id, platform="telegram"):
        if not account_id:
            raise ValueError("account_id is required")
        if not isinstance(platform, str) or not platform.strip():
            raise ValueError("platform is required")
        self.plugin, self.account_id, self.platform = plugin, account_id, platform

    async def _query(self, action, **params):
        return result(await self.plugin.api(f"calls.{action}", {
            **params, "account_id": self.account_id, "platform": self.platform,
        }))

    async def get(self, call_id):
        return await self._query("get", call_id=call_id)

    async def find(self, idempotency_key):
        return await self._query("find", idempotency_key=idempotency_key)

    async def detail(self, call_id):
        return await self._query("detail", call_id=call_id)

    async def list(self, **filters):
        return await self._query("list", **filters)

    async def queue(self):
        return await self._query("queue")
