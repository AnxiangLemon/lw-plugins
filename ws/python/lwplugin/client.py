"""WS client. WeChat v2 events replay after reconnect until acknowledged."""

from __future__ import annotations

import asyncio
import inspect
import json
import logging
import traceback
from uuid import uuid4
from collections import OrderedDict
from contextvars import ContextVar
from typing import Any, Awaitable, Callable, Dict, Iterable, List, Optional, Sequence, Set, Union

import websockets

from .endpoint import Endpoint, load_endpoint
from .wechat import Wechat, source_event
from .calls import Calls
from .events import ALL_EVENTS, MESSAGE_RECEIVED, Event, text_segment

LOGGER = logging.getLogger("lwplugin")

PROTOCOL_VERSION = 2

Handler = Callable[["Event", "Context"], Union[None, Awaitable[None]]]
Segments = Sequence[Dict[str, Any]]


class PluginError(RuntimeError):
    """宿主返回的错误（带协议错误码）。"""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(f"{code}: {message}")
        self.code = code
        self.message = message


class Context:
    """交给处理函数的宿主操作句柄。"""

    def __init__(self, plugin: "Plugin") -> None:
        self._plugin = plugin

    async def reply(
        self,
        event: Event,
        text: Optional[str] = None,
        *,
        segments: Optional[Segments] = None,
        idempotency_key: Optional[str] = None,
    ) -> Dict[str, Any]:
        """回复触发该事件的会话。微信走业务 API；通用平台走 action.reply。"""
        if segments is None:
            if text is None: raise ValueError("either text or segments is required")
            segments = [text_segment(text)]
        if event.platform and event.platform != "wechat":
            if not event.event_id:
                raise ValueError("reply requires event_id")
            event.raw["_reply_index"] = event.raw.get("_reply_index", 0) + 1
            return await self._plugin._action("reply", event_id=event.event_id, platform=event.platform, segments=segments,
                idempotency_key=idempotency_key or f"reply:{event.event_id}:{event.raw['_reply_index']}")
        if event.platform == "wechat":
            event.raw["_reply_index"] = event.raw.get("_reply_index", 0) + 1
            return await self._plugin.wechat(event.account_id).send(event.peer, segments,
                idempotency_key=idempotency_key or f"reply:{event.event_id}:{event.raw['_reply_index']}")
        raise ValueError(f"unsupported reply platform: {event.platform or '<missing>'}")

    def wechat(self, account_id):
        return self._plugin.wechat(account_id)

    def calls(self, account_id, *, platform=None):
        return self._plugin.calls(account_id, platform=platform)

    async def send(
        self,
        account_id: str,
        to: str,
        text: Optional[str] = None,
        *,
        segments: Optional[Segments] = None,
        platform: str = "wechat",
        idempotency_key: Optional[str] = None,
    ) -> Dict[str, Any]:
        """主动发送默认微信（兼容语义），不沿用事件平台；其他平台必须显式传 platform。"""
        return await self._plugin.send(account_id, to, text=text, segments=segments,
                                       platform=platform, idempotency_key=idempotency_key)

    async def api(self, method: str, params: Optional[Dict[str, Any]] = None) -> Any:
        return await self._plugin.api(method, params)

    async def log(self, message: str, level: str = "info") -> None:
        await self._plugin.log(message, level)


class Plugin:
    """WS 插件主体：连接、订阅、分发。"""

    def __init__(
        self,
        plugin_id: str,
        *,
        title: Optional[str] = None,
        version: str = "0.1.0",
        description: str = "",
        events: Optional[Iterable[str]] = None,
        event_filter: Optional[Dict[str, Any]] = None,
        url: Optional[str] = None,
        token: Optional[str] = None,
        endpoint_file: Optional[str] = None,
        reconnect: bool = True,
        dedupe_size: int = 2048,
        max_concurrency: int = 32,
        request_timeout: Optional[float] = 70.0,
    ) -> None:
        self.plugin_id = plugin_id
        self.title = title or plugin_id
        self.version = version
        self.description = description
        self.event_filter = event_filter
        self.reconnect = reconnect

        self._explicit_url = url
        self._explicit_token = token
        self._endpoint_file = endpoint_file
        self._request_timeout = None if request_timeout is None else max(request_timeout, 0.0)

        self._handlers: Dict[str, List[Handler]] = {}
        self._subscribed = list(events) if events is not None else None

        self.host_version = ""
        self.host_events: List[str] = []

        self._ws: Optional[Any] = None
        self._pending: Dict[str, asyncio.Future] = {}
        self._seq = 0
        self._seen: "OrderedDict[str, None]" = OrderedDict()
        self._dedupe_size = max(dedupe_size, 0)
        self._context = Context(self)
        self._event_platform = ContextVar(f"lw_platform_{plugin_id}", default="telegram")
        self._closing = False
        self._max_concurrency = max(max_concurrency, 1)
        self._slots: Optional[asyncio.Semaphore] = None
        self._tasks: Set[asyncio.Task] = set()
        self._durable_lock = asyncio.Lock()
        self._filters = []
        self.host_capabilities = frozenset()

    def wechat(self, account_id):
        return Wechat(self, account_id)

    def calls(self, account_id, *, platform=None):
        return Calls(self, account_id, self._event_platform.get() if platform is None else platform)

    def filter(self, handler):
        """Explicit, bounded inbound prefilter. Return plugin.stop(reason) to stop dispatch."""
        self._filters.append(handler)
        return handler

    @staticmethod
    def stop(reason=""):
        return {"stop": True, "reason": reason}

    # ── 注册处理函数 ──

    def on(
        self,
        event_name: str = MESSAGE_RECEIVED,
        handler: Optional[Handler] = None,
    ) -> Union["Plugin", Callable[[Handler], Handler]]:
        """注册事件处理函数，可叠加多个。

        直接传处理函数时返回自身以便链式调用，不传时当装饰器用：

            plugin.on("account.online", on_online).run()

            @plugin.on("account.online")
            async def on_online(event, ctx): ...
        """
        if event_name not in ALL_EVENTS:
            raise ValueError(f"unknown event: {event_name}")

        if handler is not None:
            self._handlers.setdefault(event_name, []).append(handler)
            return self

        def decorator(func: Handler) -> Handler:
            self._handlers.setdefault(event_name, []).append(func)
            return func

        return decorator

    def on_message(
        self, handler: Optional[Handler] = None
    ) -> Union["Plugin", Callable[[Handler], Handler]]:
        """注册 `message.received` 处理函数，语义同 `on`。"""
        return self.on(MESSAGE_RECEIVED, handler)

    # ── 运行 ──

    def run(self) -> None:
        """阻塞运行，Ctrl-C 退出。"""
        try:
            asyncio.run(self.run_async())
        except KeyboardInterrupt:
            LOGGER.info("plugin stopped by user")

    async def run_async(self) -> None:
        backoff = 1.0
        while not self._closing:
            try:
                await self._session()
                backoff = 1.0
            except Exception as exc:  # noqa: BLE001 - 顶层循环必须吞掉一切以便重连
                if self._closing:
                    break
                LOGGER.warning("connection lost: %s", exc)
            if not self.reconnect or self._closing:
                break
            LOGGER.info("reconnecting in %.0fs", backoff)
            await asyncio.sleep(backoff)
            backoff = min(backoff * 2, 30.0)

    async def close(self) -> None:
        self._closing = True
        if self._ws is not None:
            await self._ws.close()
            self._ws = None
        await self._drain_tasks()

    async def _drain_tasks(self) -> None:
        """断线后取消在飞的处理任务：它们的 reply/api 已经无处投递。"""
        tasks = list(self._tasks)
        self._tasks.clear()
        for task in tasks:
            task.cancel()
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)

    # ── 会话 ──

    async def _session(self) -> None:
        endpoint: Endpoint = load_endpoint(
            url=self._explicit_url,
            token=self._explicit_token,
            endpoint_file=self._endpoint_file,
        )
        self._slots = asyncio.Semaphore(self._max_concurrency)
        # 令牌走查询串而不是 Authorization 头：不同 websockets 版本的
        # 自定义头参数名不一致，查询串在所有版本上都能用。
        try:
            async with websockets.connect(
                endpoint.url_with_token(), max_size=32 * 1024 * 1024
            ) as ws:
                self._ws = ws
                self._fail_pending("connection restarted")
                hello: Dict[str, Any] = {
                    "v": PROTOCOL_VERSION,
                    "type": "hello",
                    "plugin_id": self.plugin_id,
                    "title": self.title,
                    "version": self.version,
                    "description": self.description,
                }
                await self._send(hello)
                async for raw in ws:
                    if isinstance(raw, bytes):
                        continue
                    await self._dispatch_frame(json.loads(raw))
        finally:
            self._ws = None
            self._fail_pending("connection closed")
            await self._drain_tasks()

    async def _dispatch_frame(self, frame: Dict[str, Any]) -> None:
        kind = frame.get("type")
        if kind == "hello.ok":
            await self._on_hello_ok(frame)
        elif kind == "event":
            self._on_event(frame)
        elif kind == "api.result":
            self._resolve_pending(frame)
        elif kind == "action.ack":
            self._resolve_pending(frame)
        elif kind == "subscribe.ok":
            LOGGER.info("subscribed: %s", frame.get("events"))
        elif kind == "error":
            LOGGER.warning("host error [%s] %s", frame.get("code"), frame.get("message"))
        elif kind == "bye":
            LOGGER.info("host closed connection: %s", frame.get("reason"))
        else:
            LOGGER.debug("ignoring unknown frame: %s", kind)

    async def _on_hello_ok(self, frame: Dict[str, Any]) -> None:
        self.host_capabilities = frozenset(frame.get("capabilities") or [])
        self.host_version = frame.get("host_version", "")
        self.host_events = list(frame.get("events") or [])
        LOGGER.info("connected to host %s", self.host_version)
        # 宿主默认只订上 message.received，其他事件必须显式订阅。
        # 没有任何处理函数时不发 subscribe：这样保留宿主默认订阅。
        events = list(self._subscribed) if self._subscribed is not None else list(self._handlers)
        if self._filters: events.append("wechat.message.filter")
        if self._subscribed is None and not events:
            LOGGER.warning("no handlers registered; nothing will be subscribed")
            return
        payload: Dict[str, Any] = {"v": PROTOCOL_VERSION, "type": "subscribe", "events": events}
        if self.event_filter:
            payload["filter"] = self.event_filter
        await self._send(payload)

    def _on_event(self, frame: Dict[str, Any]) -> None:
        """把事件交给后台任务。

        绝对不能在这里 await 用户的处理函数：`ctx.reply` / `ctx.api` 的结果要靠
        接收循环读回来，处理函数一旦占住这个循环就会自己把自己饿死。
        """
        if len(self._tasks) >= 512:
            if self._ws is not None: asyncio.create_task(self._ws.close())
            return
        event = Event(frame)
        if frame.get("cursor") is not None:
            task = asyncio.create_task(self._run_durable(event))
        else:
            if self._is_duplicate(event.event_id): return
            task = asyncio.create_task(self._run_handlers(event, self._handlers.get(event.name, [])))
        self._tasks.add(task)
        task.add_done_callback(self._tasks.discard)

    async def _run_durable(self, event):
        # Acknowledge in delivery order; never advance past an unfinished callback.
        async with self._durable_lock:
            marker = source_event.set(event.event_id)
            try:
                if event.name == "wechat.message.filter":
                    decision = None
                    for handler in self._filters:
                        decision = handler(event)
                        if inspect.isawaitable(decision): decision = await decision
                        if isinstance(decision, dict) and decision.get("stop"): break
                    await self.api("wechat.filters.decide", {"event_id": event.event_id, "stop": bool(isinstance(decision, dict) and decision.get("stop"))})
                elif event.event_id not in self._seen:
                    await self._run_handlers(event, self._handlers.get(event.name, []))
                await self.api("wechat.events.ack", {"cursor": event.raw["cursor"]})
                self._is_duplicate(event.event_id)
            except asyncio.CancelledError:
                raise
            except Exception:
                LOGGER.exception("durable event failed; reconnect will replay %s", event.event_id)
                if self._ws is not None: await self._ws.close()
            finally:
                source_event.reset(marker)

    async def _run_handlers(self, event: Event, handlers: List[Handler]) -> None:
        assert self._slots is not None
        # 并发上限只是自保：真正的背压在宿主侧（队列满了先丢事件再断连）
        async with self._slots:
            for handler in handlers:
                platform_marker = self._event_platform.set(event.platform)
                try:
                    parameters = inspect.signature(handler).parameters
                    result = handler(event) if len(parameters) == 1 else handler(event, self._context)
                    if inspect.isawaitable(result):
                        await result
                except asyncio.CancelledError:
                    raise
                except Exception:  # noqa: BLE001 - 单个处理函数出错不该断连
                    diagnostic = (
                        f"WS 插件事件处理失败 plugin_id={self.plugin_id} "
                        f"event={event.name} event_id={event.event_id} "
                        f"account_id={event.account_id} handler={getattr(handler, '__qualname__', type(handler).__name__)}"
                    )
                    error = traceback.format_exc()
                    LOGGER.error("%s\n%s", diagnostic, error)
                    try:
                        # 同步到宿主日志，方便从插件页统一排障；不影响后续 handler。
                        await self.log(f"{diagnostic}\n{error}", level="error")
                    except Exception:  # noqa: BLE001 - 连接已断时仍保留本地 traceback
                        LOGGER.debug("插件异常日志未能送达宿主 plugin_id=%s", self.plugin_id)
                    if event.raw.get("cursor") is not None: raise
                finally:
                    self._event_platform.reset(platform_marker)

    def _is_duplicate(self, event_id: str) -> bool:
        if not event_id or self._dedupe_size == 0:
            return False
        if event_id in self._seen:
            return True
        self._seen[event_id] = None
        while len(self._seen) > self._dedupe_size:
            self._seen.popitem(last=False)
        return False

    # ── 出站 ──

    async def send(
        self,
        account_id: str,
        to: str,
        text: Optional[str] = None,
        *,
        segments: Optional[Segments] = None,
        platform: str = "wechat",
        idempotency_key: Optional[str] = None,
    ) -> Dict[str, Any]:
        """主动发送默认微信，即使位于其他平台回调中也不变；多平台发送须显式传 platform。"""
        if segments is None:
            if text is None: raise ValueError("either text or segments is required")
            segments = [text_segment(text)]
        if platform == "wechat":
            options = {} if idempotency_key is None else {"idempotency_key": idempotency_key}
            return await self.wechat(account_id).send(to, segments, **options)
        if platform:
            if not account_id or not to:
                raise ValueError("send requires account_id and to")
            return await self._action("send", account_id=account_id, to=to,
                                      platform=platform, segments=segments,
                                      idempotency_key=idempotency_key or uuid4().hex)
        raise ValueError(f"unsupported send platform: {platform}")

    async def _action(self, kind: str, *, segments: Segments, **target: str) -> Dict[str, Any]:
        target.setdefault("idempotency_key", uuid4().hex)
        action_id = self._next_id("act")
        frame = {"v": PROTOCOL_VERSION, "type": "action", "action_id": action_id,
                 "kind": kind, "segments": list(segments), **target}
        try:
            ack = await self._request(action_id, frame)
        except PluginError as exc:
            exc.idempotency_key = target["idempotency_key"]
            exc.account_id = target.get("account_id")
            raise
        if not ack.get("ok"):
            exc = PluginError(ack.get("code") or "internal", ack.get("message") or "action failed")
            exc.call_id, exc.status = ack.get("call_id"), ack.get("status")
            exc.idempotency_key = target["idempotency_key"]
            exc.account_id = target.get("account_id")
            raise exc
        return ack

    async def api(self, method: str, params: Optional[Dict[str, Any]] = None) -> Any:
        req_id = self._next_id("req")
        frame = {
            "v": PROTOCOL_VERSION,
            "type": "api.call",
            "req_id": req_id,
            "method": method,
            "params": params or {},
        }
        try:
            result = await self._request(req_id, frame)
        except PluginError as exc:
            exc.idempotency_key = (params or {}).get("options", {}).get("idempotency_key")
            exc.account_id = (params or {}).get("account_id")
            raise
        if not result.get("ok"):
            raise PluginError(
                result.get("code") or "internal", result.get("message") or "api failed"
            )
        return result.get("data")

    async def log(self, message: str, level: str = "info") -> None:
        await self._send(
            {"v": PROTOCOL_VERSION, "type": "log", "level": level, "message": message}
        )

    # ── 底层 ──

    async def _request(self, req_id: str, frame: Dict[str, Any]) -> Dict[str, Any]:
        loop = asyncio.get_running_loop()
        future: asyncio.Future = loop.create_future()
        self._pending[req_id] = future
        try:
            await self._send(frame)
            if self._request_timeout is None:
                return await future
            return await asyncio.wait_for(future, timeout=self._request_timeout)
        except asyncio.TimeoutError as exc:
            future.cancel()
            raise PluginError(
                "timeout",
                f"request {req_id} timed out after {self._request_timeout:g}s",
            ) from exc
        finally:
            self._pending.pop(req_id, None)

    def _resolve_pending(self, frame: Dict[str, Any]) -> None:
        key = frame.get("req_id") or frame.get("action_id")
        if not key:
            return
        future = self._pending.get(key)
        if future is not None and not future.done():
            future.set_result(frame)

    def _fail_pending(self, reason: str) -> None:
        for future in self._pending.values():
            if not future.done():
                future.set_exception(PluginError("internal", reason))
        self._pending.clear()

    async def _send(self, frame: Dict[str, Any]) -> None:
        if self._ws is None:
            raise PluginError("internal", "not connected")
        await self._ws.send(json.dumps(frame, ensure_ascii=False))

    def _next_id(self, prefix: str) -> str:
        self._seq += 1
        return f"{prefix}-{self._seq}"
