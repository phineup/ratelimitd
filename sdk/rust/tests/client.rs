use std::io::{Read, Write};
use std::net::TcpListener;
use std::sync::mpsc::{channel, Receiver};
use std::thread;
use std::time::Duration;

use ratelimitd::{CheckRequest, Client, Error, Rule};

struct Seen {
    request_line: String,
    headers: String, // lower-cased
    body: String,
}

/// Serves one canned `(status, body)` per connection, in order, and reports what it saw.
fn serve(responses: Vec<(u16, &'static str)>) -> (String, Receiver<Seen>) {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let url = format!("http://{}", listener.local_addr().unwrap());
    let (tx, rx) = channel();
    thread::spawn(move || {
        for (status, canned) in responses {
            let Ok((mut sock, _)) = listener.accept() else {
                return;
            };
            let mut buf = Vec::new();
            let mut tmp = [0u8; 1024];
            let header_end = loop {
                let n = sock.read(&mut tmp).unwrap_or(0);
                if n == 0 {
                    break buf.len();
                }
                buf.extend_from_slice(&tmp[..n]);
                if let Some(p) = buf.windows(4).position(|w| w == b"\r\n\r\n") {
                    break p + 4;
                }
            };
            let head = String::from_utf8_lossy(&buf[..header_end]).to_string();
            let len = head
                .lines()
                .find_map(|l| {
                    l.to_ascii_lowercase()
                        .strip_prefix("content-length:")
                        .and_then(|v| v.trim().parse::<usize>().ok())
                })
                .unwrap_or(0);
            while buf.len() < header_end + len {
                let n = sock.read(&mut tmp).unwrap_or(0);
                if n == 0 {
                    break;
                }
                buf.extend_from_slice(&tmp[..n]);
            }
            let end = (header_end + len).min(buf.len());
            let mut lines = head.lines();
            let seen = Seen {
                request_line: lines.next().unwrap_or("").to_string(),
                headers: lines.collect::<Vec<_>>().join("\n").to_ascii_lowercase(),
                body: String::from_utf8_lossy(&buf[header_end..end]).to_string(),
            };
            let _ = tx.send(seen);

            let resp = if canned.is_empty() {
                format!("HTTP/1.1 {status} X\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
            } else {
                format!(
                    "HTTP/1.1 {status} X\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{canned}",
                    canned.len()
                )
            };
            let _ = sock.write_all(resp.as_bytes());
        }
    });
    (url, rx)
}

#[test]
fn check_encodes_request_and_decodes_allowed() {
    let (url, rx) = serve(vec![(
        200,
        r#"{"allowed":true,"consumed":true,"remaining":4,"retry_after_ms":0,"policy":"login","rules":[],"future":1}"#,
    )]);
    let c = Client::new(&format!("{url}/")).with_token("tok");
    let r = c
        .check(&CheckRequest::policy("u", "login").cost(2).peek())
        .unwrap();
    assert!(r.allowed && r.consumed);
    assert_eq!(r.remaining, 4);

    let s = rx.recv_timeout(Duration::from_secs(2)).unwrap();
    assert_eq!(s.request_line, "POST /v1/check HTTP/1.1");
    assert!(s.headers.contains("authorization: bearer tok"));
    assert!(s.headers.contains("content-type: application/json"));
    let body: serde_json::Value = serde_json::from_str(&s.body).unwrap();
    assert_eq!(
        body,
        serde_json::json!({"key":"u","policy":"login","cost":2,"peek":true})
    );
}

#[test]
fn denied_is_a_result_not_an_error() {
    let (url, _rx) = serve(vec![
        (
            429,
            r#"{"allowed":false,"consumed":false,"remaining":0,"retry_after_ms":1500,"limited_by":"r","policy":"p","rules":[{"name":"r","algorithm":"fixed_window","allowed":false,"limit":1,"remaining":0,"retry_after_ms":1500,"reset_after_ms":1500}]}"#,
        ),
        (429, r#"{"allowed":false,"rules":[]}"#),
    ]);
    let c = Client::new(&url);
    let r = c.check(&CheckRequest::policy("u", "p")).unwrap();
    assert!(!r.allowed);
    assert_eq!(r.retry_after(), Duration::from_millis(1500));
    assert_eq!(r.limited_by.as_deref(), Some("r"));
    assert_eq!(r.rules[0].retry_after_ms, 1500);
    assert!(!c.allow("p", "u").unwrap());
}

#[test]
fn inline_rules_omit_unset_fields() {
    let (url, rx) = serve(vec![(
        200,
        r#"{"allowed":true,"rules":[],"policy":"inline"}"#,
    )]);
    let rule = Rule::new("token_bucket", 10, "1m").with_burst(20);
    Client::new(&url)
        .check(&CheckRequest::rules("1.2.3.4", vec![rule]))
        .unwrap();
    let body: serde_json::Value = serde_json::from_str(&rx.recv().unwrap().body).unwrap();
    assert_eq!(
        body,
        serde_json::json!({"key":"1.2.3.4","rules":[{"algorithm":"token_bucket","limit":10,"window":"1m","burst":20}]})
    );
}

#[test]
fn error_statuses_carry_server_message() {
    let (url, _rx) = serve(vec![
        (404, r#"{"error":"unknown policy \"x\""}"#),
        (502, "bad gateway text"),
        (503, ""),
    ]);
    let c = Client::new(&url);

    let e = c.check(&CheckRequest::policy("k", "x")).unwrap_err();
    assert_eq!(e.status(), Some(404));
    assert!(e.to_string().contains(r#"unknown policy "x""#), "{e}");

    let e = c.policies().unwrap_err();
    assert!(matches!(&e, Error::Api { status: 502, message } if message == "bad gateway text"));

    let e = c.healthy().unwrap_err();
    assert!(matches!(&e, Error::Api { status: 503, message } if message == "HTTP 503"));
}

#[test]
fn reset_policies_healthy() {
    let (url, rx) = serve(vec![
        (204, ""),
        (
            200,
            r#"{"policies":[{"name":"login","description":"d","rules":[{"limit":5,"window":"1m"}]}]}"#,
        ),
        (200, r#"{"status":"ok"}"#),
    ]);
    let c = Client::new(&url);
    c.reset(&CheckRequest::policy("u", "login")).unwrap();
    assert_eq!(rx.recv().unwrap().request_line, "POST /v1/reset HTTP/1.1");
    let ps = c.policies().unwrap();
    assert_eq!(ps[0].name, "login");
    assert_eq!(ps[0].rules[0].limit, 5);
    c.healthy().unwrap();
}

#[test]
fn degraded_flag_is_decoded() {
    let (url, _rx) = serve(vec![(
        200,
        r#"{"allowed":true,"policy":"p","degraded":true,"error":"store unavailable","rules":[]}"#,
    )]);
    let r = Client::new(&url)
        .check(&CheckRequest::policy("k", "p"))
        .unwrap();
    assert!(r.allowed && r.degraded);
    assert_eq!(r.error.as_deref(), Some("store unavailable"));
}

#[test]
fn malformed_body_is_a_decode_error() {
    let (url, _rx) = serve(vec![(200, "not json")]);
    let e = Client::new(&url)
        .check(&CheckRequest::policy("k", "p"))
        .unwrap_err();
    assert!(matches!(e, Error::Decode(_)), "{e:?}");
}

#[test]
fn connection_failure_is_a_transport_error() {
    // Bind then drop to get a port with nothing listening.
    let port = TcpListener::bind("127.0.0.1:0")
        .unwrap()
        .local_addr()
        .unwrap()
        .port();
    let e = Client::new(&format!("http://127.0.0.1:{port}"))
        .with_timeout(Duration::from_millis(500))
        .healthy()
        .unwrap_err();
    assert!(matches!(e, Error::Transport(_)), "{e:?}");
}

#[test]
fn timeout_is_a_transport_error() {
    let listener = TcpListener::bind("127.0.0.1:0").unwrap();
    let url = format!("http://{}", listener.local_addr().unwrap());
    thread::spawn(move || {
        // Accept and never answer.
        let _conn = listener.accept();
        thread::sleep(Duration::from_secs(2));
    });
    let e = Client::new(&url)
        .with_timeout(Duration::from_millis(100))
        .check(&CheckRequest::policy("k", "p"))
        .unwrap_err();
    assert!(matches!(e, Error::Transport(_)), "{e:?}");
}
