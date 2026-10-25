import asyncio
import unittest
from unittest.mock import AsyncMock

from lwplugin import Event, Plugin


class HandlerDiagnosticsTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.plugin = Plugin("test.diagnostics")
        self.plugin._slots = asyncio.Semaphore(1)
        self.plugin.log = AsyncMock()
        self.event = Event({
            "event": "message.received", "event_id": "event-1",
            "account": {"id": "account-1", "platform": "wechat"},
        })

    async def test_handler_failure_keeps_traceback_and_reaches_host(self):
        async def broken(event, ctx):
            raise ValueError("diagnostic fixture")

        following = AsyncMock()
        with self.assertLogs("lwplugin", level="ERROR") as logs:
            await self.plugin._run_handlers(self.event, [broken, following])

        following.assert_awaited_once()
        self.plugin.log.assert_awaited_once()
        report = self.plugin.log.call_args.args[0]
        for expected in ("test.diagnostics", "event-1", "account-1", "broken", "Traceback", "ValueError: diagnostic fixture"):
            self.assertIn(expected, report)
        self.assertEqual(self.plugin.log.call_args.kwargs["level"], "error")
        self.assertIn("ValueError: diagnostic fixture", logs.output[0])

    async def test_unavailable_host_does_not_stop_following_handlers(self):
        async def broken(event, ctx):
            raise RuntimeError("handler failed")

        self.plugin.log.side_effect = ConnectionError("disconnected")
        following = AsyncMock()
        with self.assertLogs("lwplugin", level="ERROR"):
            await self.plugin._run_handlers(self.event, [broken, following])
        following.assert_awaited_once()

    async def test_cancellation_is_not_reported_as_plugin_error(self):
        async def cancelled(event, ctx):
            raise asyncio.CancelledError()

        with self.assertRaises(asyncio.CancelledError):
            await self.plugin._run_handlers(self.event, [cancelled])
        self.plugin.log.assert_not_awaited()


if __name__ == "__main__":
    unittest.main()
