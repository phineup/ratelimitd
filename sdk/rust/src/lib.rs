//! Client for the [ratelimitd](https://github.com/phineup/ratelimitd) rate limiting sidecar.
//!
//! ```no_run
//! use ratelimitd::{Client, CheckRequest};
//!
//! let client = Client::new("http://localhost:8080").with_token("secret");
//! let res = client.check(&CheckRequest::policy("user-42", "login"))?;
//! if !res.allowed {
//!     println!("retry in {:?}", res.retry_after());
//! }
//! # Ok::<(), ratelimitd::Error>(())
//! ```

use std::fmt;
use std::time::Duration;

use serde::{Deserialize, Serialize};

/// An inline rule. `window` uses the server syntax: `"30s"`, `"5h"`, `"1d12h"`.
#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize, Deserialize)]
pub struct Rule {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    /// `fixed_window`, `sliding_window`, `sliding_log` or `token_bucket`.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub algorithm: Option<String>,
    pub limit: i64,
    pub window: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub burst: Option<i64>,
    /// `clock` or `first_request` (fixed_window only).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub anchor: Option<String>,
}

impl Rule {
    pub fn new(algorithm: &str, limit: i64, window: &str) -> Self {
        Rule {
            algorithm: Some(algorithm.to_string()),
            limit,
            window: window.to_string(),
            ..Default::default()
        }
    }

    pub fn with_burst(mut self, burst: i64) -> Self {
        self.burst = Some(burst);
        self
    }
}

/// A named policy configured on the server.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct Policy {
    pub name: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub rules: Vec<Rule>,
    #[serde(default)]
    pub dynamic: bool,
}

/// One check. Set either `policy` or `rules`.
#[derive(Debug, Clone, PartialEq, Eq, Default, Serialize)]
pub struct CheckRequest {
    pub key: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub policy: Option<String>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub rules: Vec<Rule>,
    /// Defaults to 1 on the server.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cost: Option<i64>,
    /// Evaluate without consuming quota.
    #[serde(skip_serializing_if = "std::ops::Not::not")]
    pub peek: bool,
}

impl CheckRequest {
    pub fn policy(key: &str, policy: &str) -> Self {
        CheckRequest {
            key: key.to_string(),
            policy: Some(policy.to_string()),
            ..Default::default()
        }
    }

    pub fn rules(key: &str, rules: Vec<Rule>) -> Self {
        CheckRequest {
            key: key.to_string(),
            rules,
            ..Default::default()
        }
    }

    pub fn cost(mut self, cost: i64) -> Self {
        self.cost = Some(cost);
        self
    }

    pub fn peek(mut self) -> Self {
        self.peek = true;
        self
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct RuleResult {
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub algorithm: String,
    pub allowed: bool,
    pub limit: i64,
    pub remaining: i64,
    pub retry_after_ms: i64,
    pub reset_after_ms: i64,
}

/// Decision returned by [`Client::check`]. A denial (HTTP 429) is a normal result.
#[derive(Debug, Clone, PartialEq, Eq, Deserialize)]
pub struct CheckResult {
    pub allowed: bool,
    #[serde(default)]
    pub consumed: bool,
    #[serde(default)]
    pub remaining: i64,
    #[serde(default)]
    pub retry_after_ms: i64,
    #[serde(default)]
    pub limited_by: Option<String>,
    #[serde(default)]
    pub rules: Vec<RuleResult>,
    #[serde(default)]
    pub policy: String,
    /// True when the server's store was down and it failed open.
    #[serde(default)]
    pub degraded: bool,
    #[serde(default)]
    pub error: Option<String>,
}

impl CheckResult {
    pub fn retry_after(&self) -> Duration {
        Duration::from_millis(self.retry_after_ms.max(0) as u64)
    }
}

#[derive(Debug)]
pub enum Error {
    /// Non-2xx response other than 429.
    Api { status: u16, message: String },
    /// Connection, timeout or protocol failure.
    Transport(String),
    /// The response body was not the expected JSON.
    Decode(String),
}

impl Error {
    /// HTTP status for [`Error::Api`].
    pub fn status(&self) -> Option<u16> {
        match self {
            Error::Api { status, .. } => Some(*status),
            _ => None,
        }
    }
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Error::Api { status, message } => write!(f, "ratelimitd: HTTP {status}: {message}"),
            Error::Transport(m) => write!(f, "ratelimitd: transport: {m}"),
            Error::Decode(m) => write!(f, "ratelimitd: decode: {m}"),
        }
    }
}

impl std::error::Error for Error {}

pub type Result<T> = std::result::Result<T, Error>;

/// Blocking, thread-safe client.
#[derive(Debug, Clone)]
pub struct Client {
    base_url: String,
    token: Option<String>,
    agent: ureq::Agent,
}

impl Client {
    /// `base_url` is e.g. `http://localhost:8080`.
    pub fn new(base_url: &str) -> Self {
        Client {
            base_url: base_url.trim_end_matches('/').to_string(),
            token: None,
            agent: ureq::AgentBuilder::new()
                .timeout(Duration::from_secs(10))
                .build(),
        }
    }

    /// Bearer token for servers with auth enabled.
    pub fn with_token(mut self, token: &str) -> Self {
        self.token = Some(token.to_string());
        self
    }

    /// Per-request timeout (default 10s).
    pub fn with_timeout(mut self, timeout: Duration) -> Self {
        self.agent = ureq::AgentBuilder::new().timeout(timeout).build();
        self
    }

    /// Evaluate (and by default consume) a request.
    pub fn check(&self, req: &CheckRequest) -> Result<CheckResult> {
        let (status, body) = self.send("POST", "/v1/check", Some(req))?;
        if status != 200 && status != 429 {
            return Err(api_error(status, &body));
        }
        serde_json::from_str(&body).map_err(|e| Error::Decode(e.to_string()))
    }

    /// Shorthand: check `key` against a named policy with cost 1.
    pub fn allow(&self, policy: &str, key: &str) -> Result<bool> {
        Ok(self.check(&CheckRequest::policy(key, policy))?.allowed)
    }

    /// Clear counters for `req.key` under a policy or inline rules.
    pub fn reset(&self, req: &CheckRequest) -> Result<()> {
        let (status, body) = self.send("POST", "/v1/reset", Some(req))?;
        if status != 204 && status != 200 {
            return Err(api_error(status, &body));
        }
        Ok(())
    }

    /// Policies configured on the server.
    pub fn policies(&self) -> Result<Vec<Policy>> {
        #[derive(Deserialize)]
        struct List {
            #[serde(default)]
            policies: Vec<Policy>,
        }
        let (status, body) = self.send::<()>("GET", "/v1/policies", None)?;
        if status != 200 {
            return Err(api_error(status, &body));
        }
        let list: List = serde_json::from_str(&body).map_err(|e| Error::Decode(e.to_string()))?;
        Ok(list.policies)
    }

    /// `Ok(())` when the server and its store are healthy.
    pub fn healthy(&self) -> Result<()> {
        let (status, body) = self.send::<()>("GET", "/healthz", None)?;
        if status != 200 {
            return Err(api_error(status, &body));
        }
        Ok(())
    }

    fn send<B: Serialize>(
        &self,
        method: &str,
        path: &str,
        body: Option<&B>,
    ) -> Result<(u16, String)> {
        let mut req = self
            .agent
            .request(method, &format!("{}{}", self.base_url, path));
        if let Some(t) = &self.token {
            req = req.set("Authorization", &format!("Bearer {t}"));
        }
        let result = match body {
            Some(b) => {
                let payload = serde_json::to_string(b).map_err(|e| Error::Decode(e.to_string()))?;
                req.set("Content-Type", "application/json")
                    .send_string(&payload)
            }
            None => req.call(),
        };
        match result {
            Ok(resp) => read(resp),
            Err(ureq::Error::Status(_, resp)) => read(resp),
            Err(ureq::Error::Transport(t)) => Err(Error::Transport(t.to_string())),
        }
    }
}

fn read(resp: ureq::Response) -> Result<(u16, String)> {
    let status = resp.status();
    let body = resp
        .into_string()
        .map_err(|e| Error::Transport(e.to_string()))?;
    Ok((status, body))
}

fn api_error(status: u16, body: &str) -> Error {
    let mut message = body.trim().to_string();
    if let Ok(v) = serde_json::from_str::<serde_json::Value>(body) {
        if let Some(m) = v.get("error").and_then(|e| e.as_str()) {
            if !m.is_empty() {
                message = m.to_string();
            }
        }
    }
    if message.is_empty() {
        message = format!("HTTP {status}");
    }
    Error::Api { status, message }
}
