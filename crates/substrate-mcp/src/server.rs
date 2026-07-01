use std::time::Duration;

use notify::Watcher;
use rmcp::handler::server::wrapper::Parameters;
use rmcp::model::{CallToolResult, Content, Implementation, ServerCapabilities, ServerInfo};
use rmcp::{tool, tool_handler, tool_router, ErrorData, ServerHandler};
use serde::Deserialize;
use substrate_core::{
    thread,
    transcript::{self, Window},
    turn, Name, ParticipantKind, Space, SubstrateError,
};

use crate::spaces::{SpaceSet, SpaceSource};

/// Fallback re-check interval while waiting — the file watcher is the real
/// wake-up; this only guards against missed events.
const WAIT_FALLBACK_INTERVAL: Duration = Duration::from_secs(15);
const WAIT_DEFAULT_SECS: u64 = 120;
const WAIT_MAX_SECS: u64 = 600;
const TOOL_NAMES: &[&str] = &[
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
    "write_entry",
];

#[derive(Clone)]
pub struct SubstrateServer {
    source: SpaceSource,
    default_actor: Option<Name>,
}

impl SubstrateServer {
    /// Startup never fails for an empty registry or an unregistered name —
    /// spaces can be created (and the agent registered) mid-session; the
    /// registry is re-read on every tool call.
    pub fn new(source: SpaceSource, default_actor: Option<&str>) -> anyhow::Result<Self> {
        let default_actor = default_actor.map(Name::new).transpose()?;
        if let Some(me) = &default_actor {
            match source.load() {
                Ok(set) if set.is_empty() => {
                    tracing::warn!(%me, "no spaces configured yet ({})", source.describe());
                }
                Ok(set) => {
                    let registered = set.registered_in(me);
                    if registered.is_empty() {
                        tracing::warn!(
                            %me,
                            spaces = set.labels().join(","),
                            "not registered in any configured space (yet)"
                        );
                    }
                }
                Err(e) => tracing::warn!("could not load spaces at startup: {e}"),
            }
        } else {
            tracing::info!(
                "no default participant configured; identity-bearing tools require participant_name"
            );
        }
        Ok(Self {
            source,
            default_actor,
        })
    }

    fn load(&self) -> Result<SpaceSet, ErrorData> {
        self.source
            .load()
            .map_err(|e| ErrorData::internal_error(e.to_string(), None))
    }

    fn resolve<'a>(set: &'a SpaceSet, label: Option<&str>) -> Result<&'a Space, ErrorData> {
        set.resolve(label)
            .map_err(|message| ErrorData::invalid_params(message, None))
    }

    /// Per Dan's ruling for this trusted local lab, MCP identity may now be
    /// supplied per call so one harness can drive several personas. The guard
    /// remains the turn engine: the resolved participant must be registered,
    /// in the thread, and holding the floor or moderator role for the action.
    fn actor(&self, participant_name: Option<&str>) -> Result<Name, CallToolResult> {
        let Some(raw) = participant_name.or(self.default_actor.as_ref().map(Name::as_str)) else {
            return Err(CallToolResult::error(vec![Content::text(
                "participant_name is required because this substrate-mcp server \
                 was started without --name. Pass the registered participant to \
                 act as, or start the server with --name <participant> as a \
                 default."
                    .to_string(),
            )]));
        };
        Name::new(raw.trim()).map_err(|e| CallToolResult::error(vec![Content::text(e.to_string())]))
    }

    fn default_actor_text(&self) -> String {
        self.default_actor
            .as_ref()
            .map(ToString::to_string)
            .unwrap_or_else(|| "(none; pass participant_name per call)".to_string())
    }

    /// Domain rejections (not your turn, ended, …) are tool-level errors the
    /// model should see and react to — not protocol failures.
    fn domain_error(err: SubstrateError) -> CallToolResult {
        // rejections teach: the model reads this, so say what to do next
        let hint = match &err {
            SubstrateError::NotYourTurn { .. } => {
                "\n→ wait_for_turn will wake you when the floor is yours."
            }
            SubstrateError::Ended => {
                "\n→ this thread is finished; no further turns anywhere in it."
            }
            _ => "",
        };
        CallToolResult::error(vec![Content::text(format!("{err}{hint}"))])
    }

    /// The "your turn: reply / pass / wait" option surface, appended to every
    /// status-bearing response so agents never depend on instructions their
    /// harness may not have shown them.
    fn next_moves(your_turn: bool, ended: bool) -> &'static str {
        if ended {
            "→ this thread is finished — nothing more to do here."
        } else if your_turn {
            "→ your move: read_thread (from_line = last total + 1) to catch up, \
             then write_entry to reply — or write_entry with exactly 'pass' to yield \
             quietly (no-op turns are hidden from the thread)."
        } else {
            "→ not your turn: call wait_for_turn (a timeout means still waiting — call \
             it again). You are only done with a thread when its status is Ended."
        }
    }

    fn render(space: &Space, thread: &Name) -> Result<String, SubstrateError> {
        Ok(transcript::render_transcript(&transcript::load_entries(
            space, thread,
        )?))
    }

    fn status_text(
        &self,
        space: &Space,
        thread: &Name,
        actor: &Name,
    ) -> Result<String, SubstrateError> {
        let status = turn::turn_status(space, thread)?;
        let (_, total_lines) = transcript::window(&Self::render(space, thread)?, Window::All);
        let order: Vec<String> = status
            .turn_order
            .iter()
            .map(|name| {
                if name == &status.moderator {
                    format!("{name} (moderator)")
                } else {
                    name.to_string()
                }
            })
            .collect();
        let mut out = format!(
            "thread: {thread}\n\
             topic: {topic}\n\
             status: {conv_status:?}\n\
             participant: {actor}\n\
             current turn: {current}\n\
             your turn: {yours}\n\
             paused on moderator: {paused}\n\
             turn order: {order}\n\
             transcript lines: {total_lines}\n",
            topic = status.topic,
            conv_status = status.status,
            actor = actor,
            current = status.current,
            yours = if &status.current == actor {
                "yes"
            } else {
                "no"
            },
            paused = if status.paused { "yes" } else { "no" },
            order = order.join(" → "),
        );
        if let Some(remaining) = status.quieted.get(actor) {
            out.push_str(&format!(
                "you are quieted for your next {remaining} turn(s)\n"
            ));
        }
        out.push_str(Self::next_moves(
            &status.current == actor,
            status.status == substrate_core::ThreadStatus::Ended,
        ));
        Ok(out)
    }

    /// Moderator gate: floor-ops are allowed only when the resolved actor is
    /// the thread's moderator. Returns the rejection to send back when it
    /// isn't — a domain error the model should read and act on.
    fn require_moderator(
        &self,
        space: &Space,
        thread: &Name,
        actor: &Name,
    ) -> Result<(), CallToolResult> {
        match turn::turn_status(space, thread) {
            Ok(status) if &status.moderator == actor => Ok(()),
            Ok(status) => Err(CallToolResult::error(vec![Content::text(format!(
                "only the moderator may do that — {moderator} moderates '{thread}', not you \
                 ({me}). You can still take part on your turn with write_entry.",
                moderator = status.moderator,
                me = actor,
            ))])),
            Err(e) => Err(Self::domain_error(e)),
        }
    }

    /// A moderator op succeeded: confirm what changed, then show the fresh
    /// floor state so the moderator sees the room as it now stands.
    fn moderator_ok(
        &self,
        space: &Space,
        thread: &Name,
        actor: &Name,
        confirmation: &str,
    ) -> CallToolResult {
        let status = self
            .status_text(space, thread, actor)
            .unwrap_or_else(|e| e.to_string());
        CallToolResult::success(vec![Content::text(format!("{confirmation}\n\n{status}"))])
    }
}

#[derive(Deserialize, schemars::JsonSchema)]
pub struct ListParams {
    /// Registered participant to evaluate "you"/"your turn" against. Defaults
    /// to --name when the server was launched with one.
    #[serde(default, alias = "participant-name", alias = "participant")]
    pub participant_name: Option<String>,
}

#[derive(Deserialize, schemars::JsonSchema)]
pub struct ThreadParams {
    /// Space label (required only when several spaces are configured).
    #[serde(default)]
    pub space: Option<String>,
    /// Thread name, as shown by list_threads.
    pub thread: String,
    /// Registered participant to evaluate "you"/"your turn" against. Defaults
    /// to --name when the server was launched with one.
    #[serde(default, alias = "participant-name", alias = "participant")]
    pub participant_name: Option<String>,
}

#[derive(Deserialize, schemars::JsonSchema)]
pub struct ReadParams {
    /// Space label (required only when several spaces are configured).
    #[serde(default)]
    pub space: Option<String>,
    /// Thread name, as shown by list_threads.
    pub thread: String,
    /// Return only the last N transcript lines.
    #[serde(default)]
    pub last_n: Option<usize>,
    /// Return transcript lines from this 1-based line number to the end.
    /// Pass your previous total + 1 to read only what's new.
    #[serde(default)]
    pub from_line: Option<usize>,
}

#[derive(Deserialize, schemars::JsonSchema)]
pub struct WriteParams {
    /// Space label (required only when several spaces are configured).
    #[serde(default)]
    pub space: Option<String>,
    /// Thread name, as shown by list_threads.
    pub thread: String,
    /// Your entry, in markdown, addressed to the whole thread. Reply exactly
    /// "pass" (or "no-op" or "...") to take a no-op turn.
    pub content: String,
    /// Registered participant to write as. Defaults to --name when the server
    /// was launched with one.
    #[serde(default, alias = "participant-name", alias = "participant")]
    pub participant_name: Option<String>,
}

#[derive(Deserialize, schemars::JsonSchema)]
pub struct WaitParams {
    /// Space label (required only when several spaces are configured).
    #[serde(default)]
    pub space: Option<String>,
    /// Thread name, as shown by list_threads.
    pub thread: String,
    /// How long to wait before reporting back (default 120s, max 600s).
    /// A timeout is NOT the end — call this again until status is Ended.
    #[serde(default)]
    pub timeout_secs: Option<u64>,
    /// Registered participant to wait as. Defaults to --name when the server
    /// was launched with one.
    #[serde(default, alias = "participant-name", alias = "participant")]
    pub participant_name: Option<String>,
}

#[derive(Deserialize, schemars::JsonSchema)]
pub struct NewThreadParams {
    /// Space label (required only when several spaces are configured).
    #[serde(default)]
    pub space: Option<String>,
    /// Registered participant creating the thread. Defaults to --name when the
    /// server was launched with one.
    #[serde(default, alias = "participant-name", alias = "participant")]
    pub participant_name: Option<String>,
    /// Thread name to create.
    #[serde(alias = "thread")]
    pub name: String,
    /// Topic line for the new thread.
    pub topic: String,
    /// Registered participant who moderates the thread.
    pub moderator: String,
    /// Speaking order for non-moderator participants. If the moderator is listed,
    /// substrate-core removes the duplicate and still opens on the moderator.
    #[serde(alias = "participants", alias = "turns", alias = "order")]
    pub turn_order: Vec<String>,
}

#[derive(Deserialize, schemars::JsonSchema)]
pub struct ModNameParams {
    /// Space label (required only when several spaces are configured).
    #[serde(default)]
    pub space: Option<String>,
    /// Thread name, as shown by list_threads.
    pub thread: String,
    /// Registered participant acting as moderator. Defaults to --name when the
    /// server was launched with one.
    #[serde(default, alias = "participant-name", alias = "participant")]
    pub participant_name: Option<String>,
    /// The participant to act on.
    pub name: String,
}

#[derive(Deserialize, schemars::JsonSchema)]
pub struct ModQuietParams {
    /// Space label (required only when several spaces are configured).
    #[serde(default)]
    pub space: Option<String>,
    /// Thread name, as shown by list_threads.
    pub thread: String,
    /// Registered participant acting as moderator. Defaults to --name when the
    /// server was launched with one.
    #[serde(default, alias = "participant-name", alias = "participant")]
    pub participant_name: Option<String>,
    /// The participant to quiet.
    pub name: String,
    /// Number of their upcoming turns to skip. 0 lifts an existing quiet.
    pub turns: u32,
}

#[derive(Deserialize, schemars::JsonSchema)]
pub struct ModOrderParams {
    /// Space label (required only when several spaces are configured).
    #[serde(default)]
    pub space: Option<String>,
    /// Thread name, as shown by list_threads.
    pub thread: String,
    /// Registered participant acting as moderator. Defaults to --name when the
    /// server was launched with one.
    #[serde(default, alias = "participant-name", alias = "participant")]
    pub participant_name: Option<String>,
    /// The new speaking order. You (the moderator) are always placed first, so
    /// you need not list yourself; anyone omitted leaves the room.
    pub order: Vec<String>,
}

#[derive(Deserialize, schemars::JsonSchema)]
pub struct ModTopicParams {
    /// Space label (required only when several spaces are configured).
    #[serde(default)]
    pub space: Option<String>,
    /// Thread name, as shown by list_threads.
    pub thread: String,
    /// Registered participant acting as moderator. Defaults to --name when the
    /// server was launched with one.
    #[serde(default, alias = "participant-name", alias = "participant")]
    pub participant_name: Option<String>,
    /// The new topic line.
    pub topic: String,
}

#[tool_router]
impl SubstrateServer {
    #[tool(
        description = "Start here if you're new: what substrate is, how threads work, and the exact loop to follow as a participant. Costs nothing, changes nothing."
    )]
    fn about(&self) -> Result<CallToolResult, ErrorData> {
        let spaces = match self.load() {
            Ok(set) if !set.is_empty() => set.labels().join(", "),
            _ => "(none yet — a moderator creates one with `substrate init`)".to_string(),
        };
        let tools = TOOL_NAMES.join(", ");
        Ok(CallToolResult::success(vec![Content::text(format!(
            "# substrate — a shared chalkboard\n\
             \n\
             server version: {version}\n\
             advertised tools: {tools}\n\
             \n\
             Local-first, turn-based group conversations between humans, agents, and \
             anything else. Everyone in a thread is a peer; every entry is markdown, \
             append-only, and addressed to the whole thread. No edits, no deletes, no \
             private messages. Default participant: {me}. Spaces you can see: {spaces}.\n\
             \n\
             ## The loop (this is the whole protocol)\n\
             1. list_threads — find threads and see whose turn it is.\n\
             2. wait_for_turn — blocks until the floor is yours (wakes instantly on \
             changes). A TIMEOUT MEANS STILL WAITING: call it again. You are only \
             done with a thread when its status is Ended.\n\
             3. read_thread — catch up. Line numbers are stable; pass \
             from_line = your previous 'transcript lines' total + 1 to read only \
             what's new.\n\
             4. write_entry — your reply, to the whole thread. Nothing to add? Send \
             exactly 'pass' (or 'no-op' or '...'): it yields the floor and stays \
             hidden from everyone's view of the thread.\n\
             5. Back to 2.\n\
             \n\
             ## Good to know\n\
             - Turns are enforced: writing out of turn is rejected (the error names \
             who holds the floor). Identity-bearing tools accept participant_name; \
             when omitted they use this server's default participant. If there is no \
             default, pass participant_name on each call.\n\
             - A trusted local harness may drive multiple personas from one MCP \
             server by setting participant_name per call. Turn enforcement is still \
             the guard: only that participant can act on that participant's turn.\n\
             - The moderator speaks first: a new thread opens paused on them, and \
             their opening entry carries the instructions/context for the thread — \
             read it carefully before your first turn.\n\
             - Whenever the moderator holds the floor the thread is paused while they \
             adjust things (topic, order, quieting). Wait it out.\n\
             - You may be quieted for a turn or two; check_turn will tell you.\n\
             - With several spaces configured, pass `space` (the label) alongside \
             `thread` to every tool.\n\
             - Every status response ends with a '→ your move / not your turn' line \
             — trust it over memory.\n\
             \n\
             ## If you moderate a thread\n\
             You also hold floor-control tools, usable while anyone has the floor \
             (they never cost a turn): set_next (hand the floor to a named \
             participant), invite (add someone — an unknown name is registered as a \
             new agent), quiet (turns = 0 lifts it), reorder_turns, set_topic, \
             end_thread, and resume_thread. Non-moderators are refused, with the \
             moderator named; only the thread's moderator may use them.",
            me = self.default_actor_text(),
            version = env!("CARGO_PKG_VERSION"),
            tools = tools,
        ))]))
    }

    #[tool(
        description = "Create a new thread in a space. Any registered participant of the space may create one; the named moderator and turn_order members must already be registered. The opening floor is returned."
    )]
    fn new_thread(
        &self,
        Parameters(p): Parameters<NewThreadParams>,
    ) -> Result<CallToolResult, ErrorData> {
        let actor = match self.actor(p.participant_name.as_deref()) {
            Ok(actor) => actor,
            Err(reject) => return Ok(reject),
        };
        let set = self.load()?;
        let space = Self::resolve(&set, p.space.as_deref())?;
        // Thread creation is space-level: any registered participant can open a
        // room. Unregistered MCP identities cannot write space config, while
        // substrate-core still owns the config schema, participant validation,
        // and moderator-first opening floor invariant.
        if let Err(e) = space.participant(actor.as_str()) {
            return Ok(match e {
                SubstrateError::UnknownParticipant(_) => {
                    CallToolResult::error(vec![Content::text(format!(
                        "only registered participants may create threads in this space — \
                         the resolved participant is '{me}', which is not registered here. Ask a \
                         moderator to add you first.",
                        me = actor
                    ))])
                }
                e => Self::domain_error(e),
            });
        }

        let thread_name = parse_name(&p.name)?;
        let moderator = parse_name(&p.moderator)?;
        let turn_order = p
            .turn_order
            .iter()
            .map(|name| Name::new(name.trim()))
            .collect::<substrate_core::Result<Vec<_>>>()
            .map_err(|e| ErrorData::invalid_params(e.to_string(), None))?;

        match thread::create_thread(space, &thread_name, &p.topic, &moderator, &turn_order) {
            Ok(config) => {
                let order: Vec<String> = config
                    .turn_order
                    .iter()
                    .map(|name| {
                        if name == &config.moderator {
                            format!("{name} (moderator)")
                        } else {
                            name.to_string()
                        }
                    })
                    .collect();
                Ok(CallToolResult::success(vec![Content::text(format!(
                    "created thread: {thread}\n\
                     topic: {topic}\n\
                     opening floor: {current}\n\
                     paused on moderator: {paused}\n\
                     turn order: {order}",
                    thread = thread_name,
                    topic = config.topic,
                    current = config.current(),
                    paused = if config.is_paused() { "yes" } else { "no" },
                    order = order.join(" → "),
                ))]))
            }
            Err(e) => Ok(Self::domain_error(e)),
        }
    }

    #[tool(
        description = "List every thread you can see, across all configured spaces: space, topic, status, whose turn it is, and whether it's yours. Pass `space` and `thread` as separate arguments to the other tools."
    )]
    fn list_threads(
        &self,
        Parameters(p): Parameters<ListParams>,
    ) -> Result<CallToolResult, ErrorData> {
        let actor = match self.actor(p.participant_name.as_deref()) {
            Ok(actor) => actor,
            Err(reject) => return Ok(reject),
        };
        let set = self.load()?;
        let mut out = String::new();
        for labeled in set.iter() {
            let threads = labeled
                .space
                .list_threads()
                .map_err(|e| ErrorData::internal_error(e.to_string(), None))?;
            for conv in threads {
                // every field labeled: a first-contact agent must never have
                // to guess which token is the thread slug (field report: one
                // tried the topic as the name)
                let space_part = if set.len() > 1 {
                    format!(" · space: {}", labeled.label)
                } else {
                    String::new()
                };
                match turn::turn_status(&labeled.space, &conv) {
                    Ok(s) => {
                        out.push_str(&format!(
                        "thread: {conv}{space_part} · status: {status:?} · turn: {current}{yours}{paused} · topic: {topic}\n",
                        status = s.status,
                        current = s.current,
                        yours = if s.current == actor { " (you)" } else { "" },
                        paused = if s.paused { " (paused on moderator)" } else { "" },
                        topic = s.topic,
                    ))
                    }
                    Err(e) => out.push_str(&format!("thread: {conv}{space_part} · unreadable: {e}\n")),
                }
            }
        }
        if out.is_empty() {
            out = if set.is_empty() {
                "no spaces exist yet — a moderator creates one with `substrate init`".to_string()
            } else {
                format!(
                    "no threads yet in configured space(s): {}",
                    set.labels().join(", ")
                )
            };
        }
        Ok(CallToolResult::success(vec![Content::text(out)]))
    }

    #[tool(
        description = "Read a thread's shared transcript (no-op turns omitted). Defaults to all of it; use last_n for a tail or from_line (1-based) to read only what's new since your last read. Line numbers are stable: entries are append-only."
    )]
    fn read_thread(
        &self,
        Parameters(p): Parameters<ReadParams>,
    ) -> Result<CallToolResult, ErrorData> {
        if p.last_n.is_some() && p.from_line.is_some() {
            return Err(ErrorData::invalid_params(
                "last_n and from_line are mutually exclusive",
                None,
            ));
        }
        let set = self.load()?;
        let space = Self::resolve(&set, p.space.as_deref())?;
        let thread = parse_name(&p.thread)?;
        let rendered = match Self::render(space, &thread) {
            Ok(r) => r,
            Err(e) => return Ok(Self::domain_error(e)),
        };
        let window = match (p.last_n, p.from_line) {
            (Some(n), None) => Window::LastN(n),
            (None, Some(n)) => Window::FromLine(n),
            _ => Window::All,
        };
        let (text, total) = transcript::window(&rendered, window);
        let footer = format!("\n--- transcript lines: {total} ---");
        Ok(CallToolResult::success(vec![Content::text(format!(
            "{text}{footer}"
        ))]))
    }

    #[tool(
        description = "Write your entry to the thread. Only allowed on your turn — check_turn or wait_for_turn first. Reply exactly 'pass' (or 'no-op' or '...') to take a no-op turn when you have nothing to add."
    )]
    fn write_entry(
        &self,
        Parameters(p): Parameters<WriteParams>,
    ) -> Result<CallToolResult, ErrorData> {
        let actor = match self.actor(p.participant_name.as_deref()) {
            Ok(actor) => actor,
            Err(reject) => return Ok(reject),
        };
        let set = self.load()?;
        let space = Self::resolve(&set, p.space.as_deref())?;
        let thread = parse_name(&p.thread)?;
        match turn::write_entry(space, &thread, &actor, &p.content) {
            Ok(written) => Ok(CallToolResult::success(vec![Content::text(format!(
                "recorded{no_op} — next turn: {next}{paused}\n{moves}",
                no_op = if written.no_op { " as a no-op" } else { "" },
                next = written.next,
                paused = if written.paused {
                    " (moderator — the thread is paused)"
                } else {
                    ""
                },
                moves = Self::next_moves(false, false),
            ))])),
            Err(e) => Ok(Self::domain_error(e)),
        }
    }

    #[tool(
        description = "Check whose turn it is in a thread, whether the thread is paused on the moderator, and the current transcript line count."
    )]
    fn check_turn(
        &self,
        Parameters(p): Parameters<ThreadParams>,
    ) -> Result<CallToolResult, ErrorData> {
        let actor = match self.actor(p.participant_name.as_deref()) {
            Ok(actor) => actor,
            Err(reject) => return Ok(reject),
        };
        let set = self.load()?;
        let space = Self::resolve(&set, p.space.as_deref())?;
        let thread = parse_name(&p.thread)?;
        match self.status_text(space, &thread, &actor) {
            Ok(text) => Ok(CallToolResult::success(vec![Content::text(text)])),
            Err(e) => Ok(Self::domain_error(e)),
        }
    }

    #[tool(
        description = "Wait until it's your turn in a thread. Wakes immediately when the floor changes (file-watch), returns at the timeout otherwise. A timeout means 'still waiting' — call this again; you are only done with a thread when its status is Ended."
    )]
    async fn wait_for_turn(
        &self,
        Parameters(p): Parameters<WaitParams>,
    ) -> Result<CallToolResult, ErrorData> {
        let actor = match self.actor(p.participant_name.as_deref()) {
            Ok(actor) => actor,
            Err(reject) => return Ok(reject),
        };
        let set = self.load()?;
        let space = Self::resolve(&set, p.space.as_deref())?.clone();
        drop(set);
        let thread = parse_name(&p.thread)?;
        let timeout = Duration::from_secs(
            p.timeout_secs
                .unwrap_or(WAIT_DEFAULT_SECS)
                .min(WAIT_MAX_SECS),
        );
        let deadline = tokio::time::Instant::now() + timeout;

        // Wake on any change in the thread directory; fall back to a
        // slow re-check in case an event is missed.
        let (tx, mut rx) = tokio::sync::mpsc::channel::<()>(4);
        let mut watcher =
            notify::recommended_watcher(move |result: notify::Result<notify::Event>| {
                if result.is_ok() {
                    let _ = tx.try_send(());
                }
            })
            .map_err(|e| ErrorData::internal_error(e.to_string(), None))?;
        watcher
            .watch(
                &space.thread_dir(&thread),
                notify::RecursiveMode::NonRecursive,
            )
            .map_err(|e| ErrorData::internal_error(e.to_string(), None))?;

        loop {
            let status = match turn::turn_status(&space, &thread) {
                Ok(s) => s,
                Err(e) => return Ok(Self::domain_error(e)),
            };
            let done =
                status.current == actor || status.status == substrate_core::ThreadStatus::Ended;
            let now = tokio::time::Instant::now();
            if done || now >= deadline {
                let mut text = self
                    .status_text(&space, &thread, &actor)
                    .unwrap_or_else(|e| e.to_string());
                if !done {
                    text.push_str(
                        "(timed out — still not your turn; call wait_for_turn again. \
                         You are only done with this thread when status is Ended.)\n",
                    );
                }
                return Ok(CallToolResult::success(vec![Content::text(text)]));
            }
            let wait = (deadline - now).min(WAIT_FALLBACK_INTERVAL);
            tokio::select! {
                _ = rx.recv() => {}
                _ = tokio::time::sleep(wait) => {}
            }
        }
    }

    // ---- Moderator floor-ops (role-gated) ------------------------------
    //
    // The same operations the TUI moderator has, exposed so moderation can
    // pass to an agent. Each is gated to the thread's moderator; a non-
    // moderator is refused with the role named. They never consume a turn.

    #[tool(
        description = "Moderator only. Hand the floor directly to a named participant — the conductor's baton. Works at any time, even mid-round; if that participant was quieted, the quiet is cleared. This is how an agent-moderator runs turn-taking."
    )]
    fn set_next(
        &self,
        Parameters(p): Parameters<ModNameParams>,
    ) -> Result<CallToolResult, ErrorData> {
        let actor = match self.actor(p.participant_name.as_deref()) {
            Ok(actor) => actor,
            Err(reject) => return Ok(reject),
        };
        let set = self.load()?;
        let space = Self::resolve(&set, p.space.as_deref())?;
        let thread = parse_name(&p.thread)?;
        let name = parse_name(&p.name)?;
        if let Err(reject) = self.require_moderator(space, &thread, &actor) {
            return Ok(reject);
        }
        match turn::set_next(space, &thread, &name) {
            Ok(()) => Ok(self.moderator_ok(
                space,
                &thread,
                &actor,
                &format!("the floor passes to {name}"),
            )),
            Err(e) => Ok(Self::domain_error(e)),
        }
    }

    #[tool(
        description = "Moderator only. Add a participant to the room, appended last in the speaking order (the current floor stays put). An unfamiliar name is registered as a new agent — naming someone is the invitation, the same policy as opening a thread."
    )]
    fn invite(
        &self,
        Parameters(p): Parameters<ModNameParams>,
    ) -> Result<CallToolResult, ErrorData> {
        let actor = match self.actor(p.participant_name.as_deref()) {
            Ok(actor) => actor,
            Err(reject) => return Ok(reject),
        };
        let set = self.load()?;
        let space = Self::resolve(&set, p.space.as_deref())?;
        let thread = parse_name(&p.thread)?;
        let name = parse_name(&p.name)?;
        if let Err(reject) = self.require_moderator(space, &thread, &actor) {
            return Ok(reject);
        }
        // mirror the TUI: the moderator naming someone IS the registration
        let registered = if space.participant(name.as_str()).is_err() {
            if let Err(e) = space.add_participant(name.clone(), ParticipantKind::Agent) {
                return Ok(Self::domain_error(e));
            }
            " (registered as a new agent)"
        } else {
            ""
        };
        match turn::invite(space, &thread, &name) {
            Ok(()) => Ok(self.moderator_ok(
                space,
                &thread,
                &actor,
                &format!("{name} joins the thread at the end of the round{registered}"),
            )),
            Err(e) => Ok(Self::domain_error(e)),
        }
    }

    #[tool(
        description = "Moderator only. Quiet a participant for their next `turns` turns — they are skipped when reached. Pass turns = 0 to lift an existing quiet. The moderator cannot be quieted."
    )]
    fn quiet(
        &self,
        Parameters(p): Parameters<ModQuietParams>,
    ) -> Result<CallToolResult, ErrorData> {
        let actor = match self.actor(p.participant_name.as_deref()) {
            Ok(actor) => actor,
            Err(reject) => return Ok(reject),
        };
        let set = self.load()?;
        let space = Self::resolve(&set, p.space.as_deref())?;
        let thread = parse_name(&p.thread)?;
        let name = parse_name(&p.name)?;
        if let Err(reject) = self.require_moderator(space, &thread, &actor) {
            return Ok(reject);
        }
        match turn::quiet(space, &thread, &name, p.turns) {
            Ok(()) => {
                let confirmation = if p.turns == 0 {
                    format!("{name} may speak again")
                } else {
                    format!("{name} quieted for {} turn(s)", p.turns)
                };
                Ok(self.moderator_ok(space, &thread, &actor, &confirmation))
            }
            Err(e) => Ok(Self::domain_error(e)),
        }
    }

    #[tool(
        description = "Moderator only. Replace the speaking order with the given participants. You are always placed first; anyone omitted leaves the room (losing any quiet). If the current speaker remains, the floor stays with them, otherwise it returns to you."
    )]
    fn reorder_turns(
        &self,
        Parameters(p): Parameters<ModOrderParams>,
    ) -> Result<CallToolResult, ErrorData> {
        let actor = match self.actor(p.participant_name.as_deref()) {
            Ok(actor) => actor,
            Err(reject) => return Ok(reject),
        };
        let set = self.load()?;
        let space = Self::resolve(&set, p.space.as_deref())?;
        let thread = parse_name(&p.thread)?;
        if let Err(reject) = self.require_moderator(space, &thread, &actor) {
            return Ok(reject);
        }
        let order: Vec<Name> = match p
            .order
            .iter()
            .map(|n| Name::new(n))
            .collect::<substrate_core::Result<Vec<Name>>>()
        {
            Ok(v) => v,
            Err(e) => return Ok(Self::domain_error(e)),
        };
        match turn::reorder_turns(space, &thread, &order) {
            Ok(()) => Ok(self.moderator_ok(
                space,
                &thread,
                &actor,
                &format!("turn order set: {}", p.order.join(" → ")),
            )),
            Err(e) => Ok(Self::domain_error(e)),
        }
    }

    #[tool(description = "Moderator only. Change the thread's topic line.")]
    fn set_topic(
        &self,
        Parameters(p): Parameters<ModTopicParams>,
    ) -> Result<CallToolResult, ErrorData> {
        let actor = match self.actor(p.participant_name.as_deref()) {
            Ok(actor) => actor,
            Err(reject) => return Ok(reject),
        };
        let set = self.load()?;
        let space = Self::resolve(&set, p.space.as_deref())?;
        let thread = parse_name(&p.thread)?;
        if let Err(reject) = self.require_moderator(space, &thread, &actor) {
            return Ok(reject);
        }
        match turn::set_topic(space, &thread, &p.topic) {
            Ok(()) => {
                Ok(self.moderator_ok(space, &thread, &actor, &format!("topic set: {}", p.topic)))
            }
            Err(e) => Ok(Self::domain_error(e)),
        }
    }

    #[tool(
        description = "Moderator only. End the thread: every entry stays readable forever, but all further writes are rejected. Reversible with resume_thread."
    )]
    fn end_thread(
        &self,
        Parameters(p): Parameters<ThreadParams>,
    ) -> Result<CallToolResult, ErrorData> {
        let actor = match self.actor(p.participant_name.as_deref()) {
            Ok(actor) => actor,
            Err(reject) => return Ok(reject),
        };
        let set = self.load()?;
        let space = Self::resolve(&set, p.space.as_deref())?;
        let thread = parse_name(&p.thread)?;
        if let Err(reject) = self.require_moderator(space, &thread, &actor) {
            return Ok(reject);
        }
        match turn::end_thread(space, &thread) {
            Ok(()) => Ok(self.moderator_ok(space, &thread, &actor, "thread ended")),
            Err(e) => Ok(Self::domain_error(e)),
        }
    }

    #[tool(
        description = "Moderator only. Reopen an ended thread; the floor returns to you so your next entry can say why the room is back."
    )]
    fn resume_thread(
        &self,
        Parameters(p): Parameters<ThreadParams>,
    ) -> Result<CallToolResult, ErrorData> {
        let actor = match self.actor(p.participant_name.as_deref()) {
            Ok(actor) => actor,
            Err(reject) => return Ok(reject),
        };
        let set = self.load()?;
        let space = Self::resolve(&set, p.space.as_deref())?;
        let thread = parse_name(&p.thread)?;
        if let Err(reject) = self.require_moderator(space, &thread, &actor) {
            return Ok(reject);
        }
        match turn::resume_thread(space, &thread) {
            Ok(()) => Ok(self.moderator_ok(
                space,
                &thread,
                &actor,
                "thread resumed — the floor is yours; say why the thread is back",
            )),
            Err(e) => Ok(Self::domain_error(e)),
        }
    }
}

fn parse_name(s: &str) -> Result<Name, ErrorData> {
    Name::new(s).map_err(|e| ErrorData::invalid_params(e.to_string(), None))
}

#[tool_handler]
impl ServerHandler for SubstrateServer {
    fn get_info(&self) -> ServerInfo {
        let spaces = self.source.describe();
        ServerInfo::new(ServerCapabilities::builder().enable_tools().build())
            .with_server_info(Implementation::from_build_env())
            .with_instructions(format!(
                "Your default substrate participant is '{me}': local-first, \
                 turn-based group conversations between humans, agents, and \
                 anything else — a shared chalkboard. Identity-bearing tools \
                 accept participant_name; when omitted they use this default. \
                 New here? Call the `about` tool for the full \
                 protocol. Spaces ({spaces}) are re-read on every call, so \
                 list_threads always shows the current threads. Ground rules:\n\
                 - Rooms are turn-based. Call wait_for_turn (or check_turn) before \
                 writing; write_entry only works when you hold the floor.\n\
                 - A wait_for_turn timeout means 'still waiting' — call it again. \
                 You are only done with a thread when its status is Ended.\n\
                 - Entries are markdown, append-only, and addressed to the whole \
                 thread. No edits, no deletes, no private messages.\n\
                 - If you have nothing to add on your turn, write exactly 'pass' \
                 (or 'no-op' or '...'): it yields the floor without clutter.\n\
                 - When the moderator holds the floor the thread is paused — wait it out.\n\
                 - A trusted local harness may drive multiple personas from one \
                 server by passing participant_name per call; turn enforcement \
                 remains the guard.\n\
                 - read_thread returns the shared transcript with no-op turns \
                 omitted; pass from_line = your previous line total + 1 to read only \
                 what's new.",
                me = self.default_actor_text()
            ))
    }
}
