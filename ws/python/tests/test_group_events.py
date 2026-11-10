"""群监控协议回归：只连接临时本地宿主，不访问真实微信账号。"""

import asyncio
import json
import unittest
from unittest.mock import AsyncMock

import websockets

from lwplugin import Event, GROUP_REFRESH_HINT, SYNC_HINT, HostInfo, Plugin, PluginError


def hint(name=GROUP_REFRESH_HINT, event_id="hint:1", **data):
    return {"type": "event", "event": name, "event_id": event_id,
            "account": {"platform": "wechat", "id": "wxid_fixture"}, "data": data}


async def hello(ws, events):
    await ws.recv()
    await ws.send(json.dumps({"type": "hello.ok", "host_version": "fixture",
                              "events": events, "capabilities": ["wechat.business.v2"]}))


def client(server, **kwargs):
    return Plugin("test.groups", url=f"ws://127.0.0.1:{server.sockets[0].getsockname()[1]}",
                  token="fixture", events=[GROUP_REFRESH_HINT, SYNC_HINT], **kwargs)


class HintModelTests(unittest.TestCase):
    def test_optional_versions_keep_zero_null_and_unknown_fields(self):
        event = Event(hint(room_id="room@chatroom", chatroom_version=0,
                           chatroom_info_version=None, extra={"future": True}))
        self.assertEqual(event.group_hint.room_id, "room@chatroom")
        self.assertEqual(event.group_hint.chatroom_version, 0)
        self.assertIsNone(event.group_hint.chatroom_info_version)
        self.assertIsNone(event.group_hint.erase_member_data)
        self.assertEqual(event.data["extra"], {"future": True})
        self.assertIsNone(event.sync_metadata)
        self.assertIsNone(event.cursor)
        self.assertEqual(event.account_id, "wxid_fixture")

    def test_selector_keeps_signed_value_and_is_not_a_member_event(self):
        event = Event(hint(SYNC_HINT, selector=-1, continue_flag=0))
        self.assertEqual(event.sync_metadata.selector, -1)
        self.assertEqual(event.sync_metadata.continue_flag, 0)
        self.assertIsNone(event.group_hint)
        self.assertEqual(event.peer, "")
        self.assertEqual(event.message, {})


class GroupConnectionTests(unittest.IsolatedAsyncioTestCase):
    async def test_old_host_rejected_before_subscription_or_initialization(self):
        received = []

        async def host(ws):
            await hello(ws, [])
            async for frame in ws:
                received.append(json.loads(frame))

        async with websockets.serve(host, "127.0.0.1", 0) as server:
            plugin = client(server)
            initializer = AsyncMock()
            plugin.on_connect(initializer)
            with self.assertRaises(PluginError) as raised:
                async with plugin.connect(timeout=2):
                    self.fail("unsupported subscription must not become ready")
            self.assertEqual(raised.exception.code, "unsupported_event")
            initializer.assert_not_awaited()
        self.assertEqual(received, [])

    async def test_reconnect_initializes_before_ordered_hints_and_can_query_rpc(self):
        # 提示故意先于订阅确认发送，模拟订阅交界处的宿主推送。
        # 每次连接都须先完成快照初始化，之后才处理提示；Hint 永远不发送 ACK。
        order, methods, subscriptions, disconnects = [], [], [], []
        processed = asyncio.Event()

        async def host(ws):
            await hello(ws, [GROUP_REFRESH_HINT, SYNC_HINT])
            subscriptions.append(json.loads(await ws.recv()))
            session = len(subscriptions)
            await ws.send(json.dumps(hint(event_id=f"hint:{session}:1", room_id="room@chatroom")))
            await ws.send(json.dumps({"type": "subscribe.ok"}))
            async for raw in ws:
                frame = json.loads(raw)
                if frame["type"] == "api.call":
                    methods.append(frame["method"])
                    await ws.send(json.dumps({"type": "api.result", "req_id": frame["req_id"],
                                              "ok": True, "data": {"accounts": []}}))
                    await ws.send(json.dumps(hint(SYNC_HINT, f"hint:{session}:2", selector=-1)))

        async with websockets.serve(host, "127.0.0.1", 0) as server:
            plugin = client(server, event_filter={"accounts": ["wxid_fixture"], "platforms": ["wechat"]})

            @plugin.on_connect()
            async def initialize(info, ctx):
                self.assertIsInstance(info, HostInfo)
                self.assertEqual(info.version, "fixture")
                self.assertTrue(plugin.connected)
                order.append("initialize")
                await ctx.api("accounts.list")
                order.append("initialized")

            async def receive(event):
                order.append(event.name + ":start")
                await asyncio.sleep(0.01)
                order.append(event.name + ":end")
                if event.name == SYNC_HINT:
                    processed.set()

            plugin.on(GROUP_REFRESH_HINT, receive)
            plugin.on(SYNC_HINT, receive)
            plugin.on_disconnect(lambda error: disconnects.append(error))
            for _ in range(2):
                processed.clear()
                async with plugin.connect(timeout=2):
                    await asyncio.wait_for(processed.wait(), 2)
                self.assertFalse(plugin.connected)
                self.assertFalse(plugin._tasks)

        expected = ["initialize", "initialized", GROUP_REFRESH_HINT + ":start",
                    GROUP_REFRESH_HINT + ":end", SYNC_HINT + ":start", SYNC_HINT + ":end"]
        self.assertEqual(order, expected * 2)
        self.assertEqual(methods, ["accounts.list"] * 2)
        self.assertEqual(disconnects, [None, None])
        self.assertTrue(all(s["filter"] == plugin.event_filter for s in subscriptions))

    async def test_failed_initialization_cancels_buffered_hint_and_reports_error(self):
        async def host(ws):
            await hello(ws, [GROUP_REFRESH_HINT, SYNC_HINT])
            await ws.recv()
            await ws.send(json.dumps(hint(room_id="room@chatroom")))
            await ws.send(json.dumps({"type": "subscribe.ok"}))
            await ws.wait_closed()

        async with websockets.serve(host, "127.0.0.1", 0) as server:
            plugin = client(server)
            handler, disconnected = AsyncMock(), []
            plugin.on(GROUP_REFRESH_HINT, handler)
            plugin.on_connect(AsyncMock(side_effect=ValueError("snapshot failed")))
            plugin.on_disconnect(lambda error: disconnected.append(error))
            with self.assertLogs("lwplugin", level="ERROR"):
                with self.assertRaisesRegex(ValueError, "snapshot failed"):
                    async with plugin.connect(timeout=2):
                        self.fail("failed initialization must not become ready")
            handler.assert_not_awaited()
            self.assertIsInstance(disconnected[0], ValueError)
            self.assertFalse(plugin._session_running)
            self.assertFalse(plugin._tasks)

    async def test_failed_hint_closes_session_before_the_next_hint(self):
        # 有序处理中的失败必须先恢复连接/快照，不能继续推进后续提示。
        plugin = Plugin("test.failure")
        plugin._slots = asyncio.Semaphore(1)
        plugin._ws = AsyncMock()
        plugin.log = AsyncMock()
        failure = AsyncMock(side_effect=ValueError("refresh failed"))
        after = AsyncMock()
        plugin.on(GROUP_REFRESH_HINT, failure)
        plugin.on(SYNC_HINT, after)
        with self.assertLogs("lwplugin", level="ERROR"):
            await asyncio.gather(plugin._run_realtime(Event(hint())),
                                 plugin._run_realtime(Event(hint(SYNC_HINT))))
        plugin._ws.close.assert_awaited_once()
        after.assert_not_awaited()

    async def test_disconnect_cancels_initialization_and_buffered_events(self):
        started, cancelled = asyncio.Event(), asyncio.Event()

        async def host(ws):
            await hello(ws, [GROUP_REFRESH_HINT, SYNC_HINT])
            await ws.recv()
            await ws.send(json.dumps(hint()))
            await ws.send(json.dumps({"type": "subscribe.ok"}))
            await started.wait()
            await ws.close()

        async with websockets.serve(host, "127.0.0.1", 0) as server:
            plugin = client(server)
            handler = AsyncMock()
            plugin.on(GROUP_REFRESH_HINT, handler)

            @plugin.on_connect()
            async def initialize(info):
                started.set()
                try:
                    await asyncio.Event().wait()
                finally:
                    cancelled.set()

            with self.assertRaises(PluginError):
                async with plugin.connect(timeout=2):
                    self.fail("disconnected initialization must not become ready")
            self.assertTrue(cancelled.is_set())
            handler.assert_not_awaited()
            self.assertIsNone(plugin._initialization)
            self.assertFalse(plugin._session_running)

    async def test_unknown_and_partial_receipts_never_reissue_a_request(self):
        wx = Plugin("test.receipt").wechat("wxid_fixture")
        wx.calls.wait = AsyncMock()
        for status in ("unknown", "partial", "failed", "succeeded"):
            receipt = {"call_id": "same", "status": status}
            self.assertEqual(await wx.wait_result(receipt), receipt)
        wx.calls.wait.assert_not_awaited()

    async def test_initialization_timeout_can_reuse_the_same_client(self):
        cancelled = asyncio.Event()
        disconnects = []
        starts = 0

        async def host(ws):
            await hello(ws, [GROUP_REFRESH_HINT, SYNC_HINT])
            await ws.recv()
            await ws.send(json.dumps({"type": "subscribe.ok"}))
            await ws.wait_closed()

        async with websockets.serve(host, "127.0.0.1", 0) as server:
            plugin = client(server)

            @plugin.on_connect()
            async def initialize(info):
                nonlocal starts
                starts += 1
                if starts == 1:
                    try:
                        await asyncio.Event().wait()
                    finally:
                        cancelled.set()

            plugin.on_disconnect(lambda error: disconnects.append(error))
            with self.assertRaises(PluginError) as raised:
                async with plugin.connect(timeout=0.2):
                    self.fail("first initialization must time out")
            self.assertEqual(raised.exception.code, "timeout")
            self.assertTrue(cancelled.is_set())
            async with plugin.connect(timeout=2):
                self.assertTrue(plugin.connected)
            self.assertEqual(starts, 2)
            self.assertEqual(disconnects, [None, None])


if __name__ == "__main__":
    unittest.main()
