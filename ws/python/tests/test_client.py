"""通用客户端与媒体编码的离线回归；测试文件只创建于临时目录。"""

import asyncio
import base64
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import AsyncMock, patch

import websockets

from lwplugin import EndpointNotFound, Plugin, PluginError, image_file_segment, load_endpoint



class ImageFileTests(unittest.TestCase):
    def test_fixture_bytes_are_preserved_in_the_wire_segment(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = Path(directory) / "image.png"
            fixture.write_bytes(b"\x89PNG\r\n\x1a\nfixture")
            segment = image_file_segment(fixture)
        self.assertEqual(segment["type"], "image")
        source = segment["data"]["source"]
        self.assertEqual(source["kind"], "base64")
        decoded = base64.b64decode(source["base64"], validate=True)
        self.assertEqual(decoded, b"\x89PNG\r\n\x1a\nfixture")
        self.assertTrue(decoded.startswith(b"\x89PNG\r\n\x1a\n"))

    def test_empty_or_missing_file_fails_before_submission(self):
        with tempfile.TemporaryDirectory() as directory:
            image = Path(directory) / "empty.png"
            image.touch()
            with self.assertRaises(ValueError):
                image_file_segment(image)
            with self.assertRaises(FileNotFoundError):
                image_file_segment(image.with_name("missing.png"))

    def test_endpoint_is_found_beside_script_even_with_another_cwd(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            script = root / "plugin" / "example.py"
            script.parent.mkdir()
            (script.parent / "plugin-endpoint.json").write_text(
                json.dumps({"host": "127.0.0.1", "port": 12345, "token": "fixture"}), encoding="utf-8"
            )
            with patch.dict(os.environ, {}, clear=True), patch.object(sys, "argv", [str(script)]), patch("lwplugin.endpoint.Path.cwd", return_value=root):
                self.assertEqual(load_endpoint().port, 12345)
                # 显式的错误环境变量不能悄悄连接其他工作目录中的宿主。
                with patch.dict(os.environ, {"LW_PLUGIN_ENDPOINT_FILE": str(root / "missing.json")}):
                    with self.assertRaises(EndpointNotFound):
                        load_endpoint()


class ConnectionTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.image = Path(self.directory.name) / "image.png"
        self.image.write_bytes(b"\x89PNG\r\n\x1a\nfixture")

    async def test_connect_waits_for_subscription_before_allowing_send(self):
        subscribed = asyncio.Event()
        release = asyncio.Event()
        frames = []

        async def host(ws):
            await ws.recv()
            await ws.send(json.dumps({"type": "hello.ok", "events": []}))
            subscription = json.loads(await ws.recv())
            self.assertEqual(subscription["events"], [])
            subscribed.set()
            await release.wait()
            await ws.send(json.dumps({"type": "subscribe.ok", "events": []}))
            frame = json.loads(await ws.recv())
            frames.append(frame)
            await ws.send(json.dumps({"type": "api.result", "req_id": frame["req_id"], "ok": True,
                                      "data": {"call_id": "image-1", "status": "succeeded"}}))
            await ws.wait_closed()

        async with websockets.serve(host, "127.0.0.1", 0) as server:
            port = server.sockets[0].getsockname()[1]
            plugin = Plugin("test.image", events=[], url=f"ws://127.0.0.1:{port}", token="fixture")
            entered = asyncio.Event()

            async def client():
                async with plugin.connect(timeout=2):
                    entered.set()
                    return await plugin.wechat("wxid_test").send_image(
                        "test@chatroom", self.image, idempotency_key="same-image"
                    )

            task = asyncio.create_task(client())
            await asyncio.wait_for(subscribed.wait(), 2)
            self.assertFalse(entered.is_set())
            release.set()
            receipt = await asyncio.wait_for(task, 3)
            self.assertEqual(receipt.status, "succeeded")
            self.assertIsNone(plugin._ws)
            self.assertIsNone(plugin._managed_session)
            self.assertFalse(plugin._ready.is_set())
        self.assertEqual(len(frames), 1)
        params = frames[0]["params"]
        self.assertEqual(frames[0]["method"], "wechat.send")
        self.assertEqual(params["account_id"], "wxid_test")
        self.assertEqual(params["params"]["to"], "test@chatroom")
        self.assertEqual(params["params"]["segments"], [image_file_segment(self.image)])
        self.assertEqual(params["options"]["idempotency_key"], "same-image")

    async def test_rejected_handshake_never_enters_business_block(self):
        async def host(ws):
            await ws.recv()
            await ws.send(json.dumps({"type": "error", "code": "disabled", "message": "fixture disabled"}))
            await ws.wait_closed()

        async with websockets.serve(host, "127.0.0.1", 0) as server:
            plugin = Plugin("test.rejected", events=[], url=f"ws://127.0.0.1:{server.sockets[0].getsockname()[1]}", token="fixture")
            with self.assertRaises(PluginError) as raised:
                async with plugin.connect(timeout=2):
                    self.fail("business code must not run before acceptance")
            self.assertEqual(raised.exception.code, "disabled")
            self.assertIsNone(plugin._managed_session)

    async def test_missing_subscribe_ack_times_out_and_closes_socket(self):
        closed = asyncio.Event()

        async def host(ws):
            await ws.recv()
            await ws.send(json.dumps({"type": "hello.ok", "events": []}))
            await ws.recv()
            await ws.wait_closed()
            closed.set()

        async with websockets.serve(host, "127.0.0.1", 0) as server:
            plugin = Plugin("test.timeout", events=[], url=f"ws://127.0.0.1:{server.sockets[0].getsockname()[1]}", token="fixture")
            with self.assertRaises(PluginError) as raised:
                async with plugin.connect(timeout=0.1):
                    self.fail("business code must not run without subscription acknowledgement")
            self.assertEqual(raised.exception.code, "timeout")
            await asyncio.wait_for(closed.wait(), 2)
            self.assertIsNone(plugin._managed_session)

    async def test_waiting_only_queries_the_original_call(self):
        wx = Plugin("test.wait").wechat("wxid_test")
        wx.calls.wait = AsyncMock(side_effect=[{"status": "running", "call_id": "same"},
                                             {"status": "succeeded", "call_id": "same"}])
        result = await wx.wait_result({"status": "queued", "call_id": "same"}, timeout=2)
        self.assertEqual(result["status"], "succeeded")
        self.assertEqual(wx.calls.wait.await_count, 2)
        self.assertTrue(all(call.args == ("same",) for call in wx.calls.wait.await_args_list))


if __name__ == "__main__":
    unittest.main()
