"""Telegram Bot API 同步薄封装：复用 WS 连接，远程安全传文件（base64）。"""

from __future__ import annotations

import asyncio
from base64 import b64decode, b64encode
from os import PathLike
from pathlib import Path
from typing import Any, Dict, Optional, Union

from .wechat import Result, result


def telegram_upload_bytes(
    data: bytes, filename: str = "", mime_type: str = ""
) -> Dict[str, Any]:
    """构造内联 Base64 上传对象（远程安全，不传递本地路径给宿主）。"""
    payload: Dict[str, Any] = {"base64": b64encode(data).decode("ascii")}
    if filename:
        payload["filename"] = filename
    if mime_type:
        payload["mime_type"] = mime_type
    return payload


def telegram_upload_file(
    path: Union[str, PathLike], mime_type: str = ""
) -> Dict[str, Any]:
    """读取插件本机文件并编码为内联上传对象。"""
    p = Path(path)
    return telegram_upload_bytes(p.read_bytes(), filename=p.name, mime_type=mime_type)


class Telegram:
    """绑定单个 Telegram Bot 账号的调用句柄。"""

    def __init__(self, plugin, account_id: str):
        if not account_id:
            raise ValueError("account_id is required")
        self.plugin = plugin
        self.account_id = account_id

    async def call(self, method: str, params: Optional[Dict[str, Any]] = None) -> Any:
        """调用任意 Telegram Bot API 方法（支持 camelCase 与 snake_case）。"""
        if not method:
            raise ValueError("method is required")
        res = await self.plugin.api(
            "telegram.call",
            {
                "account_id": self.account_id,
                "method": method,
                "params": params or {},
            },
        )
        return result(res)

    async def download_file(self, file_id: str) -> Result:
        """按 file_id 下载 Telegram 文件，返回含 base64 / file_path / mime_type 的 Result。"""
        if not file_id:
            raise ValueError("file_id is required")
        res = await self.plugin.api(
            "telegram.download_file",
            {"account_id": self.account_id, "file_id": file_id},
        )
        return result(res)

    async def download_bytes(self, file_id: str) -> bytes:
        info = await self.download_file(file_id)
        return b64decode(info["base64"])

    async def send_message(self, chat_id: Union[int, str], text: str, **extra: Any) -> Any:
        return await self.call("sendMessage", {"chat_id": chat_id, "text": text, **extra})

    async def send_photo(self, chat_id: Union[int, str], photo: Any, **extra: Any) -> Any:
        return await self.call("sendPhoto", {"chat_id": chat_id, "photo": photo, **extra})

    async def send_photo_file(
        self, chat_id: Union[int, str], path: Union[str, PathLike], **extra: Any
    ) -> Any:
        upload = await asyncio.to_thread(telegram_upload_file, path)
        return await self.send_photo(chat_id, upload, **extra)

    async def send_document(self, chat_id: Union[int, str], document: Any, **extra: Any) -> Any:
        return await self.call("sendDocument", {"chat_id": chat_id, "document": document, **extra})

    async def send_voice(self, chat_id: Union[int, str], voice: Any, **extra: Any) -> Any:
        return await self.call("sendVoice", {"chat_id": chat_id, "voice": voice, **extra})

    async def answer_callback_query(
        self, callback_query_id: str, text: str = "", show_alert: bool = False
    ) -> Any:
        params: Dict[str, Any] = {"callback_query_id": callback_query_id}
        if text:
            params["text"] = text
        if show_alert:
            params["show_alert"] = True
        return await self.call("answerCallbackQuery", params)

    async def edit_message_text(
        self, chat_id: Union[int, str], message_id: int, text: str, **extra: Any
    ) -> Any:
        return await self.call(
            "editMessageText",
            {"chat_id": chat_id, "message_id": message_id, "text": text, **extra},
        )

    async def delete_message(self, chat_id: Union[int, str], message_id: int) -> Any:
        return await self.call("deleteMessage", {"chat_id": chat_id, "message_id": message_id})

    async def set_my_commands(self, commands: list, **extra: Any) -> Any:
        return await self.call("setMyCommands", {"commands": commands, **extra})

    async def restrict_chat_member(
        self,
        chat_id: Union[int, str],
        user_id: int,
        permissions: Dict[str, Any],
        until_date: int = 0,
    ) -> Any:
        params: Dict[str, Any] = {
            "chat_id": chat_id,
            "user_id": user_id,
            "permissions": permissions,
        }
        if until_date > 0:
            params["until_date"] = until_date
        return await self.call("restrictChatMember", params)

    async def ban_chat_member(
        self,
        chat_id: Union[int, str],
        user_id: int,
        until_date: int = 0,
        revoke_messages: bool = False,
    ) -> Any:
        params: Dict[str, Any] = {"chat_id": chat_id, "user_id": user_id}
        if until_date > 0:
            params["until_date"] = until_date
        if revoke_messages:
            params["revoke_messages"] = True
        return await self.call("banChatMember", params)

    async def approve_chat_join_request(self, chat_id: Union[int, str], user_id: int) -> Any:
        return await self.call("approveChatJoinRequest", {"chat_id": chat_id, "user_id": user_id})

    async def decline_chat_join_request(self, chat_id: Union[int, str], user_id: int) -> Any:
        return await self.call("declineChatJoinRequest", {"chat_id": chat_id, "user_id": user_id})
