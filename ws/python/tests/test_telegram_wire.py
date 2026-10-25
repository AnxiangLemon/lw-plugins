import asyncio
import json
import unittest
from unittest.mock import AsyncMock, Mock

import websockets

from lwplugin import Plugin, PluginError
from lwplugin.events import Event


def event(platform, event_id, account_id, peer):
    return {
        "v": 2,
        "type": "event",
        "event": "message.received",
        "event_id": event_id,
        "account": {"platform": platform, "id": account_id},
        "message": {"peer": peer, "text": "/ping", "direction": "inbound"},
    }


class TelegramWireTests(unittest.IsolatedAsyncioTestCase):
    async def test_generic_subscription_telegram_reply_and_explicit_send(self):
        loop = asyncio.get_running_loop()
        completed = loop.create_future()
        frames = []

        async def host(ws):
            hello = json.loads(await ws.recv())
            self.assertEqual((hello["type"], hello["v"]), ("hello", 2))
            await ws.send(json.dumps({"type": "hello.ok", "host_version": "test", "events": ["message.received"]}))
            subscribed = json.loads(await ws.recv())
            self.assertEqual(subscribed["events"], ["message.received"])
            await ws.send(json.dumps(event("telegram", "tg:1", "123456", "987654")))

            for outbox_id in (31, 32):
                action = json.loads(await asyncio.wait_for(ws.recv(), 2))
                frames.append(action)
                await ws.send(json.dumps({"type": "action.ack", "action_id": action["action_id"],
                                          "ok": True, "outbox_id": outbox_id, "delivered": True}))
            await asyncio.wait_for(completed, 2)

        async with websockets.serve(host, "127.0.0.1", 0) as server:
            port = server.sockets[0].getsockname()[1]
            plugin = Plugin("test.telegram", url=f"ws://127.0.0.1:{port}", token="fixture")

            @plugin.on_message()
            async def on_message(inbound, ctx):
                reply = await ctx.reply(inbound, "pong")
                sent = await ctx.send(inbound.account_id, inbound.peer, "notice",
                                      platform="telegram")
                completed.set_result((reply, sent))

            await asyncio.wait_for(plugin._session(), 3)

        self.assertEqual([frame["kind"] for frame in frames], ["reply", "send"])
        self.assertEqual(frames[0]["event_id"], "tg:1")
        self.assertEqual(frames[0]["platform"], "telegram")
        self.assertEqual(frames[0]["idempotency_key"], "reply:tg:1:1")
        self.assertTrue(frames[1]["idempotency_key"])
        self.assertEqual(frames[1]["platform"], "telegram")
        self.assertEqual(frames[0]["segments"][0]["data"]["text"], "pong")
        self.assertEqual((frames[1]["account_id"], frames[1]["to"]), ("123456", "987654"))
        self.assertEqual([ack["outbox_id"] for ack in completed.result()], [31, 32])

    async def test_third_platform_reply_and_concurrent_query_context(self):
        plugin = Plugin("test.platforms")
        plugin._action = AsyncMock(return_value={"ok": True, "status": "queued", "call_id": "third"})
        third = Event(event("feishu", "third:1", "123456", "987654"))
        await plugin._context.reply(third, "pong")
        self.assertEqual(plugin._action.await_args.kwargs["platform"], "feishu")
        self.assertEqual(plugin._action.await_args.kwargs["event_id"], "third:1")
        plugin._slots = asyncio.Semaphore(2)
        ready = asyncio.Event()
        seen = []
        observed = []

        async def handler(inbound, ctx):
            seen.append(inbound.platform)
            if len(seen) == 2: ready.set()
            await asyncio.wait_for(ready.wait(), 1)
            observed.append((inbound.platform, ctx.calls(inbound.account_id).platform,
                             plugin.calls(inbound.account_id).platform))

        await asyncio.gather(
            plugin._run_handlers(third, [handler]),
            plugin._run_handlers(Event(event("matrix", "other:1", "123456", "room")), [handler]),
        )
        self.assertCountEqual(observed, [("feishu", "feishu", "feishu"), ("matrix", "matrix", "matrix")])
        self.assertEqual(plugin.calls("bot").platform, "telegram")

    async def test_wechat_reply_keeps_business_api(self):
        loop = asyncio.get_running_loop()
        completed = loop.create_future()

        async def host(ws):
            await ws.recv()
            await ws.send(json.dumps({"type": "hello.ok", "host_version": "test", "events": ["message.received"]}))
            self.assertEqual(json.loads(await ws.recv())["events"], ["message.received"])
            await ws.send(json.dumps(event("wechat", "wx:1", "wxid_bot", "wxid_friend")))
            call = json.loads(await asyncio.wait_for(ws.recv(), 2))
            self.assertEqual((call["type"], call["method"]), ("api.call", "wechat.send"))
            self.assertEqual(call["params"]["options"]["idempotency_key"], "reply:wx:1:1")
            await ws.send(json.dumps({"type": "api.result", "req_id": call["req_id"],
                                      "ok": True, "data": {"status": "delivered", "call_id": "call_1"}}))
            await asyncio.wait_for(completed, 2)

        async with websockets.serve(host, "127.0.0.1", 0) as server:
            port = server.sockets[0].getsockname()[1]
            plugin = Plugin("test.wechat", url=f"ws://127.0.0.1:{port}", token="fixture")

            @plugin.on_message()
            async def on_message(inbound, ctx):
                completed.set_result(await ctx.reply(inbound, "pong"))

            await asyncio.wait_for(plugin._session(), 3)

        self.assertEqual(completed.result().status, "delivered")

    async def test_send_default_remains_wechat_even_inside_third_platform_callback(self):
        plugin = Plugin("test.send-default")
        wx = Mock(send=AsyncMock(return_value={"call_id": "wx-call"}))
        plugin.wechat = Mock(return_value=wx)
        plugin._action = AsyncMock(return_value={"call_id": "third-call"})
        plugin._slots = asyncio.Semaphore(1)
        results = []

        async def handler(inbound, ctx):
            results.append(await ctx.send("bot", "peer", "default"))
            results.append(await plugin.send("bot", "peer", "default"))
            results.append(await ctx.send("bot", "peer", "explicit", platform=inbound.platform))

        await plugin._run_handlers(Event(event("feishu", "third:send", "bot", "peer")), [handler])
        self.assertEqual(results, [{"call_id": "wx-call"}, {"call_id": "wx-call"}, {"call_id": "third-call"}])
        self.assertEqual(wx.send.await_count, 2)
        self.assertEqual(plugin._action.await_args.kwargs["platform"], "feishu")

    async def test_unknown_error_preserves_call_and_key_for_queries(self):
        plugin = Plugin("test.telegram")
        plugin._request = AsyncMock(return_value={"ok": False, "call_id": "call-unknown",
            "status": "unknown", "message": "uncertain delivery"})
        with self.assertRaises(PluginError) as caught:
            await plugin.send("bot", "123", "once", platform="telegram", idempotency_key="stable")
        self.assertEqual(caught.exception.call_id, "call-unknown")
        self.assertEqual(caught.exception.status, "unknown")
        self.assertEqual(caught.exception.idempotency_key, "stable")
        plugin.api = AsyncMock(return_value={"call_id": "call-unknown", "status": "unknown"})
        found = await plugin.calls("bot").find("stable")
        self.assertEqual(found.call_id, "call-unknown")
        plugin.api.assert_awaited_with("calls.find", {"platform": "telegram", "account_id": "bot", "idempotency_key": "stable"})

    async def test_timeout_preserves_key_and_queries_force_account_scope(self):
        plugin = Plugin("test.telegram")
        plugin._request = AsyncMock(side_effect=PluginError("timeout", "fixture timeout"))
        with self.assertRaises(PluginError) as caught:
            await plugin.send("bot", "123", "once", platform="telegram", idempotency_key="same-key")
        self.assertEqual(caught.exception.idempotency_key, "same-key")
        self.assertEqual(caught.exception.account_id, "bot")
        plugin.api = AsyncMock(return_value={"items": []})
        await plugin.calls("bot").list(account_id="other", platform="wechat", status="unknown")
        plugin.api.assert_awaited_with("calls.list", {"platform": "telegram", "account_id": "bot", "status": "unknown"})

    async def test_telegram_action_error_raises_and_clears_pending_request(self):
        loop = asyncio.get_running_loop()
        ready = loop.create_future()

        async def host(ws):
            await ws.recv()
            await ws.send(json.dumps({"type": "hello.ok", "host_version": "test", "events": ["message.received"]}))
            await ws.recv()
            ready.set_result(None)
            action = json.loads(await asyncio.wait_for(ws.recv(), 2))
            await ws.send(json.dumps({"type": "action.ack", "action_id": action["action_id"],
                                      "ok": False, "code": "event_expired", "message": "expired",
                                      "delivered": False}))

        async with websockets.serve(host, "127.0.0.1", 0) as server:
            port = server.sockets[0].getsockname()[1]
            plugin = Plugin("test.reject", url=f"ws://127.0.0.1:{port}", token="fixture")
            plugin.on_message(lambda event: None)
            session = asyncio.create_task(plugin._session())
            await asyncio.wait_for(ready, 2)
            with self.assertRaises(PluginError) as caught:
                await plugin.send("123456", "987654", "notice", platform="telegram")
            self.assertEqual(caught.exception.code, "event_expired")
            self.assertEqual(plugin._pending, {})
            await asyncio.wait_for(session, 2)


if __name__ == "__main__":
    unittest.main()
