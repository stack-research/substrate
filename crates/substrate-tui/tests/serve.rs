//! Drive `substrate serve` the way a GET-only web assistant would: raw HTTP
//! requests, reply smuggled into the URL.

use std::io::{BufRead, BufReader, Read, Write};
use std::net::TcpStream;
use std::process::{Child, Command, Stdio};

use base64::Engine;
use tempfile::TempDir;

const KEY: &str = "testkey-abcdef0123456789";

fn substrate() -> Command {
    Command::new(assert_cmd::cargo::cargo_bin("substrate"))
}

fn get(addr: &str, path_and_query: &str) -> (u16, String) {
    let mut stream = TcpStream::connect(addr).unwrap();
    write!(
        stream,
        "GET {path_and_query} HTTP/1.0\r\nHost: test\r\n\r\n"
    )
    .unwrap();
    let mut raw = String::new();
    stream.read_to_string(&mut raw).unwrap();
    let status: u16 = raw
        .split_whitespace()
        .nth(1)
        .and_then(|s| s.parse().ok())
        .unwrap_or(0);
    let body = raw
        .split_once("\r\n\r\n")
        .map(|(_, b)| b.to_string())
        .unwrap_or_default();
    (status, body)
}

struct ServerGuard(Child);
impl Drop for ServerGuard {
    fn drop(&mut self) {
        let _ = self.0.kill();
    }
}

#[test]
fn web_only_participant_takes_turns_over_get() {
    let space = TempDir::new().unwrap();
    let home = space.path().join(".home");
    let run = |args: &[&str]| {
        let out = substrate()
            .env("SUBSTRATE_HOME", &home)
            .arg("--space")
            .arg(space.path())
            .args(args)
            .output()
            .unwrap();
        assert!(out.status.success(), "{args:?}: {:?}", out);
        String::from_utf8(out.stdout).unwrap()
    };
    run(&["init"]);
    run(&["add", "user-name", "--kind", "human"]);
    run(&["add", "kagi", "--kind", "other"]);
    run(&[
        "new",
        "research",
        "--topic",
        "GET-only transports",
        "--moderator",
        "user-name",
        "--turns",
        "kagi",
    ]);
    run(&[
        "write",
        "research",
        "--as",
        "user-name",
        "-m",
        "kagi: summarize the plan, please.",
    ]);

    // brief (the manual courier packet) addresses the participant
    let brief = run(&["brief", "research", "--for", "kagi"]);
    assert!(brief.contains("participant: kagi"), "{brief}");
    assert!(brief.contains("you - reply now"), "{brief}");
    assert!(brief.contains("summarize the plan"), "{brief}");

    // start the server with a fixed key; read its actual address
    let mut child = substrate()
        .env("SUBSTRATE_HOME", &home)
        .arg("--space")
        .arg(space.path())
        .args(["serve", "--port", "0", "--proxy", "kagi", "--key", KEY])
        .stdout(Stdio::piped())
        .spawn()
        .unwrap();
    let stdout = child.stdout.take().unwrap();
    // keep the reader alive for the whole test — dropping it closes the
    // pipe and the server dies of broken-pipe on its next startup print
    let mut reader = BufReader::new(stdout);
    let mut first_line = String::new();
    reader.read_line(&mut first_line).unwrap();
    let addr = first_line
        .trim()
        .strip_prefix("listening on http://")
        .unwrap()
        .to_string();
    let _guard = ServerGuard(child);

    // no key, no entry
    let (status, _) = get(&addr, "/t/research");
    assert_eq!(status, 403);

    // read the thread: brief + write recipe with the current thread version
    let (status, body) = get(&addr, &format!("/t/research?key={KEY}&nonce=read-1"));
    assert_eq!(status, 200, "{body}");
    assert!(body.contains("you - reply now"), "{body}");
    assert!(body.contains("thread version: 1"), "{body}");
    assert!(
        body.contains("IMPORTANT: USE A NEW NONCE FOR EVERY REQUEST"),
        "{body}"
    );
    assert!(
        body.contains("/t/research?key=testkey-abcdef0123456789&nonce=NONCE"),
        "{body}"
    );
    assert!(body.contains("plain ASCII markdown only"), "{body}");
    assert!(
        body.contains("&turn=1&nonce=NONCE&b64=URL_SAFE_BASE64_REPLY"),
        "{body}"
    );

    // stale version is rejected with instructions, not appended
    let reply = "## Summary\n\nGET-only transports can still hold the floor.";
    let b64 = base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(reply);
    let (status, body) = get(
        &addr,
        &format!("/t/research/write?key={KEY}&turn=0&nonce=write-stale&b64={b64}"),
    );
    // write outcomes are HTML documents at 200 — fetch-and-parse tools
    // (Kagi's librarian) can't read bare-text acks or non-2xx responses
    assert_eq!(status, 200, "{body}");
    assert!(body.starts_with("<!DOCTYPE html"), "{body}");
    assert!(body.contains("thread changed"), "{body}");
    assert!(body.contains("NOT recorded"), "{body}");

    // correct version: the write lands through the same turn engine
    let (status, body) = get(
        &addr,
        &format!("/t/research/write?key={KEY}&turn=1&nonce=write-1&b64={b64}"),
    );
    assert_eq!(status, 200, "{body}");
    assert!(body.contains("entry recorded"), "{body}");
    assert!(body.contains("next turn: user-name"), "{body}");
    // the response embeds the refreshed thread — no second fetch needed
    assert!(body.contains("## Summary"), "{body}");

    // replaying the same URL is harmless: floor moved, outcome in-page
    let (status, body) = get(
        &addr,
        &format!("/t/research/write?key={KEY}&turn=1&nonce=replay-1&b64={b64}"),
    );
    assert_eq!(status, 200, "{body}");
    assert!(body.contains("thread changed"), "{body}");

    // the entry is real, multi-line markdown intact
    let transcript = run(&["read", "research"]);
    assert!(transcript.contains("## Summary"), "{transcript}");
    assert!(transcript.contains("hold the floor"), "{transcript}");

    // text= variant works too (user-name resumes, then kagi again via percent-encoding)
    run(&[
        "write",
        "research",
        "--as",
        "user-name",
        "-m",
        "thanks — one more?",
    ]);
    let (status, body) = get(
        &addr,
        &format!("/t/research/write?key={KEY}&nonce=write-2&text=pass"),
    );
    assert_eq!(status, 200, "{body}");
    assert!(body.contains("no-op"), "{body}");
}

#[test]
fn responses_defeat_caches() {
    let space = TempDir::new().unwrap();
    let home = space.path().join(".home");
    let run = |args: &[&str]| {
        let out = substrate()
            .env("SUBSTRATE_HOME", &home)
            .arg("--space")
            .arg(space.path())
            .args(args)
            .output()
            .unwrap();
        assert!(out.status.success(), "{args:?}");
    };
    run(&["init"]);
    run(&["add", "kagi", "--kind", "other"]);
    run(&["add", "mod", "--kind", "human"]);
    run(&[
        "new",
        "cachy",
        "--topic",
        "t",
        "--moderator",
        "mod",
        "--turns",
        "kagi",
    ]);

    let mut child = substrate()
        .env("SUBSTRATE_HOME", &home)
        .arg("--space")
        .arg(space.path())
        .args(["serve", "--port", "0", "--proxy", "kagi", "--key", KEY])
        .stdout(Stdio::piped())
        .spawn()
        .unwrap();
    let stdout = child.stdout.take().unwrap();
    let mut reader = BufReader::new(stdout);
    let mut first_line = String::new();
    reader.read_line(&mut first_line).unwrap();
    let addr = first_line
        .trim()
        .strip_prefix("listening on http://")
        .unwrap()
        .to_string();
    let _guard = ServerGuard(child);

    // raw response this time: we need the headers
    let mut stream = TcpStream::connect(&addr).unwrap();
    write!(
        stream,
        "GET /t/cachy?key={KEY}&nonce=zz91 HTTP/1.0\r\nHost: t\r\n\r\n"
    )
    .unwrap();
    let mut raw = String::new();
    stream.read_to_string(&mut raw).unwrap();

    // no-store headers present; the cache-busting nonce is ignored by the
    // stateless server, but makes the URL unique; the brief
    // itself teaches the cache-busting recipe
    assert!(raw.contains("Cache-Control: no-store"), "{raw}");
    assert!(raw.contains("Pragma: no-cache"), "{raw}");
    assert!(raw.contains("thread version: 0"), "{raw}");
    assert!(raw.contains("&nonce=NONCE"), "{raw}");
}
