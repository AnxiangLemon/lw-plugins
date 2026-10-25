"""lwplugin —— LW Tools WS 异步订阅插件的 Python SDK。"""

from .client import Context, Plugin, PluginError
from .endpoint import Endpoint, EndpointNotFound, load_endpoint
from .events import (
    ACCOUNT_OFFLINE,
    ACCOUNT_ONLINE,
    ALL_EVENTS,
    MESSAGE_RECEIVED,
    MESSAGE_SENT,
    WECHAT_MESSAGE_RECEIVED,
    WECHAT_MESSAGE_SENT,
    CALL_UPDATED,
    Event,
    at_segment,
    image_segment,
    voice_segment,
    video_segment,
    wechat_segment,
    text_segment,
)

__version__ = "2.1.0"

__all__ = [
    "ACCOUNT_OFFLINE",
    "ACCOUNT_ONLINE",
    "ALL_EVENTS",
    "MESSAGE_RECEIVED",
    "MESSAGE_SENT",
    "WECHAT_MESSAGE_RECEIVED",
    "WECHAT_MESSAGE_SENT",
    "CALL_UPDATED",
    "Context",
    "Endpoint",
    "EndpointNotFound",
    "Event",
    "Plugin",
    "PluginError",
    "at_segment",
    "image_segment",
    "load_endpoint",
    "voice_segment",
    "video_segment",
    "wechat_segment",
    "text_segment",
]
