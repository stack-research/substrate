//! Drive the real substrate-mcp binary through the MCP protocol, the way an
//! agent harness would — two server processes (two agents) sharing one space.

use std::time::Duration;

use rmcp::model::CallToolRequestParams;
use rmcp::service::RunningService;
use rmcp::transport::TokioChildProcess;
use rmcp::{RoleClient, ServiceExt};
use substrate_core::{thread, turn, Name, ParticipantKind, Space};
use tempfile::TempDir;
use tokio::process::Command;

type Client = RunningService<RoleClient, ()>;

async fn connect(space: &TempDir, me: &str) -> Client {
    connect_args(&[("--space", space.path().to_str().unwrap()), ("--name", me)]).await
}

async fn connect_args(args: &[(&str, &str)]) -> Client {
    let mut cmd = Command::new(env!("CARGO_BIN_EXE_substrate-mcp"));
    for (flag, value) in args {
        cmd.arg(flag).arg(value);
    }
    ().serve(TokioChildProcess::new(cmd).unwrap())
        .await
        .unwrap()
}

async fn call(client: &Client, tool: &str, args: serde_json::Value) -> (String, bool) {
    let result = client
        .call_tool(
            CallToolRequestParams::new(tool.to_string())
                .with_arguments(args.as_object().cloned().unwrap_or_default()),
        )
        .await
        .unwrap();
    let is_error = result.is_error.unwrap_or(false);
    let value = serde_json::to_value(&result).unwrap();
    let text = value["content"][0]["text"]
        .as_str()
        .unwrap_or("")
        .to_string();
    (text, is_error)
}

fn set_up_space() -> (TempDir, Space) {
    let dir = TempDir::new().unwrap();
    let space = Space::init(dir.path()).unwrap();
    for (name, kind) in [
        ("user-name", ParticipantKind::Human),
        ("claude-a", ParticipantKind::Agent),
        ("codex-b", ParticipantKind::Agent),
    ] {
        space
            .add_participant(Name::new(name).unwrap(), kind)
            .unwrap();
    }
    thread::create_thread(
        &space,
        &Name::new("lab").unwrap(),
        "protocol test",
        &Name::new("user-name").unwrap(),
        &[
            Name::new("claude-a").unwrap(),
            Name::new("codex-b").unwrap(),
        ],
    )
    .unwrap();
    (dir, space)
}

#[tokio::test]
async fn two_agents_converse_through_the_protocol() {
    let (dir, space) = set_up_space();
    let a = connect(&dir, "claude-a").await;
    let b = connect(&dir, "codex-b").await;

    // all tools advertised — six participant tools plus the moderator floor-ops
    let tools = a.list_all_tools().await.unwrap();
    let mut names: Vec<&str> = tools.iter().map(|t| t.name.as_ref()).collect();
    names.sort();
    assert_eq!(
        names,
        [
            "about",
            "check_turn",
            "end_thread",
            "invite",
            "list_threads",
            "new_thread",
            "quiet",
            "read_thread",
            "reorder_turns",
            "resume_thread",
            "set_next",
            "set_topic",
            "wait_for_turn",
            "write_entry"
        ]
    );

    // the orientation tool teaches the loop; every status response carries
    // the option surface so agents never depend on unseen instructions
    let (text, err) = call(&a, "about", serde_json::json!({})).await;
    assert!(!err);
    assert!(text.contains("TIMEOUT MEANS STILL WAITING"), "{text}");
    assert!(
        text.contains(&format!("server version: {}", env!("CARGO_PKG_VERSION"))),
        "{text}"
    );
    for tool in names {
        assert!(text.contains(tool), "about output missing {tool}: {text}");
    }
    // thread just opened: user-name (moderator-first) holds the floor, so claude-a
    // gets the not-your-turn affordance
    let (text, _) = call(&a, "check_turn", serde_json::json!({"thread": "lab"})).await;
    assert!(text.contains("→ not your turn"), "{text}");

    // the thread opens paused on user-name (moderator first); user-name sets instructions
    turn::write_entry(
        &space,
        &Name::new("lab").unwrap(),
        &Name::new("user-name").unwrap(),
        "moderator opening",
    )
    .unwrap();

    // now the floor is claude-a's; wait_for_turn returns immediately
    let (text, err) = call(&a, "wait_for_turn", serde_json::json!({"thread": "lab"})).await;
    assert!(!err);
    assert!(text.contains("your turn: yes"), "{text}");

    let (text, err) = call(
        &a,
        "write_entry",
        serde_json::json!({"thread": "lab", "content": "Opening thought from claude-a."}),
    )
    .await;
    assert!(!err);
    assert!(text.contains("next turn: codex-b"), "{text}");

    // writing twice is rejected as a tool error the model can read
    let (text, err) = call(
        &a,
        "write_entry",
        serde_json::json!({"thread": "lab", "content": "double dip"}),
    )
    .await;
    assert!(err);
    assert!(text.contains("codex-b"), "{text}");

    // codex-b sees the thread, takes a no-op turn
    let (text, _) = call(&b, "list_threads", serde_json::json!({})).await;
    assert!(
        text.contains("thread: lab") && text.contains("turn: codex-b (you)"),
        "{text}"
    );

    let (text, err) = call(
        &b,
        "write_entry",
        serde_json::json!({"thread": "lab", "content": "pass"}),
    )
    .await;
    assert!(!err);
    assert!(text.contains("no-op"), "{text}");
    assert!(text.contains("the thread is paused"), "{text}");

    // the shared transcript shows the entry, hides the no-op, reports lines
    let (text, _) = call(&b, "read_thread", serde_json::json!({"thread": "lab"})).await;
    assert!(text.contains("Opening thought from claude-a."), "{text}");
    assert!(!text.contains("pass\n"), "{text}");
    assert!(text.contains("transcript lines: 6"), "{text}");

    // from_line windowing through the protocol
    let (text, _) = call(
        &b,
        "read_thread",
        serde_json::json!({"thread": "lab", "from_line": 5}),
    )
    .await;
    assert!(!text.contains("[claude-a @"), "{text}");
    assert!(text.contains("Opening thought"), "{text}");

    // user-name (moderator, human, in the TUI in real life) holds the floor: b times out
    let (text, err) = call(
        &b,
        "wait_for_turn",
        serde_json::json!({"thread": "lab", "timeout_secs": 1}),
    )
    .await;
    assert!(!err);
    assert!(text.contains("timed out"), "{text}");
    assert!(text.contains("paused on moderator: yes"), "{text}");

    // user-name writes (same engine the TUI uses); the floor returns to claude-a
    turn::write_entry(
        &space,
        &Name::new("lab").unwrap(),
        &Name::new("user-name").unwrap(),
        "moderator resuming",
    )
    .unwrap();

    let (text, err) = call(&a, "wait_for_turn", serde_json::json!({"thread": "lab"})).await;
    assert!(!err);
    assert!(text.contains("your turn: yes"), "{text}");

    a.cancel().await.unwrap();
    b.cancel().await.unwrap();
}

#[tokio::test]
async fn per_call_identity_drives_multiple_personas_with_default_fallback() {
    let (dir, space) = set_up_space();
    let multi = connect_args(&[("--space", dir.path().to_str().unwrap())]).await;
    let fallback = connect(&dir, "codex-b").await;

    let (text, err) = call(&multi, "list_threads", serde_json::json!({})).await;
    assert!(err, "{text}");
    assert!(text.contains("participant_name is required"), "{text}");

    let (text, err) = call(
        &multi,
        "check_turn",
        serde_json::json!({"thread": "lab", "participant_name": "claude-a"}),
    )
    .await;
    assert!(!err, "{text}");
    assert!(text.contains("participant: claude-a"), "{text}");
    assert!(text.contains("→ not your turn"), "{text}");

    // thread opens moderator-first; once user-name opens, claude-a holds the floor.
    turn::write_entry(
        &space,
        &Name::new("lab").unwrap(),
        &Name::new("user-name").unwrap(),
        "moderator opening",
    )
    .unwrap();

    let (text, err) = call(
        &multi,
        "write_entry",
        serde_json::json!({
            "thread": "lab",
            "participant_name": "codex-b",
            "content": "too early for codex-b"
        }),
    )
    .await;
    assert!(err, "{text}");
    assert!(text.contains("claude-a"), "{text}");

    let (text, err) = call(
        &multi,
        "list_threads",
        serde_json::json!({"participant_name": "claude-a"}),
    )
    .await;
    assert!(!err, "{text}");
    assert!(text.contains("turn: claude-a (you)"), "{text}");

    let (text, err) = call(
        &multi,
        "write_entry",
        serde_json::json!({
            "thread": "lab",
            "participant_name": "claude-a",
            "content": "per-call identity from claude-a"
        }),
    )
    .await;
    assert!(!err, "{text}");
    assert!(text.contains("next turn: codex-b"), "{text}");

    // Existing --name configurations remain backward compatible: no
    // participant_name falls back to the launch default.
    let (text, err) = call(
        &fallback,
        "write_entry",
        serde_json::json!({"thread": "lab", "content": "fallback identity from codex-b"}),
    )
    .await;
    assert!(!err, "{text}");

    let entries =
        substrate_core::transcript::load_entries(&space, &Name::new("lab").unwrap()).unwrap();
    let authors: Vec<&str> = entries.iter().map(|e| e.meta.author.as_str()).collect();
    assert_eq!(authors, ["user-name", "claude-a", "codex-b"]);

    multi.cancel().await.unwrap();
    fallback.cancel().await.unwrap();
}

#[tokio::test]
async fn new_thread_creates_via_core_and_opens_on_moderator() {
    let (dir, space) = set_up_space();
    let creator = connect(&dir, "claude-a").await;

    let (text, err) = call(
        &creator,
        "new_thread",
        serde_json::json!({
            "name": "fresh-lab",
            "topic": "mcp-created room",
            "moderator": "user-name",
            "turn_order": ["claude-a", "user-name", "codex-b"]
        }),
    )
    .await;
    assert!(!err, "{text}");
    assert!(text.contains("created thread: fresh-lab"), "{text}");
    assert!(text.contains("opening floor: user-name"), "{text}");
    assert!(text.contains("paused on moderator: yes"), "{text}");
    assert!(
        text.contains("turn order: user-name (moderator) → claude-a → codex-b"),
        "{text}"
    );

    let thread = Name::new("fresh-lab").unwrap();
    let status = turn::turn_status(&space, &thread).unwrap();
    let order: Vec<&str> = status.turn_order.iter().map(Name::as_str).collect();
    assert_eq!(order, ["user-name", "claude-a", "codex-b"]);
    assert_eq!(status.current.as_str(), "user-name");
    assert!(status.paused);

    // Turn enforcement is immediate on the created thread: the creator is
    // registered and in the room, but cannot write before the moderator opens.
    let (text, err) = call(
        &creator,
        "write_entry",
        serde_json::json!({"thread": "fresh-lab", "content": "too early"}),
    )
    .await;
    assert!(err, "{text}");
    assert!(text.contains("user-name"), "{text}");

    creator.cancel().await.unwrap();
}

#[tokio::test]
async fn new_thread_requires_registered_creator() {
    let (dir, space) = set_up_space();
    let stranger = connect(&dir, "stranger").await;

    let (text, err) = call(
        &stranger,
        "new_thread",
        serde_json::json!({
            "name": "rogue-room",
            "topic": "should not exist",
            "moderator": "user-name",
            "turn_order": ["claude-a"]
        }),
    )
    .await;
    assert!(err, "{text}");
    assert!(text.contains("only registered participants"), "{text}");
    assert!(turn::turn_status(&space, &Name::new("rogue-room").unwrap()).is_err());

    stranger.cancel().await.unwrap();
}

/// One server, many spaces: a registry file maps labels to roots, tools take
/// a `space` argument, list_threads federates, and an ambiguous call
/// without `space` is rejected.
#[tokio::test]
async fn one_server_many_spaces() {
    let root = TempDir::new().unwrap();
    let mut spaces = Vec::new();
    for label in ["alpha", "beta"] {
        let dir = root.path().join(label);
        let space = Space::init(&dir).unwrap();
        for name in ["user-name", "claude-a"] {
            space
                .add_participant(
                    Name::new(name).unwrap(),
                    if name == "user-name" {
                        ParticipantKind::Human
                    } else {
                        ParticipantKind::Agent
                    },
                )
                .unwrap();
        }
        thread::create_thread(
            &space,
            &Name::new("thread").unwrap(),
            &format!("topic in {label}"),
            &Name::new("user-name").unwrap(),
            &[Name::new("claude-a").unwrap()],
        )
        .unwrap();
        turn::write_entry(
            &space,
            &Name::new("thread").unwrap(),
            &Name::new("user-name").unwrap(),
            "moderator opening",
        )
        .unwrap();
        spaces.push(space);
    }
    let registry = root.path().join("spaces.yaml");
    std::fs::write(
        &registry,
        format!(
            "spaces:\n  alpha: {}\n  beta: {}\n",
            root.path().join("alpha").display(),
            root.path().join("beta").display()
        ),
    )
    .unwrap();

    let client = connect_args(&[
        ("--spaces-file", registry.to_str().unwrap()),
        ("--name", "claude-a"),
    ])
    .await;

    // federated listing labels both threads
    let (text, _) = call(&client, "list_threads", serde_json::json!({})).await;
    assert!(text.contains("thread: thread · space: alpha"), "{text}");
    assert!(text.contains("thread: thread · space: beta"), "{text}");
    assert!(text.contains("topic in beta"), "{text}");

    // ambiguous calls are rejected with the configured labels
    let result = client
        .call_tool(
            CallToolRequestParams::new("check_turn".to_string()).with_arguments(
                serde_json::json!({"thread": "thread"})
                    .as_object()
                    .cloned()
                    .unwrap(),
            ),
        )
        .await;
    let err = format!("{result:?}");
    assert!(result.is_err(), "{err}");
    assert!(err.contains("alpha"), "{err}");

    // writes are addressed per space and land in the right thread
    let (text, err) = call(
        &client,
        "write_entry",
        serde_json::json!({"space": "beta", "thread": "thread", "content": "hello beta"}),
    )
    .await;
    assert!(!err, "{text}");
    let beta_entries =
        substrate_core::transcript::load_entries(&spaces[1], &Name::new("thread").unwrap())
            .unwrap();
    assert_eq!(beta_entries.len(), 2); // user-name's opening + claude-a's reply
    let alpha_entries =
        substrate_core::transcript::load_entries(&spaces[0], &Name::new("thread").unwrap())
            .unwrap();
    assert_eq!(alpha_entries.len(), 1); // only user-name's opening

    // wait_for_turn wakes promptly on a file event, not at the poll interval
    let waiter = {
        let client_alpha = connect_args(&[
            ("--spaces-file", registry.to_str().unwrap()),
            ("--name", "claude-a"),
        ])
        .await;
        tokio::spawn(async move {
            let started = std::time::Instant::now();
            // alpha thread: user-name holds nothing yet — it IS claude-a's turn there,
            // so use beta where user-name now holds the floor.
            let (text, _) = call(
                &client_alpha,
                "wait_for_turn",
                serde_json::json!({"space": "beta", "thread": "thread", "timeout_secs": 30}),
            )
            .await;
            client_alpha.cancel().await.unwrap();
            (text, started.elapsed())
        })
    };
    tokio::time::sleep(Duration::from_millis(800)).await;
    turn::write_entry(
        &spaces[1],
        &Name::new("thread").unwrap(),
        &Name::new("user-name").unwrap(),
        "back to you",
    )
    .unwrap();
    let (text, elapsed) = waiter.await.unwrap();
    assert!(text.contains("your turn: yes"), "{text}");
    assert!(
        elapsed < Duration::from_secs(5),
        "watch-wake took {elapsed:?} — should be near-instant after the write"
    );

    client.cancel().await.unwrap();
}

/// A space whose thread is moderated by an AGENT (claude-a), so an MCP server
/// running as that agent can exercise the moderator floor-ops.
fn set_up_agent_moderated() -> (TempDir, Space) {
    let dir = TempDir::new().unwrap();
    let space = Space::init(dir.path()).unwrap();
    for name in ["claude-a", "codex-b"] {
        space
            .add_participant(Name::new(name).unwrap(), ParticipantKind::Agent)
            .unwrap();
    }
    thread::create_thread(
        &space,
        &Name::new("modtest").unwrap(),
        "agent-moderated",
        &Name::new("claude-a").unwrap(),
        &[Name::new("codex-b").unwrap()],
    )
    .unwrap();
    (dir, space)
}

/// Moderator floor-ops are exposed over MCP but gated to the thread's
/// moderator: a non-moderator is refused (with the role named), and the
/// moderator can drive the whole set — including inviting a brand-new name.
#[tokio::test]
async fn moderator_ops_are_role_gated() {
    let (dir, space) = set_up_agent_moderated();
    let thread = Name::new("modtest").unwrap();
    let moderator = connect(&dir, "claude-a").await; // moderates the thread
    let member = connect(&dir, "codex-b").await; // an ordinary participant

    // a non-moderator is refused, and told who holds the role
    let (text, err) = call(
        &member,
        "set_next",
        serde_json::json!({"thread": "modtest", "name": "codex-b"}),
    )
    .await;
    assert!(err, "{text}");
    assert!(text.contains("claude-a"), "{text}");

    // the moderator invites a brand-new name → registered as an agent, joins last
    let (text, err) = call(
        &moderator,
        "invite",
        serde_json::json!({"thread": "modtest", "name": "hermes-x"}),
    )
    .await;
    assert!(!err, "{text}");
    assert!(text.contains("registered as a new agent"), "{text}");
    assert!(text.contains("hermes-x"), "{text}");

    // set the topic and the speaking order
    let (text, err) = call(
        &moderator,
        "set_topic",
        serde_json::json!({"thread": "modtest", "topic": "agent-run room"}),
    )
    .await;
    assert!(!err, "{text}");
    assert!(text.contains("agent-run room"), "{text}");

    let (text, err) = call(
        &moderator,
        "reorder_turns",
        serde_json::json!({"thread": "modtest", "order": ["codex-b", "hermes-x"]}),
    )
    .await;
    assert!(!err, "{text}");

    // hand the floor to codex-b, then quiet them
    let (text, err) = call(
        &moderator,
        "set_next",
        serde_json::json!({"thread": "modtest", "name": "codex-b"}),
    )
    .await;
    assert!(!err, "{text}");
    assert!(text.contains("current turn: codex-b"), "{text}");

    let (text, err) = call(
        &moderator,
        "quiet",
        serde_json::json!({"thread": "modtest", "name": "codex-b", "turns": 2}),
    )
    .await;
    assert!(!err, "{text}");
    assert!(text.contains("quieted for 2"), "{text}");

    // end then resume — reversible; the floor returns to the moderator
    let (text, err) = call(
        &moderator,
        "end_thread",
        serde_json::json!({"thread": "modtest"}),
    )
    .await;
    assert!(!err, "{text}");
    assert!(text.contains("Ended"), "{text}");

    let (text, err) = call(
        &moderator,
        "resume_thread",
        serde_json::json!({"thread": "modtest"}),
    )
    .await;
    assert!(!err, "{text}");
    assert!(text.contains("the floor is yours"), "{text}");

    // the engine on disk reflects the moderator's edits
    let status = turn::turn_status(&space, &thread).unwrap();
    assert!(status.turn_order.iter().any(|n| n.as_str() == "hermes-x"));
    assert_eq!(status.moderator.as_str(), "claude-a");

    moderator.cancel().await.unwrap();
    member.cancel().await.unwrap();
}

#[tokio::test]
async fn per_call_identity_gates_moderator_ops() {
    let (dir, _space) = set_up_agent_moderated();
    let multi = connect_args(&[("--space", dir.path().to_str().unwrap())]).await;

    let (text, err) = call(
        &multi,
        "set_next",
        serde_json::json!({
            "thread": "modtest",
            "participant_name": "codex-b",
            "name": "codex-b"
        }),
    )
    .await;
    assert!(err, "{text}");
    assert!(text.contains("claude-a"), "{text}");

    let (text, err) = call(
        &multi,
        "set_next",
        serde_json::json!({
            "thread": "modtest",
            "participant_name": "claude-a",
            "name": "codex-b"
        }),
    )
    .await;
    assert!(!err, "{text}");
    assert!(text.contains("current turn: codex-b"), "{text}");
    assert!(text.contains("participant: claude-a"), "{text}");

    multi.cancel().await.unwrap();
}
