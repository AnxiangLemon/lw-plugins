"""Isolated protocol fixture used by --demo and tests; no upstream HTTP client."""
import asyncio
import json
import time
from aiohttp import web

DEMO_EMIT_KEY = web.AppKey('demo_emit', object)

def capabilities(catalog):
    return {'version':2,'platform':'wechat','business':[{'name':m['name'],'path':m['path']} for m in catalog['methods']]}


async def start_mock(catalog, calls=None, sample_message=False):
    allowed = {m['name'] for m in catalog['methods']}
    sockets = set()
    receipts = {}
    keys = {}
    subscribed = asyncio.Event()
    def sample_event(index, account, peer, sender, content, msg_type):
        return {'v':2,'type':'event','event':'wechat.message.received',
                'event_id':f'panel-demo-message-{index}','cursor':index,
                'received_at':int(time.time()),'account_id':account,
                'account':{'id':account,'platform':'wechat'},
                'message':{'msg_id':f'demo-message-{index}','peer':peer,
                           'from':{'id':sender,'platform':'wechat'},
                           'chat_type':'group' if peer.endswith('@chatroom') else 'private',
                           'timestamp':int(time.time()),'text':content if msg_type==1 else '',
                           'segments':[{'type':'text','data':{'text':content}}] if msg_type==1 else [],
                           'raw':{'msg_type':msg_type,'content':content}}}
    samples = [sample_event(1,'wxid_demo','friend_demo','friend_demo','模拟收到的消息：你好，面板！',1),
               sample_event(2,'wxid_other','group_demo@chatroom','friend_other',
                            '<msg><emoji md5="demo-emoji" /></msg>',47),
               sample_event(3,'wxid_demo','friend_demo','friend_demo',
                            '<msg><img aeskey="demo-image" /></msg>',3)]
    next_cursor = 0
    async def emit(frame=None):
        nonlocal next_cursor
        await asyncio.wait_for(subscribed.wait(), 3)
        for event in ([frame] if frame is not None else samples if sample_message else []):
            if frame is None:
                next_cursor += 1
                event = {**event, 'cursor':next_cursor,
                         'event_id':f"{event['event_id']}-{next_cursor}"}
            for socket in tuple(sockets):
                if not socket.closed: await socket.send_json(event)
    async def handler(request):
        if request.query.get('token') != 'panel-demo-only': raise web.HTTPUnauthorized()
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        sockets.add(ws)
        try:
            async for message in ws:
                if message.type != web.WSMsgType.TEXT: continue
                frame = json.loads(message.data)
                kind = frame.get('type')
                if kind == 'hello':
                    await ws.send_json({'v':2,'type':'hello.ok','plugin_id':frame['plugin_id'],'host_version':'panel-mock','events':[]})
                elif kind == 'subscribe':
                    await ws.send_json({'v':2,'type':'subscribe.ok','events':frame.get('events',[])})
                    subscribed.set()
                elif kind == 'api.call':
                    method, params = frame['method'], frame['params']
                    if calls is not None: calls.append(frame)
                    result = {'v':2,'type':'api.result','req_id':frame['req_id'],'ok':True}
                    if method == 'accounts.list':
                        result['data'] = [
                            {'account_id':'wxid_demo','platform':'wechat','display_name':'模拟微信账号','online':True,'enabled':True},
                            {'account_id':'tg_demo','platform':'telegram','display_name':'Telegram fixture','online':True,'enabled':True}]
                    elif method == 'wechat.methods': result['data']=capabilities(catalog)
                    elif method == 'wechat.events.ack': result['data']={'cursor':params['cursor']}
                    elif method in ('wechat.call','wechat.submit') and params['account_id']=='wxid_demo' and params['method'] in allowed:
                        key=params.get('options',{}).get('idempotency_key')
                        if key in keys:
                            result['data']=receipts[keys[key]]
                        else:
                            call_id=f'call_mock_{len(receipts)+1}'
                            failure='mock-error' in json.dumps(params['params'])
                            data={'mock':True,'method':params['method'],'received_params':params['params'],'large_id':9223372036854775807}
                            terminal={'call_id':call_id,'account_id':'wxid_demo','method':params['method'],'status':'failed' if failure else 'succeeded','revision':3,'wait_timed_out':False,'outbox_id':None,'messages':[],'data':data,'raw':{'code':400 if failure else 200,'data':data},'error':{'stage':'remote','code':'remote_rejected','message':'mock business failure'} if failure else None}
                            receipts[call_id]=terminal
                            keys[key]=call_id
                            result['data']=dict(terminal,status='queued',revision=1,data=None,raw=None,error=None) if method=='wechat.submit' else terminal
                    elif method == 'wechat.calls.get': result['data']=receipts.get(params['call_id'])
                    elif method == 'wechat.calls.find': result['data']=receipts.get(keys.get(params['idempotency_key']))
                    else:
                        result.update(ok=False,code='unknown_method',message='unknown or forbidden method')
                    await ws.send_json(result)
        finally:
            sockets.discard(ws)
        return ws
    app = web.Application()
    app[DEMO_EMIT_KEY] = emit
    app.router.add_get('/', handler)
    async def shutdown(app):
        for ws in list(sockets): await ws.close()
    app.on_shutdown.append(shutdown)
    runner = web.AppRunner(app, access_log=None)
    await runner.setup()
    site = web.TCPSite(runner,'127.0.0.1',0)
    await site.start()
    port = runner.addresses[0][1]
    return runner, f'ws://127.0.0.1:{port}'
