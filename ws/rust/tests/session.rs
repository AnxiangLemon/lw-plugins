use std::{fs, time::Duration};

use futures_util::{SinkExt, StreamExt};
use lw_plugin_client::{ConnectOptions, Endpoint, Manifest, Plaintext, Session, Subscription};
use serde_json::{Value, json};
use tokio::{net::TcpListener, time::timeout};
use tokio_tungstenite::{accept_hdr_async, tungstenite::Message};

#[test]
fn endpoint_policy_and_discovery() {
    let local = Plaintext::Loopback;
    assert!(Endpoint::new("ws://[::1]:26810", "mock-token", local).is_ok());
    assert!(Endpoint::new("wss://example.test/ws", "Bearer mock-token", local).is_ok());
    assert!(Endpoint::new("ws://192.168.1.2", "mock-token", local).is_err());
    assert!(Endpoint::new("ws://192.168.1.2", "mock-token", Plaintext::PrivateIpv4).is_ok());
    assert!(Endpoint::new("ws://127.0.0.1?token=mock-token", "mock-token", local).is_err());
    assert!(Endpoint::new("ws://127.0.0.1", "bad\r\nheader", local).is_err());

    let file = tempfile::NamedTempFile::new().unwrap();
    fs::write(
        file.path(),
        r#"{"host":"::1","port":26810,"token":"mock-token","protocol_version":2}"#,
    )
    .unwrap();
    assert!(Endpoint::from_file(file.path(), local).is_ok());
}

#[tokio::test]
#[allow(clippy::result_large_err)]
async fn session_negotiates_and_routes_frames() {
    timeout(Duration::from_secs(5), async {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = format!("ws://{}", listener.local_addr().unwrap());
        let host = tokio::spawn(async move {
            let (stream, _) = listener.accept().await.unwrap();
            let mut ws = accept_hdr_async(
                stream,
                |request: &tokio_tungstenite::tungstenite::handshake::server::Request, response| {
                    assert_eq!(request.headers()["authorization"], "Bearer mock-token");
                    assert!(request.uri().query().is_none());
                    Ok(response)
                },
            )
            .await
            .unwrap();
            let hello: Value =
                serde_json::from_str(ws.next().await.unwrap().unwrap().to_text().unwrap()).unwrap();
            assert_eq!(
                hello,
                json!({"v":2,"type":"hello","plugin_id":"rust-test","title":"Test","version":"0.1.0"})
            );
            ws.send(Message::Ping(vec![1, 2, 3])).await.unwrap();
            assert_eq!(
                ws.next().await.unwrap().unwrap(),
                Message::Pong(vec![1, 2, 3])
            );
            ws.send(Message::Text(
                json!({"v":2,"type":"hello.ok","host_version":"test"}).to_string(),
            ))
            .await
            .unwrap();
            let subscribe: Value =
                serde_json::from_str(ws.next().await.unwrap().unwrap().to_text().unwrap()).unwrap();
            assert_eq!(
                subscribe,
                json!({"v":2,"type":"subscribe","events":["message.received"],"filter":{"platforms":["wechat"]}})
            );
            ws.send(Message::Text(
                json!({"v":2,"type":"subscribe.ok","events":["message.received"]}).to_string(),
            ))
            .await
            .unwrap();
            let call: Value =
                serde_json::from_str(ws.next().await.unwrap().unwrap().to_text().unwrap()).unwrap();
            assert_eq!(call["v"], 2);
            assert_eq!(call["type"], "api.call");
            assert_eq!(call["method"], "accounts.list");
            assert_eq!(call["params"], json!({}));
            for frame in [
                json!({"v":2,"type":"event","event":"message.received","event_id":"e1","id":18446744073709551615u64}),
                json!({"v":2,"type":"api.result","req_id":call["req_id"],"ok":true,"data":[]}),
            ] {
                ws.send(Message::Text(frame.to_string())).await.unwrap();
            }
            ws.close(None).await.unwrap();
        });

        let endpoint = Endpoint::new(&address, "bearer mock-token", Plaintext::Loopback).unwrap();
        let mut session = Session::connect(
            &endpoint,
            Manifest {
                id: "rust-test",
                title: "Test",
                version: "0.1.0",
            },
            Subscription {
                events: &["message.received"],
                filter: Some(json!({"platforms":["wechat"]})),
            },
            ConnectOptions::default(),
        )
        .await
        .unwrap();
        assert_eq!(session.hello()["host_version"], "test");
        let request = session.send_call("accounts.list", json!({})).await.unwrap();
        assert!(!request.is_empty());
        let event = session.recv().await.unwrap().unwrap();
        assert_eq!(event["type"], "event");
        assert_eq!(event["id"].as_u64(), Some(u64::MAX));
        let result = session.recv().await.unwrap().unwrap();
        assert_eq!(result["req_id"], request);
        assert_eq!(result["data"], json!([]));
        assert!(session.recv().await.is_err());
        host.await.unwrap();
    })
    .await
    .unwrap();
}
