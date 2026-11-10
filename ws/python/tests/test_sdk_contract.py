"""与 Go 共用协议样例；用临时 WS 服务验证，不读取真实端点或发送微信消息。"""
import asyncio
import json
import os
from pathlib import Path
import unittest
from unittest.mock import AsyncMock, patch

import websockets
from lwplugin import (ALL_EVENTS, Event, Plugin, PluginError, MESSAGE_FILTER,
                      at_segment, text_segment, image_segment, voice_segment,
                      video_segment, file_segment, wechat_segment, load_endpoint)
from lwplugin.wechat import result

FIXTURE = json.loads((Path(__file__).resolve().parents[2] / 'go/testdata/sdk-contract.json').read_text(encoding='utf-8'))


class ContractTests(unittest.TestCase):
    def test_events_segments_and_future_fields_match_go(self):
        self.assertCountEqual(ALL_EVENTS, FIXTURE['events'])
        segments = [text_segment('你好'), at_segment('wxid_fixture'), image_segment('AA=='),
                    voice_segment('AA==', 4, 1000), video_segment('AA==', 'AQ=='),
                    file_segment('AA==', '说明.txt'), wechat_segment('msg.send_text_message', {'content':'你好'})]
        self.assertEqual(segments, FIXTURE['segments'])
        event = Event(FIXTURE['event'])
        self.assertEqual(event.trace_id, 'trace-1')
        self.assertEqual(event.call_id, 'call-1')
        self.assertEqual(event.delivery['outbox_id'], 9223372036854775807)
        self.assertEqual(event.status['reason'], 'fixture')
        self.assertEqual(event.result['status'], 'succeeded')
        self.assertEqual(event.raw['future']['value'], 18446744073709551615)
        receipt = result(FIXTURE['result'])
        self.assertEqual(receipt.data.id, 18446744073709551615)
        self.assertEqual(receipt.future, 'preserve')
        self.assertFalse(receipt.delivered)

    def test_endpoint_retains_tls_path_ipv6_and_strips_query_token(self):
        with patch.dict(os.environ, {}, clear=True):
            endpoint = load_endpoint(url=FIXTURE['endpoint']['url'], token=FIXTURE['endpoint']['token'])
            self.assertEqual(endpoint.url, FIXTURE['endpoint']['clean_url'])
            self.assertEqual(endpoint.token, 'header-secret')
            self.assertNotIn('secret', repr(endpoint))
            self.assertEqual(load_endpoint(url='wss://example.test/ws?token=a%2Bb').token, 'a+b')
            self.assertEqual(load_endpoint(url='wss://example.test/ws?token=fixture').port, 443)
            for url in ('http://example.test', 'ws://user:pass@example.test', 'ws://', 'ws://localhost:0', 'ws://localhost:70000', 'wss://x/#fragment'):
                with self.subTest(url=url), self.assertRaises(ValueError):
                    load_endpoint(url=url, token='fixture')

    def test_invalid_plugin_id_is_rejected_locally(self):
        for name in ('', 'with space', '中文', 'a' * 65):
            with self.assertRaises(ValueError):
                Plugin(name)


class WireParityTests(unittest.IsolatedAsyncioTestCase):
    async def test_reply_keeps_source_and_account_metadata(self):
        plugin = Plugin('test.reply')
        plugin.api = AsyncMock(return_value={'status':'queued'})
        event = Event({'event':'message.received','event_id':'source',
                       'account':{'id':'account','platform':'wechat'},'message':{'peer':'room'}})
        await plugin._context.reply(event, 'first', idempotency_key='explicit')
        await plugin._context.reply(event, 'second')
        options = plugin.api.await_args.args[1]['options']
        self.assertEqual(options['source_event_id'], 'source')
        self.assertEqual(options['idempotency_key'], 'reply:source:2')
        event.raw['account']['platform'] = 'telegram'
        plugin._action = AsyncMock(side_effect=PluginError('disconnected','fixture'))
        with self.assertRaises(PluginError) as raised:
            await plugin._context.reply(event, 'third')
        self.assertEqual(raised.exception.account_id, 'account')

    async def test_header_auth_error_ref_and_all_rpc_names(self):
        observed = []
        async def host(ws):
            # 两种公开服务端 API 分别对应 websockets 12/13 与 14/15。
            headers = ws.request.headers if hasattr(ws, 'request') else ws.request_headers
            path = ws.request.path if hasattr(ws, 'request') else ws.path
            self.assertEqual(headers['Authorization'], 'Bearer fixture')
            self.assertEqual(path, '/plugin?mode=1')
            await ws.recv()
            await ws.send(json.dumps({'type':'hello.ok','events':[]}))
            subscription = json.loads(await ws.recv())
            self.assertEqual(subscription['events'], [])
            await ws.send(json.dumps({'type':'subscribe.ok'}))
            async for raw in ws:
                frame = json.loads(raw)
                observed.append(frame['method'])
                if frame['method'] == 'reject':
                    reply = {'type':'error','ref':frame['req_id'],'code':'not_permitted','message':'fixture', 'call_id':'original','status':'unknown'}
                else:
                    reply = {'type':'api.result','req_id':frame['req_id'],'ok':True,'data':{'method':frame['method']}}
                await ws.send(json.dumps(reply))
        async with websockets.serve(host, '127.0.0.1', 0) as server:
            plugin = Plugin('test.wire', events=[], url=f'ws://127.0.0.1:{server.sockets[0].getsockname()[1]}/plugin?mode=1&token=fixture', request_timeout=2)
            async with plugin.connect():
                for method in FIXTURE['rpc']:
                    self.assertEqual(await plugin.api(method), {'method':method})
                with self.assertRaises(PluginError) as raised:
                    await plugin.api('reject', {'account_id':'wxid_fixture','options':{'idempotency_key':'original-key'}})
                self.assertEqual(raised.exception.code, 'not_permitted')
                self.assertEqual(raised.exception.call_id, 'original')
                self.assertEqual(raised.exception.status, 'unknown')
                self.assertEqual(raised.exception.idempotency_key, 'original-key')
        self.assertEqual(observed, FIXTURE['rpc'] + ['reject'])

    async def test_ack_failure_replay_skips_successful_callback_but_not_other_event_names(self):
        plugin = Plugin('test.ack')
        plugin._slots = asyncio.Semaphore(1)
        plugin._ws = AsyncMock()
        handler = AsyncMock()
        plugin.on('wechat.call.updated', handler)
        event = Event({'event':'wechat.call.updated','event_id':'same','cursor':1})
        plugin.api = AsyncMock(side_effect=PluginError('disconnected', 'fixture'))
        with self.assertLogs('lwplugin', level='ERROR'):
            await plugin._run_durable(event)
        handler.assert_awaited_once()
        # 代表重连后的新会话；成功回调缓存仍保留，但新会话错误状态清除。
        plugin._connection_error = None
        plugin.api = AsyncMock()
        await plugin._run_durable(event)
        handler.assert_awaited_once()
        plugin.api.assert_awaited_once_with('wechat.events.ack', {'cursor':1})
        other = AsyncMock()
        plugin.on('message.received', other)
        plugin._on_event({'event':'message.received','event_id':'same'})
        await asyncio.gather(*plugin._tasks)
        other.assert_awaited_once()

    async def test_durable_failure_never_acks_a_later_cursor(self):
        plugin = Plugin('test.order')
        plugin._slots = asyncio.Semaphore(2)
        plugin._ws = AsyncMock()
        plugin.log = AsyncMock()
        plugin.api = AsyncMock()
        handler = AsyncMock(side_effect=ValueError('fixture'))
        plugin.on('wechat.call.updated', handler)
        with self.assertLogs('lwplugin', level='ERROR'):
            await asyncio.gather(*(plugin._run_durable(Event({'event':'wechat.call.updated', 'event_id':str(n), 'cursor':n})) for n in (1, 2)))
        plugin.api.assert_not_awaited()
        handler.assert_awaited_once()

    async def test_filter_decides_before_ack_and_uses_event_scope(self):
        plugin = Plugin('test.filter')
        plugin.api = AsyncMock()
        @plugin.filter
        async def decide(event):
            self.assertEqual(plugin.calls(event.account_id).platform, 'wechat')
            return plugin.stop('fixture')
        await plugin._run_durable(Event({'event':MESSAGE_FILTER,'event_id':'f','cursor':3,'account':{'id':'wx','platform':'wechat'}}))
        self.assertEqual([c.args[0] for c in plugin.api.await_args_list], ['wechat.filters.decide','wechat.events.ack'])
        self.assertTrue(plugin.api.await_args_list[0].args[1]['stop'])
        with self.assertRaises(ValueError):
            plugin.on(MESSAGE_FILTER, lambda event: None)

    async def test_transport_write_failure_retains_key_without_resend(self):
        plugin = Plugin('test.write')
        plugin._send = AsyncMock(side_effect=OSError('fixture'))
        with self.assertRaises(PluginError) as raised:
            await plugin.wechat('wx').send_text('room', 'text', idempotency_key='same')
        self.assertEqual(raised.exception.idempotency_key, 'same')
        self.assertEqual(raised.exception.account_id, 'wx')
        plugin._send.assert_awaited_once()

    async def test_long_running_plugin_also_times_out_during_handshake(self):
        disconnected = []
        async def host(ws):
            await ws.recv()
            await ws.wait_closed()
        async with websockets.serve(host, '127.0.0.1', 0) as server:
            plugin = Plugin('test.handshake', events=[], reconnect=False,
                            url=f'ws://127.0.0.1:{server.sockets[0].getsockname()[1]}',
                            token='fixture', handshake_timeout=0.1)
            plugin.on_disconnect(lambda error: disconnected.append(error))
            with self.assertLogs('lwplugin', level='WARNING'):
                await asyncio.wait_for(plugin.run_async(), 2)
            self.assertEqual(disconnected[0].code, 'timeout')
            self.assertFalse(plugin.connected)

    async def test_close_interrupts_backoff_and_rejects_another_runner(self):
        plugin = Plugin('test.backoff')
        started = asyncio.Event()
        async def session():
            started.set()
            raise PluginError('disconnected', 'fixture')
        plugin._session = session
        with self.assertLogs('lwplugin', level='WARNING'):
            task = asyncio.create_task(plugin.run_async())
            await started.wait()
            with self.assertRaises(PluginError):
                await plugin.run_async()
            await plugin.close()
            await asyncio.wait_for(task, 0.5)
        self.assertFalse(plugin._running)

    async def test_event_handler_can_close_without_waiting_for_itself(self):
        async def host(ws):
            await ws.recv()
            await ws.send(json.dumps({'type':'hello.ok','events':['message.received']}))
            await ws.recv()
            await ws.send(json.dumps({'type':'subscribe.ok'}))
            await ws.send(json.dumps({'type':'event','event':'message.received','event_id':'close'}))
            await ws.wait_closed()
        async with websockets.serve(host, '127.0.0.1', 0) as server:
            plugin = Plugin('test.close', reconnect=False,
                            url=f'ws://127.0.0.1:{server.sockets[0].getsockname()[1]}', token='fixture')
            @plugin.on_message()
            async def receive(event):
                await plugin.close()
            await asyncio.wait_for(plugin.run_async(), 2)
            self.assertFalse(plugin.connected)
            self.assertFalse(plugin._tasks)


if __name__ == '__main__':
    unittest.main()
