use std::{fs, path::Path};

use anyhow::{Context, Result, ensure};
use serde::Deserialize;
use tokio_tungstenite::tungstenite::http::HeaderValue;
use url::{Host, Url};

pub const DEFAULT_URL: &str = "ws://127.0.0.1:26810";

/// Controls which addresses may receive the plugin token over plaintext WebSocket.
#[derive(Clone, Copy)]
pub enum Plaintext {
    Loopback,
    PrivateIpv4,
}

#[derive(Clone)]
pub struct Endpoint {
    pub(crate) url: Url,
    pub(crate) authorization: HeaderValue,
}

impl Endpoint {
    /// Accepts a bare token or an existing Bearer header value.
    pub fn new(address: &str, token: &str, plaintext: Plaintext) -> Result<Self> {
        let url = Self::validate_url(address, plaintext)?;
        let token = token.trim();
        let token = match token.split_once(' ') {
            Some((scheme, value)) if scheme.eq_ignore_ascii_case("bearer") => value.trim(),
            _ => token,
        };
        ensure!(
            !token.is_empty() && token.bytes().all(|b| b.is_ascii_graphic()),
            "请输入有效的插件接入令牌"
        );
        let mut authorization = HeaderValue::from_str(&format!("Bearer {token}"))?;
        authorization.set_sensitive(true);
        Ok(Self { url, authorization })
    }

    pub fn validate_url(address: &str, plaintext: Plaintext) -> Result<Url> {
        let url = Url::parse(address.trim()).context("请输入完整的 WS 地址")?;
        ensure!(
            url.username().is_empty()
                && url.password().is_none()
                && url.query().is_none()
                && url.fragment().is_none(),
            "WS 地址不可包含凭据、查询参数或片段"
        );
        let local = match url.host() {
            Some(Host::Domain(name)) => name.eq_ignore_ascii_case("localhost"),
            Some(Host::Ipv4(ip)) => {
                ip.is_loopback() || matches!(plaintext, Plaintext::PrivateIpv4) && ip.is_private()
            }
            Some(Host::Ipv6(ip)) => ip.is_loopback(),
            None => false,
        };
        ensure!(
            url.scheme() == "wss" && url.has_host() || url.scheme() == "ws" && local,
            "此地址需要使用 wss://"
        );
        Ok(url)
    }

    /// Reads the LW Tools discovery file on each call, allowing token and port rotation.
    pub fn from_file(path: impl AsRef<Path>, plaintext: Plaintext) -> Result<Self> {
        #[derive(Deserialize)]
        struct Discovery {
            #[serde(default)]
            host: String,
            port: u16,
            token: String,
        }

        let data = fs::read(path.as_ref()).context("无法读取插件端点文件")?;
        let discovery: Discovery = serde_json::from_slice(&data).context("插件端点文件格式无效")?;
        ensure!(discovery.port != 0, "插件端点端口必须在 1..65535 范围内");
        let mut url = Url::parse(DEFAULT_URL)?;
        let host = discovery.host.trim();
        if !host.is_empty() {
            let ipv6 = host.parse::<std::net::Ipv6Addr>().ok();
            let bracketed = ipv6.map(|ip| format!("[{ip}]"));
            url.set_host(Some(bracketed.as_deref().unwrap_or(host)))
                .context("插件端点地址无效")?;
        }
        url.set_port(Some(discovery.port))
            .map_err(|()| anyhow::anyhow!("插件端点端口无效"))?;
        Self::new(url.as_str(), &discovery.token, plaintext)
    }
}
