# Rust WebSocket 插件 SDK

`lw-plugin-client` 是 LW Tools WS v2 的 Tokio 客户端库，与本仓库的 [Python SDK](../python/README.md) 和 [Go SDK](../go/README.md) 对接同一协议。

## 引用和运行

使用支持 Rust 2024 edition 的工具链。将本仓库检出到插件项目旁边，并按 `Cargo.toml` 的位置填写路径。

```toml
[dependencies]
lw-plugin-client = { path = "../lw-plugins/ws/rust" }
```

在 LW Tools 的「插件 → WS 插件」中启动服务并允许 `rust-listen` 接入。以下命令在本仓库根目录运行，示例会查询账号数量并监听消息事件。

```powershell
cargo run --locked --manifest-path ws/rust/Cargo.toml --example listen -- 'C:\path\to\host-data\config\plugin-endpoint.json'
```

也可通过 `LW_PLUGIN_ENDPOINT_FILE` 指定端点文件。使用显式地址时调用 `Endpoint::new`。访问回环地址可用 `ws://`，访问远程地址用 `wss://`。需要访问可信局域网时，可显式使用 `Plaintext::PrivateIpv4` 允许私有 IPv4 地址使用 `ws://`。

## 会话接口

- `Session::connect` 发送 Bearer 认证、`hello` 和 `subscribe`，确认宿主接受请求的事件。通过 `Session::hello` 读取已声明的宿主能力。
- `Session::send_call` 返回 `req_id`，通过 `Session::recv` 读取事件和 `api.result`。调用方按 `req_id` 关联响应。原始 JSON 保留协议字段与 64 位整数。
- `Session::send_frame` 可发送协议中的其他 JSON 帧。读取时自动回应 WebSocket Ping。
- 应用负责断线重连和业务回执处理。连接中断或等待超时后，应用应根据原回执及幂等键决定怎样恢复。

完整调用流程见 [监听示例](examples/listen.rs)。测试使用本机模拟 WS 宿主。

```powershell
cargo test --locked --manifest-path ws/rust/Cargo.toml --all-targets
```

使用 [MIT License](LICENSE)。
