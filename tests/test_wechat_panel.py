"""HTTP -> real Python SDK -> isolated WS fixture, covering every panel entry."""
import asyncio
import json
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'ws/python'))
# Tests can run from sibling source checkouts without installing the SDK.
sys.path.insert(0, str(ROOT.parent / 'lw-sdk/sdk/python'))
from aiohttp import ClientSession, web
import wechat_panel as panel
from wechat_panel_mock import DEMO_EMIT_KEY, start_mock


class PanelFlow(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        self.calls = []
        self.mock, url = await start_mock(panel.CATALOG, self.calls, sample_message=True)
        self.plugin = panel.PanelPlugin(url=url, token='panel-demo-only', reconnect=False)
        self.task = asyncio.create_task(self.plugin.run_async())
        await asyncio.wait_for(self.plugin.ready.wait(), 3)
        self.service = panel.Panel(self.plugin, demo=True)
        self.runner = web.AppRunner(panel.create_app(self.service))
        await self.runner.setup()
        site = web.TCPSite(self.runner,'127.0.0.1',0)
        await site.start()
        self.base = f'http://127.0.0.1:{self.runner.addresses[0][1]}'
        self.http = ClientSession()

    async def asyncTearDown(self):
        await self.http.close()
        await self.runner.cleanup()
        await self.plugin.close()
        await self.task
        await self.mock.cleanup()

    async def post(self, method, arguments, account='wxid_demo', route='call', mode='call', key=None):
        from uuid import uuid4
        async with self.http.post(self.base+'/api/'+route, headers={'X-Panel-Request':'1'}, json={
            'method':method,'account_id':account,'mode':mode,'idempotency_key':key or uuid4().hex,
            'arguments':arguments if isinstance(arguments,str) else json.dumps(arguments),
        }) as response: return response.status,await response.json()

    async def test_all_public_entries_roundtrip(self):
        async with self.http.get(self.base+'/api/state?account_id=wxid_demo') as r: state=await r.json()
        self.assertEqual(set(state['supported']),set(panel.METHODS))
        self.assertEqual(len(panel.METHODS),85)
        self.assertNotIn('msg.sync_messages',panel.METHODS)
        for name,method in panel.METHODS.items():
            with self.subTest(method=name):
                status,value=await self.post(name,method['example'])
                self.assertEqual(status,200,value)
                result=json.loads(value['result_json'])
                self.assertEqual(result['status'],'succeeded')
                self.assertEqual(result['data']['received_params'],method['example'])
                sent=[f for f in self.calls if f['method']=='wechat.call'][-1]['params']
                self.assertEqual(sent['params'],method['example'])
                self.assertIn('9223372036854775807',value['result_json'])

    async def test_account_selection_is_explicit(self):
        async with self.http.get(self.base+'/api/state') as r: value=await r.json()
        self.assertEqual(value['account_id'],'')
        self.assertEqual(value['supported'],[])
        status,_=await self.post('group.quit',{'chatRoomId':'g'},account='')
        self.assertEqual(status,400)

    async def test_messages_are_forwarded_live_without_database_or_replay(self):
        async with self.http.ws_connect(self.base+'/api/events') as viewer:
            event={'v':2,'type':'event','event':'wechat.message.received','cursor':201,
                   'event_id':'live-emoji-1','received_at':100,
                   'account':{'id':'wxid_other','platform':'wechat'},
                   'message':{'msg_id':'emoji-1','peer':'group@chatroom',
                              'from':{'id':'friend'},'timestamp':100,'text':'',
                              'raw':{'msg_type':47,'content':'<msg><emoji /></msg>'}}}
            await self.mock.app[DEMO_EMIT_KEY](event)
            self.assertEqual(await viewer.receive_json(timeout=2),event)
            async with self.http.ws_connect(self.base+'/api/events') as late_viewer:
                with self.assertRaises(asyncio.TimeoutError):
                    await late_viewer.receive_json(timeout=0.2)
                second={**event,'cursor':202,'event_id':'live-text-2',
                        'account':{'id':'wxid_demo','platform':'wechat'},
                        'message':{**event['message'],'text':'hello',
                                   'raw':{'msg_type':1,'content':'hello'}}}
                await self.mock.app[DEMO_EMIT_KEY](second)
                self.assertEqual(await viewer.receive_json(timeout=2),second)
                self.assertEqual(await late_viewer.receive_json(timeout=2),second)
        self.assertFalse(any(frame['method']=='db.query_messages' for frame in self.calls))

    async def test_forbidden_and_old_fields(self):
        for method,body in [('login.logout',{}),('auth.session',{}),('official.oauth_authorize',{}),('msg.sync_messages',{}),('group.quit',{'chat_room_id':'g'}),('msg.send_voice',{'toWxid':'u','type':True}),('sns.post',{'wxid':'another'}),('send_text',[]),('msg.send_image_by_url',{})]:
            status,_=await self.post(method,body)
            self.assertEqual(status,400,(method,body))
        self.assertFalse(any(f['method']=='wechat.call' for f in self.calls))

    async def test_async_and_recovery(self):
        status,value=await self.post('group.quit',{'chatRoomId':'mock'},mode='submit',key='stable')
        self.assertEqual(status,200)
        accepted=json.loads(value['result_json'])
        self.assertEqual(accepted['status'],'queued')
        async with self.http.post(self.base+'/api/receipt',headers={'X-Panel-Request':'1'},json={'account_id':'wxid_demo','idempotency_key':'stable'}) as r:
            final=await r.json()
        self.assertEqual(final['status'],'succeeded')
        self.assertEqual(final['call_id'],accepted['call_id'])

    async def test_business_failure_is_not_http_success(self):
        status,value=await self.post('group.quit',{'chatRoomId':'mock-error'})
        self.assertEqual(status,200)
        self.assertEqual(value['status'],'failed')
        self.assertEqual(json.loads(value['result_json'])['error']['code'],'remote_rejected')

    async def test_result_precision_and_limits(self):
        view=panel.result_view({'id':9223372036854775807,'media':'A'*1000})
        fields={item['key']:item for item in view['children']}
        self.assertEqual(fields['id']['value'],'9223372036854775807')
        self.assertTrue(fields['media']['truncated'])
        self.assertTrue(panel.result_view(list(range(10000)))['truncated'])

    async def test_origin_and_snippet(self):
        async with self.http.post(self.base+'/api/call',headers={'Origin':'https://example.com'},json={}) as response:
            self.assertEqual(response.status,403)
        status,value=await self.post('msg.send_text_message',{'toWxid':'filehelper','content':'hello'},route='preview')
        self.assertEqual(status,200,value)
        self.assertIn('wx.call(',value['python'])
        self.assertIn('idempotency_key=',value['python'])

if __name__=='__main__': unittest.main()
