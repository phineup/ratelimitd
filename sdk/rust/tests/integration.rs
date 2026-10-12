//! Real-server tests; skipped unless RATELIMITD_URL is set:
//!   go run ./cmd/ratelimitd -config sdk/test-config.json
//!   RATELIMITD_URL=http://127.0.0.1:18080 cargo test

use std::time::{SystemTime, UNIX_EPOCH};

use ratelimitd::{CheckRequest, Client, Rule};

fn client() -> Option<Client> {
    let url = std::env::var("RATELIMITD_URL")
        .ok()
        .filter(|u| !u.is_empty())?;
    let mut c = Client::new(&url);
    if let Ok(t) = std::env::var("RATELIMITD_TOKEN") {
        if !t.is_empty() {
            c = c.with_token(&t);
        }
    }
    Some(c)
}

#[test]
fn flow() {
    let Some(c) = client() else { return };
    let key = format!(
        "rs-{}",
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    );
    let rules = vec![Rule::new("fixed_window", 2, "1m")];

    c.healthy().unwrap();
    for _ in 0..2 {
        let r = c.check(&CheckRequest::rules(&key, rules.clone())).unwrap();
        assert!(r.allowed && r.consumed);
    }
    let denied = c.check(&CheckRequest::rules(&key, rules.clone())).unwrap();
    assert!(!denied.allowed);
    assert!(denied.retry_after_ms > 0);

    c.reset(&CheckRequest::rules(&key, rules.clone())).unwrap();
    let peek = c.check(&CheckRequest::rules(&key, rules).peek()).unwrap();
    assert!(peek.allowed && !peek.consumed);
}

#[test]
fn errors_and_policies() {
    let Some(c) = client() else { return };
    let rules = vec![Rule::new("fixed_window", 1, "1m")];
    let e = c
        .check(&CheckRequest::rules("k", rules).cost(5))
        .unwrap_err();
    assert_eq!(e.status(), Some(422));
    let e = c.check(&CheckRequest::policy("k", "nope")).unwrap_err();
    assert_eq!(e.status(), Some(404));
    assert!(c.policies().unwrap().iter().any(|p| p.name == "sdk_test"));
}
