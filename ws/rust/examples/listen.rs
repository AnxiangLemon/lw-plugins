use std::{env, path::PathBuf};

use anyhow::{Context, Result, bail};
use lw_plugin_client::{ConnectOptions, Endpoint, Manifest, Plaintext, Session, Subscription};
use serde_json::json;

#[tokio::main(flavor = "current_thread")]
async fn main() -> Result<()> {
    let file = env::args_os()
        .nth(1)
        .or_else(|| env::var_os("LW_PLUGIN_ENDPOINT_FILE"))
        .context("请指定 plugin-endpoint.json 路径或设置 LW_PLUGIN_ENDPOINT_FILE")?;
    let endpoint = Endpoint::from_file(PathBuf::from(file), Plaintext::Loopback)?;
    let mut session = Session::connect(
        &endpoint,
        Manifest {
            id: "rust-listen",
            title: "Rust Listener",
            version: env!("CARGO_PKG_VERSION"),
        },
        Subscription {
            events: &["message.received"],
            filter: None,
        },
        ConnectOptions::default(),
    )
    .await?;

    let request_id = session.send_call("accounts.list", json!({})).await?;
    loop {
        let Some(frame) = session.recv().await? else {
            continue;
        };
        match frame["type"].as_str() {
            Some("api.result") if frame["req_id"] == request_id => {
                if frame["ok"] != true {
                    bail!("accounts.list: {}", frame["message"]);
                }
                let accounts = frame["data"].as_array().context("账号列表格式无效")?;
                println!("已连接，宿主返回 {} 个账号", accounts.len());
            }
            Some("event") => println!("收到事件 {}", frame["event"].as_str().unwrap_or("")),
            Some("error") if frame["code"] == "event_gap" => bail!("宿主报告事件缺口"),
            _ => {}
        }
    }
}
