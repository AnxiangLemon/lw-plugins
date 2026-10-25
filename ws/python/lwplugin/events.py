"""事件对象 —— 在原始 JSON 外包一层便利访问。

刻意保留 `raw`：协议会加字段，SDK 不该成为瓶颈。
"""

from __future__ import annotations

from typing import Any, Dict, List, Optional

MESSAGE_RECEIVED = "message.received"
MESSAGE_SENT = "message.sent"
WECHAT_MESSAGE_RECEIVED = "wechat.message.received"
WECHAT_MESSAGE_SENT = "wechat.message.sent"
ACCOUNT_ONLINE = "account.online"
ACCOUNT_OFFLINE = "account.offline"

CALL_UPDATED = "wechat.call.updated"
ALL_EVENTS = (
    MESSAGE_RECEIVED, MESSAGE_SENT, WECHAT_MESSAGE_RECEIVED,
    WECHAT_MESSAGE_SENT, CALL_UPDATED, ACCOUNT_ONLINE, ACCOUNT_OFFLINE,
)


class Event:
    """一条宿主事件。"""

    __slots__ = ("raw",)

    def __init__(self, raw: Dict[str, Any]) -> None:
        self.raw = raw

    # ── 通用字段 ──
    @property
    def name(self) -> str:
        return self.raw.get("event", "")

    @property
    def event_id(self) -> str:
        return self.raw.get("event_id", "")

    @property
    def trace_id(self) -> str:
        return self.raw.get("trace_id", "")

    @property
    def received_at(self) -> int:
        return int(self.raw.get("received_at") or 0)

    @property
    def platform(self) -> str:
        return (self.raw.get("account") or {}).get("platform", "")

    @property
    def account_id(self) -> str:
        return (self.raw.get("account") or {}).get("id", "")

    # ── message.received ──
    @property
    def message(self) -> Dict[str, Any]:
        return self.raw.get("message") or {}

    @property
    def text(self) -> str:
        """由 segments 派生的纯文本，非文本消息为占位描述。"""
        return self.message.get("text", "")

    @property
    def segments(self) -> List[Dict[str, Any]]:
        return self.message.get("segments") or []

    @property
    def msg_id(self) -> str:
        return self.message.get("msg_id", "")

    @property
    def peer(self) -> str:
        """会话标识：群聊为群 ID，私聊为对方 ID。"""
        return self.message.get("peer_id", self.message.get("peer", ""))

    @property
    def is_group(self) -> bool:
        return bool(self.message.get("is_group"))

    @property
    def chat_type(self) -> str:
        return self.message.get("chat_type", "")

    @property
    def sender_id(self) -> str:
        return (self.message.get("from") or {}).get("id", "")

    # ── message.sent ──
    @property
    def delivery(self) -> Dict[str, Any]:
        return self.raw.get("delivery") or {}

    # ── account.online / offline ──
    @property
    def status(self) -> Dict[str, Any]:
        return self.raw.get("status") or {}

    @property
    def is_online(self) -> Optional[bool]:
        if self.name == ACCOUNT_ONLINE:
            return True
        if self.name == ACCOUNT_OFFLINE:
            return False
        return None

    def __repr__(self) -> str:
        return f"<Event {self.name} id={self.event_id!r} account={self.account_id!r}>"


def text_segment(text: str) -> Dict[str, Any]:
    return {"type": "text", "data": {"text": text}}


def at_segment(target_id: str) -> Dict[str, Any]:
    return {"type": "at", "data": {"target_id": target_id}}


def image_segment(base64: str) -> Dict[str, Any]:
    return {"type": "image", "data": {"source": {"kind": "base64", "base64": base64}}}


def voice_segment(base64: str, voice_type: int, voice_time: int) -> Dict[str, Any]:
    return {"type":"voice","data":{"base64":base64,"type":voice_type,"voiceTime":voice_time}}

def video_segment(base64: str, image_base64: str, play_length: int) -> Dict[str, Any]:
    return {"type":"video","data":{"base64":base64,"imageBase64":image_base64,"playLength":play_length}}

def wechat_segment(method: str, params: Dict[str, Any]) -> Dict[str, Any]:
    return {"type":"wechat","data":{"method":method,"params":params}}
