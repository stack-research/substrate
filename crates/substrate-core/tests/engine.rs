//! Storage + turn engine tests on real (temp) filesystems — the same way
//! every process in the room uses substrate.

use substrate_core::{
    thread::{self, ThreadConfig, ThreadStatus},
    transcript::{self, Window},
    turn, Name, ParticipantKind, Space, SubstrateError,
};
use tempfile::TempDir;

fn n(s: &str) -> Name {
    Name::new(s).unwrap()
}

/// A real group: two humans and three agents from different harnesses,
/// user-name moderating. The engine itself is kind-blind.
fn group_space() -> (TempDir, Space) {
    let dir = TempDir::new().unwrap();
    let space = Space::init(dir.path()).unwrap();
    for (name, kind) in [
        ("user-name", ParticipantKind::Human),
        ("pat", ParticipantKind::Human),
        ("claude-a", ParticipantKind::Agent),
        ("codex-b", ParticipantKind::Agent),
        ("gemini-c", ParticipantKind::Agent),
    ] {
        space.add_participant(n(name), kind).unwrap();
    }
    (dir, space)
}

fn group_thread(space: &Space) -> Name {
    let conv = n("lab");
    thread::create_thread(
        space,
        &conv,
        "storage design",
        &n("user-name"),
        &[n("claude-a"), n("pat"), n("codex-b"), n("gemini-c")],
    )
    .unwrap();
    conv
}

#[test]
fn space_init_open_add() {
    let (dir, space) = group_space();
    assert_eq!(space.config().unwrap().participants.len(), 5);

    // duplicate registration rejected
    let err = space
        .add_participant(n("user-name"), ParticipantKind::Other)
        .unwrap_err();
    assert!(matches!(err, SubstrateError::DuplicateParticipant(_)));

    // reopen works; opening a non-space fails; double init fails
    Space::open(dir.path()).unwrap();
    let empty = TempDir::new().unwrap();
    assert!(matches!(
        Space::open(empty.path()).unwrap_err(),
        SubstrateError::NotASpace(_)
    ));
    assert!(Space::init(dir.path()).is_err());
}

#[test]
fn create_thread_validates() {
    let (_dir, space) = group_space();

    // moderator listed mid-order still lands last, deduped
    let conv = n("review");
    let config = thread::create_thread(
        &space,
        &conv,
        "t",
        &n("user-name"),
        &[n("claude-a"), n("user-name"), n("pat"), n("claude-a")],
    )
    .unwrap();
    assert_eq!(
        config.turn_order,
        vec![n("user-name"), n("claude-a"), n("pat")]
    );

    assert!(matches!(
        thread::create_thread(&space, &conv, "t", &n("user-name"), &[n("pat")]).unwrap_err(),
        SubstrateError::ThreadExists(_)
    ));
    assert!(matches!(
        thread::create_thread(&space, &n("x"), "t", &n("user-name"), &[n("nobody")]).unwrap_err(),
        SubstrateError::UnknownParticipant(_)
    ));
    assert!(matches!(
        thread::create_thread(&space, &n("x"), "t", &n("user-name"), &[]).unwrap_err(),
        SubstrateError::TooFewParticipants
    ));
}

#[test]
fn full_rounds_cycle_through_the_whole_group() {
    let (_dir, space) = group_space();
    let conv = group_thread(&space);
    let order = ["user-name", "claude-a", "pat", "codex-b", "gemini-c"];

    // two full rounds: humans and agents interleaved, identical mechanics
    for round in 0..2 {
        for (i, speaker) in order.iter().enumerate() {
            let status = turn::turn_status(&space, &conv).unwrap();
            assert_eq!(status.current, n(speaker));
            assert_eq!(status.paused, *speaker == "user-name");

            let written = turn::write_entry(
                &space,
                &conv,
                &n(speaker),
                &format!("r{round} from {speaker}"),
            )
            .unwrap();
            assert!(!written.no_op);
            assert_eq!(written.next, n(order[(i + 1) % order.len()]));
        }
    }

    let entries = transcript::load_entries(&space, &conv).unwrap();
    assert_eq!(entries.len(), 10);
    let speakers: Vec<&str> = entries.iter().map(|e| e.meta.author.as_str()).collect();
    assert_eq!(&speakers[..5], &order);
    assert_eq!(&speakers[5..], &order);
}

#[test]
fn write_rejections() {
    let (_dir, space) = group_space();
    let conv = group_thread(&space);

    match turn::write_entry(&space, &conv, &n("pat"), "out of turn").unwrap_err() {
        SubstrateError::NotYourTurn { current } => assert_eq!(current, n("user-name")),
        other => panic!("expected NotYourTurn, got {other}"),
    }
    assert!(matches!(
        turn::write_entry(&space, &n("nope"), &n("pat"), "x").unwrap_err(),
        SubstrateError::UnknownThread(_)
    ));

    // an unregistered-in-this-room (but registered-in-space) writer
    let conv2 = n("duo");
    thread::create_thread(&space, &conv2, "t", &n("user-name"), &[n("pat")]).unwrap();
    assert!(matches!(
        turn::write_entry(&space, &conv2, &n("claude-a"), "let me in").unwrap_err(),
        SubstrateError::NotInThread(_)
    ));
}

#[test]
fn no_op_turns_advance_but_stay_invisible() {
    let (_dir, space) = group_space();
    let conv = group_thread(&space);

    turn::write_entry(
        &space,
        &conv,
        &n("user-name"),
        "welcome — topic on the board",
    )
    .unwrap();
    turn::write_entry(&space, &conv, &n("claude-a"), "real entry").unwrap();
    let written = turn::write_entry(&space, &conv, &n("pat"), "  PASS ").unwrap();
    assert!(written.no_op);
    assert!(written.filename.ends_with("__pat__no-op.md"));
    assert_eq!(written.next, n("codex-b"));

    let entries = transcript::load_entries(&space, &conv).unwrap();
    assert_eq!(entries.len(), 2);
    assert_eq!(entries[1].meta.author, n("claude-a"));

    // the no-op file is on disk regardless (append-only record)
    let on_disk = std::fs::read_dir(space.thread_dir(&conv))
        .unwrap()
        .filter_map(|e| e.unwrap().file_name().into_string().ok())
        .filter(|f| f.ends_with(".md"))
        .count();
    assert_eq!(on_disk, 3);
}

#[test]
fn quiet_skips_and_expires() {
    let (_dir, space) = group_space();
    let conv = group_thread(&space);

    // the room opens paused on user-name — moderation works straight away
    turn::quiet(&space, &conv, &n("pat"), 2).unwrap();
    assert!(matches!(
        turn::quiet(&space, &conv, &n("user-name"), 1).unwrap_err(),
        SubstrateError::CannotQuietModerator
    ));

    turn::write_entry(&space, &conv, &n("user-name"), "pat, sit out two rounds").unwrap();

    // round 1: pat skipped (counter 2 -> 1)
    turn::write_entry(&space, &conv, &n("claude-a"), "r1").unwrap();
    let status = turn::turn_status(&space, &conv).unwrap();
    assert_eq!(status.current, n("codex-b"));
    turn::write_entry(&space, &conv, &n("codex-b"), "r1").unwrap();
    turn::write_entry(&space, &conv, &n("gemini-c"), "r1").unwrap();
    turn::write_entry(&space, &conv, &n("user-name"), "pass").unwrap();

    // round 2: pat skipped (counter 1 -> 0, removed)
    turn::write_entry(&space, &conv, &n("claude-a"), "r2").unwrap();
    assert_eq!(
        turn::turn_status(&space, &conv).unwrap().current,
        n("codex-b")
    );
    turn::write_entry(&space, &conv, &n("codex-b"), "r2").unwrap();
    turn::write_entry(&space, &conv, &n("gemini-c"), "r2").unwrap();
    turn::write_entry(&space, &conv, &n("user-name"), "pass").unwrap();

    // round 3: pat speaks again
    turn::write_entry(&space, &conv, &n("claude-a"), "r3").unwrap();
    let status = turn::turn_status(&space, &conv).unwrap();
    assert_eq!(status.current, n("pat"));
    assert!(status.quieted.is_empty());
    turn::write_entry(&space, &conv, &n("pat"), "back!").unwrap();
}

#[test]
fn moderator_ops_work_anytime_and_reorder_invite_end() {
    let (_dir, space) = group_space();
    let conv = group_thread(&space);

    // user-name opens the room (their first entry is the instructions slot)
    turn::write_entry(&space, &conv, &n("user-name"), "ground rules: be brief").unwrap();

    // now it's claude-a's turn, but the moderator can still adjust the room
    turn::set_topic(&space, &conv, "new").unwrap();
    assert_eq!(
        turn::turn_status(&space, &conv).unwrap().current,
        n("claude-a")
    );
    turn::invite(&space, &conv, &n("gemini-c")).unwrap();
    let config = ThreadConfig::load(&space, &conv).unwrap();
    assert!(config.turn_order.contains(&n("gemini-c")));
    assert_eq!(config.current(), &n("claude-a"));

    // reordering preserves the current speaker when they remain in the room
    turn::reorder_turns(
        &space,
        &conv,
        &[n("pat"), n("claude-a"), n("codex-b"), n("gemini-c")],
    )
    .unwrap();
    let config = ThreadConfig::load(&space, &conv).unwrap();
    assert_eq!(
        config.turn_order,
        vec![
            n("user-name"),
            n("pat"),
            n("claude-a"),
            n("codex-b"),
            n("gemini-c")
        ]
    );
    assert_eq!(config.current(), &n("claude-a"));

    // walk the current round back to user-name. Pat moved before the current
    // speaker, so they wait until the next round.
    for speaker in ["claude-a", "codex-b", "gemini-c"] {
        turn::write_entry(&space, &conv, &n(speaker), "hi").unwrap();
    }

    // chain several adjustments during one pause; none consume the turn
    turn::set_topic(&space, &conv, "narrowed topic").unwrap();
    turn::reorder_turns(&space, &conv, &[n("pat"), n("claude-a")]).unwrap();
    let config = ThreadConfig::load(&space, &conv).unwrap();
    assert_eq!(
        config.turn_order,
        vec![n("user-name"), n("pat"), n("claude-a")]
    );
    assert_eq!(config.current(), &n("user-name"));
    assert_eq!(config.topic, "narrowed topic");

    turn::invite(&space, &conv, &n("gemini-c")).unwrap();
    let config = ThreadConfig::load(&space, &conv).unwrap();
    assert_eq!(
        config.turn_order,
        vec![n("user-name"), n("pat"), n("claude-a"), n("gemini-c")]
    );
    assert_eq!(config.current(), &n("user-name"));

    // moderator ends their turn by writing; new order takes effect
    turn::write_entry(&space, &conv, &n("user-name"), "resuming with new order").unwrap();
    assert_eq!(turn::turn_status(&space, &conv).unwrap().current, n("pat"));

    // walk back to user-name and end the thread
    turn::write_entry(&space, &conv, &n("pat"), "x").unwrap();
    turn::write_entry(&space, &conv, &n("claude-a"), "x").unwrap();
    turn::write_entry(&space, &conv, &n("gemini-c"), "x").unwrap();
    turn::end_thread(&space, &conv).unwrap();

    assert!(matches!(
        turn::write_entry(&space, &conv, &n("pat"), "too late").unwrap_err(),
        SubstrateError::Ended
    ));
    assert_eq!(
        turn::turn_status(&space, &conv).unwrap().status,
        ThreadStatus::Ended
    );
    // history stays readable
    assert!(!transcript::load_entries(&space, &conv).unwrap().is_empty());
}

#[test]
fn rapid_writes_never_lose_entries() {
    let (_dir, space) = group_space();
    let conv = n("duo");
    thread::create_thread(&space, &conv, "t", &n("user-name"), &[n("claude-a")]).unwrap();

    // sub-millisecond alternation exercises the filename collision bump
    for i in 0..50 {
        turn::write_entry(&space, &conv, &n("user-name"), &format!("d{i}")).unwrap();
        turn::write_entry(&space, &conv, &n("claude-a"), &format!("a{i}")).unwrap();
    }
    let entries = transcript::load_entries(&space, &conv).unwrap();
    assert_eq!(entries.len(), 100);
    // chronological and alternating
    for pair in entries.chunks(2) {
        assert_eq!(pair[0].meta.author, n("user-name"));
        assert_eq!(pair[1].meta.author, n("claude-a"));
    }
}

#[test]
fn torn_writes_and_strays_are_invisible() {
    let (_dir, space) = group_space();
    let conv = group_thread(&space);
    turn::write_entry(&space, &conv, &n("user-name"), "real").unwrap();

    let dir = space.thread_dir(&conv);
    std::fs::write(dir.join(".tmp-999-0"), "half a write").unwrap();
    std::fs::write(dir.join("NOTES.md"), "a stray hand-dropped file").unwrap();

    let entries = transcript::load_entries(&space, &conv).unwrap();
    assert_eq!(entries.len(), 1);
    assert_eq!(entries[0].body, "real");
}

#[test]
fn transcript_lines_are_a_stable_cursor() {
    let (_dir, space) = group_space();
    let conv = group_thread(&space);

    turn::write_entry(&space, &conv, &n("user-name"), "one\ntwo").unwrap();
    let (_, total_before) = transcript::window(
        &transcript::render_transcript(&transcript::load_entries(&space, &conv).unwrap()),
        Window::All,
    );

    turn::write_entry(&space, &conv, &n("claude-a"), "pass").unwrap(); // invisible
    turn::write_entry(&space, &conv, &n("pat"), "three").unwrap();

    let rendered = transcript::render_transcript(&transcript::load_entries(&space, &conv).unwrap());
    let (new_part, total_after) = transcript::window(&rendered, Window::FromLine(total_before + 1));
    assert!(total_after > total_before);
    assert!(new_part.contains("pat"));
    assert!(new_part.contains("three"));
    assert!(!new_part.contains("two")); // nothing old re-sent, nothing shifted
    assert!(!rendered.contains("pass")); // the no-op never rendered
}

#[test]
fn ended_threads_can_be_resumed_by_the_moderator() {
    let (_dir, space) = group_space();
    let conv = group_thread(&space);

    // resume on an active thread is rejected
    assert!(matches!(
        turn::resume_thread(&space, &conv).unwrap_err(),
        SubstrateError::NotEnded
    ));

    // user-name opens, ends the room on a later pause
    turn::write_entry(&space, &conv, &n("user-name"), "opening").unwrap();
    for speaker in ["claude-a", "pat", "codex-b", "gemini-c"] {
        turn::write_entry(&space, &conv, &n(speaker), "hi").unwrap();
    }
    turn::end_thread(&space, &conv).unwrap();
    assert!(matches!(
        turn::write_entry(&space, &conv, &n("user-name"), "too late").unwrap_err(),
        SubstrateError::Ended
    ));

    // resume: floor returns to the moderator, the round restarts cleanly
    turn::resume_thread(&space, &conv).unwrap();
    let status = turn::turn_status(&space, &conv).unwrap();
    assert_eq!(status.status, ThreadStatus::Active);
    assert_eq!(status.current, n("user-name"));
    assert!(status.paused);

    turn::write_entry(&space, &conv, &n("user-name"), "we're back — new question").unwrap();
    assert_eq!(
        turn::turn_status(&space, &conv).unwrap().current,
        n("claude-a")
    );

    // history survived: opening + 4 + reopening = 6 visible entries
    assert_eq!(transcript::load_entries(&space, &conv).unwrap().len(), 6);
}

#[test]
fn next_redirects_the_floor_mid_round() {
    let (_dir, space) = group_space();
    let conv = group_thread(&space);

    // user-name opens; the floor is claude-a's — but user-name calls on gemini-c mid-round
    turn::write_entry(&space, &conv, &n("user-name"), "opening").unwrap();
    turn::set_next(&space, &conv, &n("gemini-c")).unwrap();
    assert_eq!(
        turn::turn_status(&space, &conv).unwrap().current,
        n("gemini-c")
    );

    // the skipped speaker is rejected; the chosen one writes, order resumes
    assert!(matches!(
        turn::write_entry(&space, &conv, &n("claude-a"), "wait, me?").unwrap_err(),
        SubstrateError::NotYourTurn { .. }
    ));
    turn::write_entry(&space, &conv, &n("gemini-c"), "called on").unwrap();
    assert_eq!(
        turn::turn_status(&space, &conv).unwrap().current,
        n("user-name")
    );

    // calling on a quieted participant clears their counter (explicit wins)
    turn::quiet(&space, &conv, &n("pat"), 3).unwrap();
    turn::set_next(&space, &conv, &n("pat")).unwrap();
    let status = turn::turn_status(&space, &conv).unwrap();
    assert_eq!(status.current, n("pat"));
    assert!(status.quieted.is_empty());
    turn::write_entry(&space, &conv, &n("pat"), "unquieted by the baton").unwrap();

    // the baton can also pull the floor back to the moderator
    turn::set_next(&space, &conv, &n("user-name")).unwrap();
    assert!(turn::turn_status(&space, &conv).unwrap().paused);

    // outsiders and ended threads are rejected
    let stranger = n("nobody");
    assert!(matches!(
        turn::set_next(&space, &conv, &stranger).unwrap_err(),
        SubstrateError::NotInThread(_)
    ));
    turn::end_thread(&space, &conv).unwrap();
    assert!(matches!(
        turn::set_next(&space, &conv, &n("pat")).unwrap_err(),
        SubstrateError::Ended
    ));
}
