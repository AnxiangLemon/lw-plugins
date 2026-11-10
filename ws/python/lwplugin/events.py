"""事件对象 —— 在原始 JSON 外包一层便利访问。

刻意保留 `raw`：协议会加字段，SDK 不该成为瓶颈。
"""

from __future__ import annotations

from base64 import b64encode
from dataclasses import dataclass
from os import PathLike
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple, Union

MESSAGE_RECEIVED = "message.received"
MESSAGE_SENT = "message.sent"
WECHAT_MESSAGE_RECEIVED = "wechat.message.received"
WECHAT_MESSAGE_SENT = "wechat.message.sent"
ACCOUNT_ONLINE = "account.online"
ACCOUNT_OFFLINE = "account.offline"

MESSAGE_FILTER = "wechat.message.filter"
CALL_UPDATED = "wechat.call.updated"
GROUP_REFRESH_HINT = "wechat.group.refresh_hint"
SYNC_HINT = "wechat.sync.hint"
ALL_EVENTS = (
    GROUP_REFRESH_HINT, SYNC_HINT, MESSAGE_FILTER,
    MESSAGE_RECEIVED, MESSAGE_SENT, WECHAT_MESSAGE_RECEIVED,
    WECHAT_MESSAGE_SENT, CALL_UPDATED, ACCOUNT_ONLINE, ACCOUNT_OFFLINE,
)


@dataclass(frozen=True)
class HostInfo:
    """本次连接的宿主能力快照，对齐 Go HostInfo，不允许回调修改订阅能力。"""
    version: str
    events: Tuple[str, ...]
    capabilities: Tuple[str, ...]


@dataclass(frozen=True)
class GroupHint:
    """群资料核对线索，不代表已确认成员加入或退出。

    可选字段缺失保持 None，不能把缺失版本当成 0，也不能把 erase_member_data
    当作全员退出。实际净变化需要插件比较两份有效完整成员名单。
    """
    room_id: str = ""
    reason: str = ""
    chatroom_version: Optional[int] = None
    chatroom_info_version: Optional[int] = None
    erase_member_data: Optional[int] = None
    info_mask: Optional[int] = None

    @classmethod
    def from_data(cls, data: Dict[str, Any]) -> "GroupHint":
        return cls(**{name: data[name] for name in cls.__dataclass_fields__ if name in data})


@dataclass(frozen=True)
class SyncMetadata:
    """账号级弱提示；selector 保留原始有符号值，不能将其直接解释为退群。"""
    source: str = ""
    selector: Optional[int] = None
    triggered_at: str = ""
    continue_flag: Optional[int] = None

    @classmethod
    def from_data(cls, data: Dict[str, Any]) -> "SyncMetadata":
        return cls(**{name: data[name] for name in cls.__dataclass_fields__ if name in data})


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
    def data(self) -> Dict[str, Any]:
        """非消息事件的只读业务提示，例如群资料刷新；不代表已确认进退群。"""
        return self.raw.get("data") or {}

    @property
    def group_hint(self) -> Optional[GroupHint]:
        """仅群资料提示返回 DTO；其他事件返回 None，原始字段仍由 data/raw 保留。"""
        return GroupHint.from_data(self.data) if self.name == GROUP_REFRESH_HINT else None

    @property
    def sync_metadata(self) -> Optional[SyncMetadata]:
        return SyncMetadata.from_data(self.data) if self.name == SYNC_HINT else None

    @property
    def cursor(self) -> Optional[int]:
        """只有持久化事件才有 cursor；两个群监控 Hint 没有补发游标。"""
        return self.raw.get("cursor")

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
        return self.message.get("peer_id") or self.message.get("peer", "")

    @property
    def is_group(self) -> bool:
        return bool(self.message.get("is_group"))

    @property
    def chat_type(self) -> str:
        return self.message.get("chat_type", "")

    @property
    def sender_id(self) -> str:
        return (self.message.get("from") or {}).get("id", "")

    @property
    def result(self) -> Dict[str, Any]:
        """wechat.call.updated 的原始业务回执；未知字段完整保留。"""
        return self.raw.get("result") or {}

    @property
    def call_id(self) -> str:
        return self.raw.get("call_id") or self.result.get("call_id", "")

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
    """构造文本消息段，不发送网络请求。"""
    return {"type": "text", "data": {"text": text}}


def at_segment(target_id: str) -> Dict[str, Any]:
    """构造提及消息段，特殊目标 ID 的支持范围由平台决定。"""
    return {"type": "at", "data": {"target_id": target_id}}


def image_segment(base64: str) -> Dict[str, Any]:
    """传入图片 Base64 内容，不是 URL 或文件路径。"""
    return {"type": "image", "data": {"source": {"kind": "base64", "base64": base64}}}


def image_file_segment(path: Union[str, PathLike]) -> Dict[str, Any]:
    """读取插件所在机器上的图片，生成当前宿主支持的 Base64 消息段。

    文件路径不会发给宿主，因此插件和 Desktop 可以运行在不同机器。
    此函数只编码原始字节，不压缩、不转码；图片格式仍由服务端校验。
    不接受 URL，文件不存在、不可读或为空时在提交前报错。
    """
    content = Path(path).read_bytes()
    if not content:
        raise ValueError("image file is empty")
    return image_segment(b64encode(content).decode("ascii"))


def voice_segment(base64: str, voice_type: int, voice_time: int) -> Dict[str, Any]:
    """语音类型和时长单位遵循当前服务端文档，SDK 不转码。"""
    return {"type":"voice","data":{"base64":base64,"type":voice_type,"voiceTime":voice_time}}

def video_segment(base64: str, image_base64: str, play_length: Optional[int] = None) -> Dict[str, Any]:
    """传视频和封面内容；旧 play_length 参数保留兼容，但不再发送给服务。"""
    return {"type":"video","data":{"base64":base64,"imageBase64":image_base64}}

def wechat_segment(method: str, params: Dict[str, Any]) -> Dict[str, Any]:
    """构造高级微信方法消息段，具体方法和参数仍由宿主校验。"""
    return {"type":"wechat","data":{"method":method,"params":params}}


def file_segment(base64: str, file_name: str) -> Dict[str, Any]:
    """传文件 Base64 内容和展示文件名，不依赖宿主本地路径。"""
    return {"type":"file","data":{"base64":base64,"fileName":file_name}}
