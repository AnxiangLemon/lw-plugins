//! Tokio client for the LW Tools WebSocket plugin protocol v2.

mod endpoint;

pub use endpoint::{DEFAULT_URL, Endpoint, Plaintext};

use std::time::Duration;

use anyhow::{Context, Result, bail, ensure};
use futures_util::{SinkExt, StreamExt};
use serde_json::{Value, json};
use tokio::{net::TcpStream, time::timeout};
use tokio_tungstenite::{
    MaybeTlsStream, WebSocketStream, connect_async_with_config,
    tungstenite::{Error, Message, client::IntoClientRequest, protocol::WebSocketConfig},
};

pub const PROTOCOL_VERSION: u16 = 2;
const SEND_TIMEOUT: Duration = Duration::from_secs(5);

/// 宿主 WS 协议 v2 公开事件名常量（含通用、微信与 Telegram 交互/群管事件）。
pub mod events {
    pub const MESSAGE_RECEIVED: &str = "message.received";
    pub const MESSAGE_SENT: &str = "message.sent";
    pub const ACCOUNT_ONLINE: &str = "account.online";
    pub const ACCOUNT_OFFLINE: &str = "account.offline";

    pub const WECHAT_MESSAGE_RECEIVED: &str = "wechat.message.received";
    pub const WECHAT_MESSAGE_SENT: &str = "wechat.message.sent";
    pub const WECHAT_MESSAGE_OBSERVED: &str = "wechat.message.observed";
    pub const WECHAT_MESSAGE_FILTER: &str = "wechat.message.filter";
    pub const WECHAT_CALL_UPDATED: &str = "wechat.call.updated";
    pub const WECHAT_GROUP_REFRESH_HINT: &str = "wechat.group.refresh_hint";
    pub const WECHAT_SYNC_HINT: &str = "wechat.sync.hint";

    pub const TELEGRAM_CALLBACK_QUERY: &str = "telegram.callback_query";
    pub const TELEGRAM_EDITED_MESSAGE: &str = "telegram.edited_message";
    pub const TELEGRAM_MY_CHAT_MEMBER: &str = "telegram.my_chat_member";
    pub const TELEGRAM_CHAT_MEMBER: &str = "telegram.chat_member";
    pub const TELEGRAM_CHAT_JOIN_REQUEST: &str = "telegram.chat_join_request";
    pub const TELEGRAM_MESSAGE_REACTION: &str = "telegram.message_reaction";
}

pub struct Manifest<'a> {
    pub id: &'a str,
    pub title: &'a str,
    pub version: &'a str,
}

pub struct Subscription<'a> {
    pub events: &'a [&'a str],
    pub filter: Option<Value>,
}

pub struct ConnectOptions {
    pub handshake_timeout: Duration,
    pub max_message_size: usize,
}

impl Default for ConnectOptions {
    fn default() -> Self {
        Self {
            handshake_timeout: Duration::from_secs(8),
            max_message_size: 4 * 1024 * 1024,
        }
    }
}

/// One authenticated connection. The caller owns the receive loop and reconnect policy.
pub struct Session {
    socket: WebSocketStream<MaybeTlsStream<TcpStream>>,
    hello: Value,
}

impl Session {
    pub async fn connect(
        endpoint: &Endpoint,
        manifest: Manifest<'_>,
        subscription: Subscription<'_>,
        options: ConnectOptions,
    ) -> Result<Self> {
        let mut request = endpoint.url.as_str().into_client_request()?;
        request
            .headers_mut()
            .insert("Authorization", endpoint.authorization.clone());
        let config = WebSocketConfig {
            max_message_size: Some(options.max_message_size),
            max_frame_size: Some(options.max_message_size),
            ..Default::default()
        };
        let (socket, _) = timeout(
            options.handshake_timeout,
            connect_async_with_config(request, Some(config), false),
        )
        .await
        .context("连接 LW Tools WS 超时")?
        .map_err(|error| match error {
            Error::Http(response) if matches!(response.status().as_u16(), 401 | 403) => {
                anyhow::anyhow!("LW Tools 拒绝接入，请检查插件令牌")
            }
            _ => anyhow::anyhow!("无法连接 LW Tools WS，请检查地址、证书和插件状态"),
        })?;
        let mut session = Self {
            socket,
            hello: Value::Null,
        };
        session
            .send_frame(json!({
                "v": PROTOCOL_VERSION, "type": "hello", "plugin_id": manifest.id,
                "title": manifest.title, "version": manifest.version
            }))
            .await?;
        session.hello = session
            .expect_frame("hello.ok", manifest.id, options.handshake_timeout)
            .await?;

        let mut subscribe = json!({
            "v": PROTOCOL_VERSION, "type": "subscribe", "events": subscription.events
        });
        if let Some(filter) = subscription.filter {
            subscribe["filter"] = filter;
        }
        session.send_frame(subscribe).await?;
        let accepted = session
            .expect_frame("subscribe.ok", manifest.id, options.handshake_timeout)
            .await?;
        let events = accepted["events"]
            .as_array()
            .context("LW Tools 返回的订阅列表格式无效")?;
        for event in subscription.events {
            ensure!(
                events.iter().any(|name| name.as_str() == Some(event)),
                "LW Tools 未确认事件订阅：{event}"
            );
        }
        Ok(session)
    }

    /// Original hello.ok, including advertised events and optional capabilities.
    pub fn hello(&self) -> &Value {
        &self.hello
    }

    /// Sends an RPC and returns the req_id used to match its api.result.
    pub async fn send_call(&mut self, method: &str, params: Value) -> Result<String> {
        let request_id = uuid::Uuid::new_v4().simple().to_string();
        self.send_frame(json!({
            "v": PROTOCOL_VERSION, "type": "api.call", "req_id": request_id,
            "method": method, "params": params
        }))
        .await?;
        Ok(request_id)
    }

    /// Calls any Telegram Bot API method through `telegram.call` and returns `req_id`.
    pub async fn send_telegram_call(
        &mut self,
        account_id: &str,
        method: &str,
        params: Value,
    ) -> Result<String> {
        self.send_call(
            "telegram.call",
            json!({
                "account_id": account_id,
                "method": method,
                "params": params,
            }),
        )
        .await
    }

    /// Downloads a Telegram file by `file_id` (base64 envelope) and returns `req_id`.
    pub async fn send_telegram_download_file(
        &mut self,
        account_id: &str,
        file_id: &str,
    ) -> Result<String> {
        self.send_call(
            "telegram.download_file",
            json!({
                "account_id": account_id,
                "file_id": file_id,
            }),
        )
        .await
    }

    /// Sends a protocol frame such as an action or an event acknowledgement request.
    pub async fn send_frame(&mut self, frame: Value) -> Result<()> {
        self.send(Message::Text(frame.to_string())).await
    }

    pub async fn ping(&mut self) -> Result<()> {
        self.send(Message::Ping(Vec::new())).await
    }

    async fn send(&mut self, message: Message) -> Result<()> {
        timeout(SEND_TIMEOUT, self.socket.send(message))
            .await
            .context("等待 LW Tools WS 发送超时")?
            .context("无法发送 LW Tools WS 请求")
    }

    /// Reads one frame, returning None for control traffic and an error when disconnected.
    /// Invalid JSON is returned as a serde_json::Error.
    pub async fn recv(&mut self) -> Result<Option<Value>> {
        match self.socket.next().await {
            Some(Ok(Message::Text(text))) => Ok(Some(serde_json::from_str(&text)?)),
            Some(Ok(Message::Ping(_))) => {
                // Tungstenite queues the Pong when reading the Ping.
                timeout(SEND_TIMEOUT, self.socket.flush())
                    .await
                    .context("等待 LW Tools WS 心跳响应发送超时")?
                    .context("无法响应 LW Tools 心跳")?;
                Ok(None)
            }
            Some(Ok(Message::Close(_))) | None => bail!("LW Tools WS 已断开"),
            Some(Err(error)) => Err(error).context("LW Tools WS 已断开"),
            _ => Ok(None),
        }
    }

    async fn expect_frame(
        &mut self,
        expected: &str,
        plugin_id: &str,
        deadline: Duration,
    ) -> Result<Value> {
        timeout(deadline, async {
            loop {
                let Some(frame) = self.recv().await? else {
                    continue;
                };
                if frame["type"] == expected {
                    return Ok(frame);
                }
                if frame["type"] == "error" {
                    if frame["code"] == "not_permitted" {
                        bail!("请在 LW Tools 的 WS 插件中允许 {plugin_id} 接入");
                    }
                    let message = frame["message"].as_str().unwrap_or("声明或订阅失败");
                    bail!("LW Tools 拒绝插件请求：{message}");
                }
            }
        })
        .await
        .context("等待 LW Tools 插件响应超时")?
    }
}
