"""端点发现：定位宿主写出的 `config/plugin-endpoint.json`。

优先级：显式参数 > 环境变量 > 发现文件。硬编码端口是最后的手段，
因为宿主允许把 `plugin_ws.port` 配成 0 由系统分配。
"""

from __future__ import annotations

import json
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Iterator, Optional

ENDPOINT_FILENAME = "plugin-endpoint.json"

# 只在 URL 里没写端口时兜底，和宿主的 plugin_ws.port 默认值保持一致
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
    token: str

    @property
    def url(self) -> str:
        host = f"[{self.host}]" if ":" in self.host else self.host
        return f"ws://{host}:{self.port}/"

    def url_with_token(self) -> str:
        return f"{self.url}?token={self.token}"


def _candidate_files(explicit: Optional[str]) -> Iterator[Path]:
    if explicit:
        yield Path(explicit)
        return
    env_file = os.environ.get(ENV_ENDPOINT_FILE)
    if env_file:
        yield Path(env_file)
    # 从当前目录逐级向上找 config/plugin-endpoint.json，
    # 这样插件放在仓库任意子目录都能跑起来
    here = Path.cwd().resolve()
    for directory in [here, *here.parents]:
        yield directory / "config" / ENDPOINT_FILENAME
        yield directory / ENDPOINT_FILENAME


def load_endpoint(
    *,
    url: Optional[str] = None,
    token: Optional[str] = None,
    endpoint_file: Optional[str] = None,
) -> Endpoint:
    """解析连接信息。缺任何一项都会去发现文件里补。"""
    url = url or os.environ.get(ENV_URL)
    token = token or os.environ.get(ENV_TOKEN)

    if url and token:
        host, port = _split_ws_url(url)
        return Endpoint(host=host, port=port, token=token)

    for path in _candidate_files(endpoint_file):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            continue
        host = data.get("host") or "127.0.0.1"
        port = int(data.get("port") or 0)
        file_token = data.get("token") or ""
        if not port:
            continue
        if url:
            host, port = _split_ws_url(url)
        return Endpoint(host=host, port=port, token=token or file_token)

    raise EndpointNotFound(
        "未找到 plugin-endpoint.json。请在应用「插件 → WS 插件」中启动服务，"
        f"或设置环境变量 {ENV_URL} 与 {ENV_TOKEN}。"
    )


def _split_ws_url(url: str) -> tuple[str, int]:
    body = url.split("://", 1)[-1]
    body = body.split("/", 1)[0]
    if body.startswith("["):
        host, _, rest = body[1:].partition("]")
        port = rest.lstrip(":")
    else:
        host, _, port = body.partition(":")
    return host or "127.0.0.1", int(port or DEFAULT_PORT)
