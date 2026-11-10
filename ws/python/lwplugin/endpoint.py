"""端点发现：定位宿主写出的 `config/plugin-endpoint.json`。

优先级：显式参数 > 环境变量 > 发现文件。硬编码端口是最后的手段，
因为宿主允许把 `plugin_ws.port` 配成 0 由系统分配。
"""

from __future__ import annotations

import json
import os
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Iterator, Optional
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

ENDPOINT_FILENAME = "plugin-endpoint.json"

# 保留旧常量供调用方引用；完整 ws/wss URL 未写端口时按标准使用 80/443。
DEFAULT_PORT = 26810

ENV_ENDPOINT_FILE = "LW_PLUGIN_ENDPOINT_FILE"
ENV_URL = "LW_PLUGIN_URL"
ENV_TOKEN = "LW_PLUGIN_TOKEN"


class EndpointNotFound(RuntimeError):
    """找不到端点信息，通常是宿主没开 WS 插件服务。"""


@dataclass(frozen=True)
class Endpoint:
    host: str
    port: int
    token: str = field(repr=False)
    address: Optional[str] = None

    @property
    def url(self) -> str:
        if self.address:
            return self.address
        host = f"[{self.host}]" if ":" in self.host else self.host
        return f"ws://{host}:{self.port}/"

    def url_with_token(self) -> str:
        """旧调用方兼容入口；SDK 连接改用 Authorization，不再把 token 放入 URL。"""
        parts = urlsplit(self.url)
        query = parse_qsl(parts.query, keep_blank_values=True) + [("token", self.token)]
        return urlunsplit(parts._replace(query=urlencode(query)))


def _candidate_files(explicit: Optional[str]) -> Iterator[Path]:
    if explicit:
        yield Path(explicit)
        return
    env_file = os.environ.get(ENV_ENDPOINT_FILE)
    if env_file:
        yield Path(env_file)
        return
    # 与 Go 插件的“程序旁边放端点文件”体验保持一致。这里找的是入口脚本，
    # 不是 python.exe 所在目录；-c/-m 等没有真实脚本路径时仅使用工作目录。
    if sys.argv and sys.argv[0] and not sys.argv[0].startswith("-"):
        script = Path(sys.argv[0]).resolve().parent
        yield script / ENDPOINT_FILENAME
        yield script / "config" / ENDPOINT_FILENAME
    # 从当前目录逐级向上找 config/plugin-endpoint.json，
    # 这样插件放在仓库任意子目录都能跑起来
    here = Path.cwd().resolve()
    for directory in [here, *here.parents]:
        yield directory / ENDPOINT_FILENAME
        yield directory / "config" / ENDPOINT_FILENAME


def load_endpoint(
    *,
    url: Optional[str] = None,
    token: Optional[str] = None,
    endpoint_file: Optional[str] = None,
) -> Endpoint:
    """解析连接信息。缺任何一项都会去发现文件里补。"""
    url = url or os.environ.get(ENV_URL)
    token = token or os.environ.get(ENV_TOKEN)

    # URL 自带 token 仍可导入，但返回的地址不再含凭据；显式 token 优先。
    if url:
        candidate = _checked_endpoint(url, token or "", require_token=False)
        token = candidate.token
        if token:
            return candidate

    for path in _candidate_files(endpoint_file):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
            port = data.get("port")
            if isinstance(port, bool) or not isinstance(port, int) or not 1 <= port <= 65535:
                continue
            host = data.get("host") or "127.0.0.1"
            host = f"[{host}]" if ":" in host and not host.startswith("[") else host
            address = url or f"ws://{host}:{port}/"
            return _checked_endpoint(address, token or data.get("token") or "")
        except (OSError, ValueError, AttributeError, TypeError):
            continue

    raise EndpointNotFound(
        "未找到有效 plugin-endpoint.json。请在应用「插件 → WS 插件」中启动服务，"
        f"或设置环境变量 {ENV_URL} 与 {ENV_TOKEN}。"
    )


def _checked_endpoint(address: str, token: str, *, require_token=True) -> Endpoint:
    """保留 wss、IPv6、路径与非凭据查询参数，拒绝账号密码 URL 和无效端口。"""
    try:
        parts = urlsplit(address)
        port = parts.port
        if (parts.scheme not in ("ws", "wss") or not parts.hostname or any(c.isspace() for c in parts.hostname)
                or parts.username is not None or parts.password is not None or parts.fragment
                or (port is not None and not 1 <= port <= 65535)):
            raise ValueError()
    except ValueError:
        raise ValueError("plugin URL must be a valid ws:// or wss:// address") from None
    query = parse_qsl(parts.query, keep_blank_values=True)
    token = token or next((v for k, v in query if k == "token"), "")
    if not isinstance(token, str) or (require_token and not token):
        raise ValueError("plugin token is empty")
    clean = urlunsplit(parts._replace(path=parts.path or "/", query=urlencode([(k, v) for k, v in query if k != "token"])))
    return Endpoint(parts.hostname, port or (443 if parts.scheme == "wss" else 80), token, clean)
