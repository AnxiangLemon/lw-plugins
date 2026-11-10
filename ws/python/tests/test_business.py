import asyncio
import unittest
from unittest.mock import AsyncMock
from lwplugin import Plugin,Event

class BusinessCallTests(unittest.IsolatedAsyncioTestCase):
    async def test_group_hint_is_a_separate_event_with_account_scope(self):
        from lwplugin import GROUP_REFRESH_HINT, SYNC_HINT
        plugin = Plugin("test.group.hints", events=[GROUP_REFRESH_HINT, SYNC_HINT])
        event = Event({"event":GROUP_REFRESH_HINT,"account":{"id":"wxid_a","platform":"wechat"},"data":{"room_id":"room@chatroom"}})
        self.assertEqual(event.account_id,"wxid_a")
        self.assertEqual(event.data["room_id"],"room@chatroom")
        self.assertEqual(event.message,{})

    async def test_media_segments_match_the_current_server_contract(self):
        from lwplugin import file_segment, video_segment
        self.assertEqual(file_segment('AA==', 'a.txt'),
                         {'type':'file','data':{'base64':'AA==','fileName':'a.txt'}})
        self.assertNotIn('playLength', video_segment('AA==', 'AA==')['data'])
        self.assertNotIn('playLength', video_segment('AA==', 'AA==', 123)['data'])

    async def test_one_contract(self):
        plugin=Plugin("test.business")
        plugin.api=AsyncMock(return_value={"status":"queued","call_id":"call_1"})
        wx=plugin.wechat("wxid_bot")
        params={"chatRoomId":"g@chatroom","content":"name"}
        result=await wx.call("group.set_name",params,idempotency_key="stable")
        self.assertEqual(result.call_id,"call_1")
        plugin.api.assert_awaited_with("wechat.call",{"account_id":"wxid_bot","method":"group.set_name","params":params,"options":{"source_event_id":"","idempotency_key":"stable"}})
        await wx.submit("group.set_name",params)
        self.assertEqual(plugin.api.call_args.args[0],"wechat.submit")
        await wx.calls.find("stable")
        plugin.api.assert_awaited_with("wechat.calls.find",{"account_id":"wxid_bot","idempotency_key":"stable"})
        await wx.methods()
        self.assertEqual(plugin.api.call_args.args[0],"wechat.methods")

    async def test_durable_ack_follows_callback_and_deduplicates(self):
        plugin=Plugin("test.events");plugin._slots=asyncio.Semaphore(1);plugin.api=AsyncMock()
        order=[]
        async def handler(event):
            await asyncio.sleep(.01)
            order.append(event.event_id)
        plugin.on("wechat.call.updated",handler)
        events=[Event({"event":"wechat.call.updated","event_id":str(i),"cursor":i}) for i in [1,2]]
        await asyncio.gather(*(plugin._run_durable(e) for e in events))
        self.assertEqual(order,["1","2"])
        self.assertEqual([c.args[1]["cursor"] for c in plugin.api.call_args_list],[1,2])
        await plugin._run_durable(events[0]);self.assertEqual(order,["1","2"])

    async def test_failed_durable_handler_is_not_acknowledged(self):
        plugin=Plugin("test.failure");plugin._slots=asyncio.Semaphore(1);plugin.api=AsyncMock();plugin.log=AsyncMock()
        async def handler(event): raise ValueError("mock failure")
        plugin.on("wechat.call.updated",handler)
        with self.assertLogs("lwplugin",level="ERROR"):
            await plugin._run_durable(Event({"event":"wechat.call.updated","event_id":"failed","cursor":3}))
        plugin.api.assert_not_awaited()

    async def test_filter_is_explicit(self):
        plugin=Plugin("test.filter");plugin.api=AsyncMock()
        @plugin.filter
        def filter(event):return plugin.stop("mock")
        await plugin._run_durable(Event({"event":"wechat.message.filter","event_id":"filter_1","cursor":4}))
        self.assertEqual(plugin.api.call_args_list[0].args,("wechat.filters.decide",{"event_id":"filter_1","stop":True}))

    async def test_queries_keep_account_scope(self):
        plugin=Plugin("test.history");plugin.api=AsyncMock(return_value={"items":[]})
        wx=plugin.wechat("account")
        await wx.calls.list(status="unknown",limit=20,account_id="other")
        plugin.api.assert_awaited_with("wechat.calls.list",{"account_id":"account","status":"unknown","limit":20})
        await wx.calls.detail("call_1")
        plugin.api.assert_awaited_with("wechat.calls.detail",{"account_id":"account","call_id":"call_1"})
        await wx.calls.queue()
        plugin.api.assert_awaited_with("wechat.calls.queue",{"account_id":"account"})
