//! A transport adapter at the edge: serve one space over HTTP so participants
//! reachable only by URL (web-only assistants with a GET-only fetch tool) can
//! read the thread and take turns. Holds zero state — every request goes
//! through the same turn engine as everything else.
//!
//! Identity comes from a capability key minted per `--proxy` participant,
//! never from a request parameter — the no-impersonation invariant, ported
//! to HTTP. Bind stays on 127.0.0.1; put `tailscale funnel` (or any TLS
//! proxy) in front for the outside world.

use std::fs;

use anyhow::Result;
use base64::Engine;
use substrate_core::{thread::ThreadStatus, transcript, turn, Name, Space, SubstrateError};

pub struct Proxy {
    pub name: Name,
    pub key: String,
}

pub fn random_key() -> String {
    // no rand dep needed; /dev/urandom exists everywhere we run —
    // but read exactly 16 bytes, never fs::read (the device is endless)
    let mut bytes = [0u8; 16];
    let filled = fs::File::open("/dev/urandom")
        .and_then(|mut f| std::io::Read::read_exact(&mut f, &mut bytes))
        .is_ok();
    if !filled {
        let fallback = format!(
            "{:x}{:x}",
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map(|d| d.as_nanos())
                .unwrap_or(0)
        );
        let fb = fallback.as_bytes();
        for (i, b) in bytes.iter_mut().enumerate() {
            *b = fb[i % fb.len()];
        }
    }
    bytes.iter().fold(String::new(), |mut s, b| {
        s.push_str(&format!("{b:02x}"));
        s
    })
}

/// The thread state version: total entries on disk (no-ops included). Echoed
/// back by writers via `turn=` so a stale reply is rejected, not appended.
fn thread_version(space: &Space, thread: &Name) -> usize {
    fs::read_dir(space.thread_dir(thread))
        .map(|entries| {
            entries
                .filter_map(|e| e.ok())
                .filter_map(|e| e.file_name().into_string().ok())
                .filter(|f| substrate_core::entry::parse_filename(f).is_some())
                .count()
        })
        .unwrap_or(0)
}

/// The courier packet: standing prompt + status + clean transcript, plus
/// (when served over HTTP) the exact write-back recipe.
pub fn brief_text(
    space: &Space,
    thread: &Name,
    for_name: Option<&Name>,
    proxy_urls: Option<(&str, &str)>,
) -> Result<String> {
    let status = turn::turn_status(space, thread)?;
    let entries = transcript::load_entries(space, thread)?;
    let rendered = transcript::render_transcript(&entries);
    let (_, total_lines) = transcript::window(&rendered, transcript::Window::All);
    let version = thread_version(space, thread);

    let mut out = String::from("SUBSTRATE THREAD\n================\n");
    if let Some(me) = for_name {
        out.push_str(&format!("participant: {me}\n"));
    } else {
        out.push_str("participant: not specified\n");
    }
    out.push_str(&format!(
        "thread: {thread}\n\
         topic: {topic}\n\
         conversation: turn-based group; append-only markdown addressed to everyone\n\
         status: {st:?}\n\
         current turn: {current}{yours}\n\
         turn order: {order}\n\
         transcript lines: {total_lines}\n\
         thread version: {version}\n",
        topic = status.topic,
        st = status.status,
        current = status.current,
        yours = match (status.status, for_name) {
            (ThreadStatus::Ended, Some(_)) => " (thread ended)",
            (ThreadStatus::Active, Some(me)) if *me == status.current => {
                " (you - reply now)"
            }
            (ThreadStatus::Active, Some(_)) => " (not you - wait)",
            (_, None) => "",
        },
        order = status
            .turn_order
            .iter()
            .map(|n| if n == &status.moderator {
                format!("{n} [moderator]")
            } else {
                n.to_string()
            })
            .collect::<Vec<_>>()
            .join(" -> "),
    ));

    if let Some((read_url, _)) = proxy_urls {
        out.push_str(&format!(
            "\nIMPORTANT: USE A NEW NONCE FOR EVERY REQUEST\n\
             ============================================\n\
             Before EVERY fetch - read or write - replace NONCE with a new \
             random ASCII value. Never reuse a nonce, including for a retry. \
             Reusing one can return an old cached page. The nonce defeats \
             caches; it is not the thread version.\n\
             \n\
             To read the latest thread, fetch this path on the same host as \
             this page:\n\
             {read_url}&nonce=NONCE\n",
        ));
    }

    out.push_str("\nTRANSCRIPT (no-op turns omitted)\n================================\n");
    out.push_str(&rendered);
    out.push_str("END TRANSCRIPT\n");

    if let Some((_, write_url)) = proxy_urls {
        out.push_str(&format!(
            "\nYOUR NEXT ACTION\n\
             ================\n\
             {action}\n",
            action = match for_name {
                Some(_) if status.status == ThreadStatus::Ended => {
                    "This thread has ended. Do not write another entry.".to_string()
                }
                Some(me) if *me == status.current => format!(
                    "You have the turn. To write your entry:\n\
                     1. Compose markdown addressed to the whole thread. Keep it under \
                        about 6KB.\n\
                     2. Use plain ASCII markdown only: printable ASCII characters plus \
                        normal line breaks. Avoid Unicode, smart quotes, decorative \
                        symbols, and invisible characters.\n\
                     3. Encode the reply as URL-safe Base64 without padding.\n\
                     4. Copy the thread version above into turn=. Replace NONCE with a \
                        brand-new random ASCII value.\n\
                     5. Fetch this path on the same host as this page:\n\
                     {write_url}&turn={version}&nonce=NONCE&b64=URL_SAFE_BASE64_REPLY\n\
                     \n\
                     For a short URL-encoded reply, you may replace b64=... with \
                     text=URL_ENCODED_REPLY. To yield without adding a visible entry, \
                     send text=pass.\n\
                     \n\
                     Success is a page titled 'substrate: entry recorded'. Every write \
                     response includes the refreshed thread. If it says 'thread changed', \
                     read that refreshed thread and resend using its new turn version, a \
                     newly encoded reply if needed, and another new nonce."
                ),
                Some(_) => format!(
                    "Do not write now; the turn belongs to {current}. To check again, \
                     use the read path above with a new nonce.",
                    current = status.current
                ),
                None => "Read the transcript, then return your reply to the courier.".to_string(),
            },
        ));
    }
    Ok(out)
}

pub fn serve(space: Space, port: u16, proxies: Vec<Proxy>) -> Result<()> {
    let server = tiny_http::Server::http(("127.0.0.1", port))
        .map_err(|e| anyhow::anyhow!("could not bind 127.0.0.1:{port}: {e}"))?;
    let addr = server.server_addr();
    // parse-friendly first line (tests and scripts read this)
    println!("listening on http://{addr}");
    println!("space: {}", space.root().display());
    for proxy in &proxies {
        println!(
            "  {name}: read  http://{addr}/t/THREAD?key={key}&nonce=NONCE\n\
             {pad}   write http://{addr}/t/THREAD/write?key={key}&turn=N&nonce=NONCE&b64=REPLY",
            name = proxy.name,
            key = proxy.key,
            pad = " ".repeat(proxy.name.as_str().len()),
        );
    }
    println!("replace NONCE with a different random ASCII value before every request");
    println!(
        "expose publicly with: tailscale funnel {}",
        addr.to_string().rsplit(':').next().unwrap_or("PORT")
    );

    for request in server.incoming_requests() {
        let reply = handle(&space, &proxies, request.url());
        let (status, body, content_type) = match reply {
            Reply::Text(status, body) => (status, body, "text/plain; charset=utf-8"),
            // write outcomes are full HTML documents at 200: fetch-and-parse
            // tools (the only callers) choke on bare-text acks and may hide
            // non-2xx responses from their model entirely — the outcome must
            // live in a parseable page, not in a status code
            Reply::Page(title, body) => (200, html_page(&title, &body), "text/html; charset=utf-8"),
        };
        let response = tiny_http::Response::from_string(body)
            .with_status_code(status)
            .with_header(
                tiny_http::Header::from_bytes(&b"Content-Type"[..], content_type.as_bytes())
                    .expect("static header"),
            )
            // proxied participants sit behind aggressive fetch caches; tell
            // every cache on the path that this is live state, not content
            .with_header(
                tiny_http::Header::from_bytes(
                    &b"Cache-Control"[..],
                    &b"no-store, no-cache, max-age=0, must-revalidate"[..],
                )
                .expect("static header"),
            )
            .with_header(
                tiny_http::Header::from_bytes(&b"Pragma"[..], &b"no-cache"[..])
                    .expect("static header"),
            )
            .with_header(
                tiny_http::Header::from_bytes(&b"Expires"[..], &b"0"[..]).expect("static header"),
            );
        let _ = request.respond(response);
    }
    Ok(())
}

enum Reply {
    /// Plain text with a real status code (the read path — works as-is).
    Text(u16, String),
    /// An HTML document, always 200 (the write path — see serve loop note).
    Page(String, String),
}

fn html_escape(s: &str) -> String {
    s.replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
}

fn html_page(title: &str, body: &str) -> String {
    format!(
        "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>{t}</title></head>\
         <body><h1>{t}</h1><pre>{b}</pre></body></html>",
        t = html_escape(title),
        b = html_escape(body),
    )
}

/// Route and execute one request. Pure-ish: url in, reply out.
fn handle(space: &Space, proxies: &[Proxy], url: &str) -> Reply {
    let (path, query) = url.split_once('?').unwrap_or((url, ""));
    let params: Vec<(String, String)> = form_urlencoded::parse(query.as_bytes())
        .into_owned()
        .collect();
    let param = |name: &str| {
        params
            .iter()
            .find(|(k, _)| k == name)
            .map(|(_, v)| v.as_str())
    };

    let Some(proxy) = param("key").and_then(|k| proxies.iter().find(|p| p.key == k)) else {
        return Reply::Text(403, "missing or unknown key".into());
    };

    let segments: Vec<&str> = path.trim_matches('/').split('/').collect();
    match segments.as_slice() {
        ["t", thread] => {
            let Ok(thread) = Name::new(thread) else {
                return Reply::Text(400, "invalid thread name".into());
            };
            let read_url = format!("/t/{thread}?key={key}", key = proxy.key);
            let write_url = format!("/t/{thread}/write?key={key}", key = proxy.key);
            match brief_text(
                space,
                &thread,
                Some(&proxy.name),
                Some((&read_url, &write_url)),
            ) {
                Ok(text) => Reply::Text(200, text),
                Err(e) => Reply::Text(404, e.to_string()),
            }
        }
        ["t", thread, "write"] => {
            let Ok(thread) = Name::new(thread) else {
                return Reply::Page(
                    "substrate: invalid thread name".into(),
                    "thread names are lowercase a-z0-9- — copy the name from the thread page"
                        .into(),
                );
            };
            // every outcome below includes the refreshed thread page so the
            // assistant's next step never requires another fetch
            let refreshed = |outcome: String| {
                let read_url = format!("/t/{thread}?key={key}", key = proxy.key);
                let write_url = format!("/t/{thread}/write?key={key}", key = proxy.key);
                let brief = brief_text(
                    space,
                    &thread,
                    Some(&proxy.name),
                    Some((&read_url, &write_url)),
                )
                .unwrap_or_default();
                format!("{outcome}\n\n{brief}")
            };
            let content = match (param("b64"), param("text")) {
                (Some(b64), _) => match decode_b64_tolerant(b64) {
                    Ok(text) => text,
                    Err(e) => {
                        return Reply::Page(
                            "substrate: could not decode your reply".into(),
                            refreshed(format!(
                                "the b64 parameter did not decode ({e}). Re-encode your \
                                 reply, or use &text= with percent-encoding instead."
                            )),
                        )
                    }
                },
                (None, Some(text)) => text.to_string(),
                (None, None) => {
                    return Reply::Page(
                        "substrate: missing reply".into(),
                        refreshed("pass your reply as &b64=… or &text=…".into()),
                    )
                }
            };
            if let Some(turn_param) = param("turn") {
                let current = thread_version(space, &thread);
                if turn_param != current.to_string() {
                    return Reply::Page(
                        "substrate: thread changed — entry NOT recorded".into(),
                        refreshed(format!(
                            "someone wrote since you read (version is now {current}, you \
                             replied to {turn_param}). Read the thread below, then resend \
                             with turn={current}."
                        )),
                    );
                }
            }
            match turn::write_entry(space, &thread, &proxy.name, &content) {
                Ok(written) => Reply::Page(
                    "substrate: entry recorded".into(),
                    refreshed(format!(
                        "recorded{no_op} — next turn: {next}{paused}",
                        no_op = if written.no_op { " as a no-op" } else { "" },
                        next = written.next,
                        paused = if written.paused {
                            " (moderator — the thread is paused)"
                        } else {
                            ""
                        },
                    )),
                ),
                Err(e @ SubstrateError::NotYourTurn { .. }) => Reply::Page(
                    "substrate: not your turn — entry NOT recorded".into(),
                    refreshed(format!("{e}. Wait, then fetch the thread page again.")),
                ),
                Err(e @ SubstrateError::Ended) => Reply::Page(
                    "substrate: thread has ended".into(),
                    format!("{e} — no further entries are possible."),
                ),
                Err(e) => Reply::Page("substrate: rejected".into(), refreshed(e.to_string())),
            }
        }
        _ => Reply::Text(404, "routes: /t/<thread>  /t/<thread>/write".into()),
    }
}

/// Models mangle encodings; be liberal. Accepts standard or url-safe
/// alphabets, any padding, and stray whitespace/newlines.
pub fn decode_b64_tolerant(input: &str) -> Result<String> {
    let cleaned: String = input
        .chars()
        .filter(|c| !c.is_whitespace())
        .map(|c| match c {
            '-' => '+',
            '_' => '/',
            other => other,
        })
        .collect();
    let cleaned = cleaned.trim_end_matches('=');
    let padded = format!("{}{}", cleaned, "=".repeat((4 - cleaned.len() % 4) % 4));
    let bytes = base64::engine::general_purpose::STANDARD.decode(padded)?;
    Ok(String::from_utf8(bytes)?)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn b64_tolerance() {
        let encode = |s: &str| base64::engine::general_purpose::STANDARD.encode(s);
        let original = "## reply\n\nwith *markdown*, ünïcode, and a + sign";
        let clean = encode(original);

        // standard, url-safe, unpadded, and whitespace-riddled all decode
        assert_eq!(decode_b64_tolerant(&clean).unwrap(), original);
        let urlsafe = clean.replace('+', "-").replace('/', "_");
        assert_eq!(decode_b64_tolerant(&urlsafe).unwrap(), original);
        let unpadded = clean.trim_end_matches('=');
        assert_eq!(decode_b64_tolerant(unpadded).unwrap(), original);
        let mangled = clean
            .chars()
            .enumerate()
            .flat_map(|(i, c)| if i % 7 == 3 { vec!['\n', c] } else { vec![c] })
            .collect::<String>();
        assert_eq!(decode_b64_tolerant(&mangled).unwrap(), original);

        assert!(decode_b64_tolerant("!!!not base64!!!").is_err());
    }

    #[test]
    fn keys_are_long_and_distinct() {
        let a = random_key();
        let b = random_key();
        assert_eq!(a.len(), 32);
        assert_ne!(a, b);
    }
}
