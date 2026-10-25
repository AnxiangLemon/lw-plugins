"""Real repository Python examples + HTTP panel -> Rust WS host -> mock lwsapi only.
Invoked by the host integration harness. Never discovers a real endpoint.
"""
import asyncio
import importlib
import json
import os
from pathlib import Path
import sys
from uuid import uuid4

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'ws/python'))
from aiohttp import web,ClientSession
from lwplugin import Event,Context,Plugin
import wechat_panel as panel
import echo,assistant,wechat_business

class ReconnectPlugin(Plugin):
    def __init__(self):
        super().__init__('test.reconnect',url=os.environ['LW_MOCK_WS_URL'],token=os.environ['LW_MOCK_WS_TOKEN'],reconnect=False)
        self.ready=asyncio.Event()
    async def _on_hello_ok(self,frame):
        await super()._on_hello_ok(frame);self.ready.set()

async def reconnect_flow():
    first=ReconnectPlugin();task=asyncio.create_task(first.run_async());await first.ready.wait()
    accepted=await first.wechat('wxid_test').submit('msg.send_text_message',{'toWxid':'filehelper','content':'mock-slow'},idempotency_key='disconnect-stable')
    await first.close();await task
    await asyncio.sleep(.3)
    second=ReconnectPlugin();done=asyncio.Event();seen=[]
    @second.on('wechat.call.updated')
    async def updated(event):
        seen.append(event.raw['result']['status'])
        if event.raw['result']['status']=='succeeded':done.set()
    task=asyncio.create_task(second.run_async());await second.ready.wait()
    try:
        await asyncio.wait_for(done.wait(),5)
        result=await second.wechat('wxid_test').calls.find('disconnect-stable')
        assert result.call_id==accepted.call_id and result.status=='succeeded'
        assert 'succeeded' in seen
        await asyncio.sleep(.2)
    finally:
        await second.close();await task

async def main():
    plugin=panel.PanelPlugin(url=os.environ['LW_MOCK_WS_URL'],token=os.environ['LW_MOCK_WS_TOKEN'],reconnect=False)
    task=asyncio.create_task(plugin.run_async())
    await asyncio.wait_for(plugin.ready.wait(),10)
    runner=web.AppRunner(panel.create_app(panel.Panel(plugin)),access_log=None)
    await runner.setup();site=web.TCPSite(runner,'127.0.0.1',0);await site.start()
    base=f'http://127.0.0.1:{runner.addresses[0][1]}'
    try:
        async with ClientSession() as http:
            async with http.get(base+'/api/state?account_id=wxid_test') as response:
                state=await response.json()
            assert len(state['supported'])==85,state
            assert 'msg.sync_messages' not in state['supported'],state
            for method in panel.CATALOG['methods']:
                body={'method':method['name'],'arguments':json.dumps(method['example']),'account_id':'wxid_test','idempotency_key':uuid4().hex,'mode':'call'}
                async with http.post(base+'/api/call',headers={'X-Panel-Request':'1'},json=body) as response:
                    value=await response.json()
                assert response.status==200,(method['name'],value)
                result=json.loads(value['result_json'])
                assert result['status']=='succeeded',(method['name'],result)
        ctx=Context(plugin)
        wx=plugin.wechat('wxid_test')
        r=await wechat_business.send_voice(ctx,'wxid_test','filehelper','AA==',4,1000)
        assert r.status=='succeeded' and r.messages[0].status=='sent',r
        assert (await wechat_business.send_video(ctx,'wxid_test','filehelper','AA==','AA==',2)).status=='succeeded'
        assert (await wechat_business.set_group_notice(ctx,'wxid_test','g@chatroom','mock')).status=='succeeded'
        assert (await wechat_business.download_image(ctx,'wxid_test','mock','mock')).status=='succeeded'
        assert (await wechat_business.follow_official(ctx,'wxid_test','gh_mock')).status=='succeeded'
        event=Event({'event':'wechat.message.received','event_id':'python-echo','account':{'id':'wxid_test','platform':'wechat'},'message':{'peer_id':'filehelper','text':'hello','from':{'id':'wxid_friend'},'direction':'inbound'}})
        await echo.on_message(event,ctx)
        event.raw['event_id']='python-assistant';event.message['text']='/ping'
        await assistant.on_command(event,ctx)
        event.raw['event_id']='python-business';event.message['text']='!wx-ws-methods'
        await wechat_business.on_message(event,ctx)
        accepted=await wx.submit('group.get_chat_room_info',{'chatRoomId':'g@chatroom'},idempotency_key='python-async')
        done=await wx.calls.wait(accepted.call_id)
        assert done.status=='succeeded' and (await wx.calls.find('python-async')).call_id==accepted.call_id
        assert (await wx.call('login.logout',{})).status=='rejected'
        await reconnect_flow()
        print('PASS: reconnect replay + 85 panel operations + echo/assistant/business examples + asynchronous receipt',flush=True)
    finally:
        await runner.cleanup();await plugin.close();await task

if __name__=='__main__': asyncio.run(main())
