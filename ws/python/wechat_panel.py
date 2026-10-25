"""WeChat business explorer: browser -> local HTTP -> lwplugin WS -> host.

Run `python ws/python/wechat_panel.py --demo` for an isolated mock host.
Real mode: `--endpoint-file C:/path/to/host-data/config/plugin-endpoint.json`.
"""
from __future__ import annotations

import argparse
import asyncio
from contextlib import suppress
import json
import logging
import math
from pathlib import Path
import time

from aiohttp import web
from jsonschema import Draft7Validator
from lwplugin import Plugin, PluginError

ASSETS = Path(__file__).with_suffix('')
CATALOG = json.loads((ASSETS / 'catalog.json').read_text(encoding='utf-8'))
METHODS = {entry['name']: entry for entry in CATALOG['methods']}
MAX_BODY = 32 * 1024 * 1024


def result_view(value):
    """Bounded display tree. Numbers are text so browsers cannot round i64 IDs."""
    remaining = 500
    def visit(item, depth=0):
        nonlocal remaining
        remaining -= 1
        if isinstance(item, (dict, list)):
            pairs = item.items() if isinstance(item, dict) else enumerate(item)
            children = []
            if depth < 8:
                for key, child in pairs:
                    if remaining <= 0 or len(children) >= 100: break
                    children.append({'key':str(key), **visit(child, depth+1)})
            return {'type':'object' if isinstance(item, dict) else 'array',
                    'count':len(item), 'children':children, 'truncated':len(children)<len(item)}
        kind = ('null' if item is None else 'boolean' if isinstance(item, bool)
                else 'number' if isinstance(item, (int,float)) else 'string')
        text = json.dumps(item,ensure_ascii=False) if kind != 'string' else item
        return {'type':kind, 'value':text[:400], 'truncated':len(text)>400}
    return visit(value)


def strict_json(text):
    def invalid(value):
        raise ValueError(f'JSON does not allow {value}')
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result: raise ValueError(f'Duplicate JSON field: {key}')
            result[key] = value
        return result
    def finite(value):
        number = float(value)
        if not math.isfinite(number): raise ValueError("JSON number must be finite")
        return number
    return json.loads(text, parse_constant=invalid, parse_float=finite, object_pairs_hook=unique)


def prepare(payload):
    """One validated request representation shared by preview and execution."""
    if not isinstance(payload, dict): raise ValueError('Request must be an object')
    name = payload.get('method')
    if not isinstance(name, str) or name not in METHODS:
        raise ValueError('Unknown or forbidden method')
    account = payload.get('account_id')
    if not isinstance(account, str) or not account.strip():
        raise ValueError('Select a WeChat account')
    text = payload.get('arguments')
    if not isinstance(text, str): raise ValueError('arguments must be JSON text')
    arguments = strict_json(text)
    entry = METHODS[name]
    errors = list(Draft7Validator(entry['schema']).iter_errors(arguments))
    if errors:
        error = errors[0]
        # Do not echo a possibly huge media payload in a validation error.
        where = '.'.join(map(str, error.absolute_path)) or 'arguments'
        raise ValueError(f'{where}: invalid {error.validator}; expected {error.validator_value}')
    obj = arguments if isinstance(arguments, dict) else arguments[0] if name == 'revoke' else {}
    if isinstance(obj, dict):
        for key in obj:
            if key.replace('_','').replace('-','').lower() in {
                'wxid','xwxid','authorization','accesstoken','refreshtoken','licensetoken',
            }:
                raise ValueError(f'Host-owned field is not allowed: {key}')
    mode=payload.get('mode','call')
    if mode not in ('call','submit'): raise ValueError('mode must be call or submit')
    key=payload.get('idempotency_key','')
    if not isinstance(key,str) or not key or len(key)>256: raise ValueError('idempotency_key is required (max 256)')
    snippet=f"wx = plugin.wechat({account!r})\nresult = await wx.{mode}({name!r}, {arguments!r}, idempotency_key={key!r})"
    return entry, account, arguments, snippet


class PanelPlugin(Plugin):
    def __init__(self, **kwargs):
        super().__init__('wechat.business.panel', title='微信业务接口面板', version='4.0.0',
                         events=['wechat.message.received'], **kwargs)
        self.ready = asyncio.Event()
        self.viewers = set()
        self.on('wechat.message.received', self.forward_message)

    async def forward_message(self, event):
        for viewer in tuple(self.viewers):
            try:
                await asyncio.wait_for(viewer.send_json(event.raw), timeout=2)
            except (ConnectionError, RuntimeError, asyncio.TimeoutError):
                self.viewers.discard(viewer)
                await viewer.close()

    async def _on_hello_ok(self, frame):
        await super()._on_hello_ok(frame)
        self.ready.set()

    async def _session(self):
        self.ready.clear()
        try:
            await super()._session()
        finally:
            self.ready.clear()


def supported_names(value):
    return {m['name'] for m in value.get('business', [])}


class Panel:
    def __init__(self, plugin, demo=False, demo_emit=None):
        self.plugin = plugin
        self.demo = demo
        self.demo_emit = demo_emit
        self.busy = False

    async def accounts(self):
        if not self.plugin.ready.is_set():
            raise PluginError('disconnected', '宿主尚未连接；请开启 WS 服务并检查 endpoint 文件。')
        items = await self.plugin.api('accounts.list')
        return [{k: a.get(k) for k in ('account_id','display_name','online','enabled')}
                for a in items if a.get('platform') == 'wechat' and a.get('account_id')]

    async def state(self, request):
        accounts = await self.accounts()
        account = request.query.get('account_id', '')
        if not any(a['account_id'] == account for a in accounts): account = ''
        supported = []
        if account and any(a['account_id'] == account for a in accounts):
            supported = sorted(supported_names(await self.plugin.wechat(account).methods()))
        return web.json_response({'connected':True, 'demo':self.demo, 'accounts':accounts,
                                  'supported':supported, 'account_id':account})

    async def events(self, request):
        viewer = web.WebSocketResponse(heartbeat=30)
        await viewer.prepare(request)
        self.plugin.viewers.add(viewer)
        try:
            if self.demo_emit:
                await self.demo_emit()
            async for _ in viewer:
                pass
        finally:
            self.plugin.viewers.discard(viewer)
        return viewer

    async def preview(self, request):
        entry, account, args, snippet = prepare(await request.json(loads=strict_json))
        arguments = args
        return web.json_response({'python':snippet, 'arguments':json.dumps(arguments,ensure_ascii=False,indent=2)})

    async def call(self, request):
        payload=await request.json(loads=strict_json)
        entry, account, args, _ = prepare(payload)
        if self.busy: raise PluginError('busy', '已有调用正在执行，请等待返回。')
        self.busy = True
        started = time.monotonic()
        try:
            accounts = await self.accounts()
            if not any(a['account_id'] == account and a['online'] for a in accounts):
                raise ValueError('请选择宿主中在线的微信账号')
            # The host is authoritative; a static catalog must never widen its allowlist.
            available = await self.plugin.wechat(account).methods()
            if entry['name'] not in supported_names(available):
                raise PluginError('unknown_method', '当前宿主没有开放该接口，请更新宿主或刷新目录。')
            wx=self.plugin.wechat(account)
            invoke=wx.submit if payload.get('mode')=='submit' else wx.call
            result=await invoke(entry['name'],args,idempotency_key=payload['idempotency_key'])
            return web.json_response({'ok':True, 'method':entry['name'], 'account_id':account,
                                      'elapsed_ms':round((time.monotonic()-started)*1000),
                                      'status':result['status'], 'call_id':result['call_id'],
                                      'demo':self.demo, 'view':result_view(result),
                                      'result_json':json.dumps(result,ensure_ascii=False,indent=2)})
        finally:
            self.busy = False

    async def receipt(self, request):
        payload=await request.json(loads=strict_json)
        wx=self.plugin.wechat(payload['account_id'])
        if payload.get('call_id'):
            value=await wx.calls.get(payload['call_id'])
        else:
            value=await wx.calls.find(payload['idempotency_key'])
        return web.json_response({'view':result_view(value),'result_json':json.dumps(value,ensure_ascii=False,indent=2),'status':value['status'] if value else 'not_found','call_id':value['call_id'] if value else None})


def create_app(panel):
    @web.middleware
    async def guard(request, handler):
        # Local browser UI only. No bearer token is exposed to JavaScript.
        host = request.host.split(':', 1)[0]
        if host not in ('127.0.0.1', 'localhost'):
            raise web.HTTPForbidden(text='Local panel only')
        origin = request.headers.get('Origin')
        if origin and origin != f'http://{request.host}':
            raise web.HTTPForbidden(text='Cross-origin request rejected')
        if request.method == 'POST' and (request.content_type != 'application/json'
                                        or request.headers.get('X-Panel-Request') != '1'):
            raise web.HTTPForbidden(text='Expected same-origin JSON panel request')
        try:
            response = await handler(request)
        except PluginError as error:
            response = web.json_response({'ok':False,'code':error.code,'message':error.message},status=502)
        except (ValueError, TypeError, KeyError, RecursionError) as error:
            response = web.json_response({'ok':False,'code':'bad_request','message':str(error)},status=400)
        except web.HTTPException:
            raise
        except Exception:
            logging.exception('Panel request failed')
            response = web.json_response({'ok':False,'code':'internal','message':'调用失败，请查看 Python 控制台。'},status=500)
        response.headers.update({'Cache-Control':'no-store', 'X-Content-Type-Options':'nosniff',
            'Content-Security-Policy':"default-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'"})
        return response

    app = web.Application(client_max_size=MAX_BODY, middlewares=[guard])
    async def static(request):
        return web.FileResponse(ASSETS / (request.match_info.get('file') or 'index.html'))
    async def catalog(request):
        return web.json_response({**CATALOG, 'demo':panel.demo})
    app.router.add_get('/', static)
    app.router.add_get('/{file:app.js|style.css}', static)
    app.router.add_get('/api/catalog', catalog)
    app.router.add_get('/api/state', panel.state)
    app.router.add_get('/api/events', panel.events)
    app.router.add_post('/api/preview', panel.preview)
    app.router.add_post('/api/call', panel.call)
    app.router.add_post('/api/receipt', panel.receipt)
    return app


async def serve(args):
    mock_runner = None
    if args.demo:
        from wechat_panel_mock import DEMO_EMIT_KEY, start_mock
        mock_runner, url = await start_mock(CATALOG, sample_message=True)
        # Fixture credentials exist only in memory; this never reads a real endpoint.
        plugin = PanelPlugin(url=url, token='panel-demo-only')
    else:
        plugin = PanelPlugin(endpoint_file=args.endpoint_file)
    task = asyncio.create_task(plugin.run_async())
    demo_emit = mock_runner.app[DEMO_EMIT_KEY] if mock_runner else None
    runner = web.AppRunner(create_app(Panel(plugin, demo=args.demo, demo_emit=demo_emit)), access_log=None)
    try:
        await runner.setup()
        site = web.TCPSite(runner, '127.0.0.1', args.port)
        await site.start()
        print(f'WeChat panel: http://127.0.0.1:{args.port} ({"MOCK" if args.demo else "HOST"})',flush=True)
        await asyncio.Event().wait()
    finally:
        await runner.cleanup()
        await plugin.close()
        task.cancel()
        with suppress(asyncio.CancelledError): await task
        if mock_runner: await mock_runner.cleanup()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--endpoint-file', help='宿主生成的 config/plugin-endpoint.json')
    parser.add_argument('--port', type=int, default=8765, help='本机 Web 面板端口（默认 8765）')
    parser.add_argument('--demo', action='store_true', help='独立 mock WS 宿主；不连接真实微信')
    args = parser.parse_args()
    if not 1 <= args.port <= 65535: parser.error('port must be between 1 and 65535')
    logging.basicConfig(level=logging.INFO, format='%(levelname)s %(message)s')
    # Avoid third-party debug logs containing the SDK-managed endpoint token.
    logging.getLogger('websockets').setLevel(logging.ERROR)
    try: asyncio.run(serve(args))
    except KeyboardInterrupt: pass
