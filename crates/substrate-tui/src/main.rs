mod app;
mod cli;
mod commands;
mod run;
mod serve;
mod ui;
mod watch;

use std::path::PathBuf;

use clap::{Parser, Subcommand};
use substrate_core::ParticipantKind;

#[derive(Parser)]
#[command(
    name = "substrate",
    about = "Turn-based group conversations (threads) between humans, agents, and anything else"
)]
struct Args {
    /// Space directory (a directory of threads).
    #[arg(long, global = true, env = "SUBSTRATE_SPACE", default_value = ".")]
    space: PathBuf,

    #[command(subcommand)]
    command: Option<Command>,
}

#[derive(Subcommand)]
enum Command {
    /// Create a new space (the directory plus .substrate/).
    Init,
    /// Register a participant in the space. Names are unique.
    Add {
        name: String,
        #[arg(long)]
        kind: ParticipantKind,
    },
    /// Create a thread. The moderator always speaks first.
    New {
        name: String,
        #[arg(long)]
        topic: String,
        #[arg(long)]
        moderator: String,
        /// Speaking order, comma-separated (moderator is prepended first).
        #[arg(long, value_delimiter = ',')]
        turns: Vec<String>,
    },
    /// Show the space (or one thread) — whose turn, status, lines.
    Status { thread: Option<String> },
    /// Write one entry as a participant. Turn-enforced like every other
    /// interface; useful for scripts and harnesses without MCP support.
    Write {
        thread: String,
        #[arg(long = "as")]
        author: String,
        /// The entry text — or use --stdin / --file for multi-line content
        /// (e.g. `pbpaste | substrate write t --as kagi --stdin`).
        #[arg(short, long)]
        message: Option<String>,
        /// Read the entry from stdin.
        #[arg(long)]
        stdin: bool,
        /// Read the entry from a file.
        #[arg(long)]
        file: Option<PathBuf>,
    },
    /// Print the courier packet for a thread: standing prompt + status +
    /// clean transcript. Pipe to pbcopy and paste to a web-only assistant.
    Brief {
        thread: String,
        /// Address the packet to this participant ("you are X — reply now").
        #[arg(long = "for")]
        for_name: Option<String>,
    },
    /// Serve this space over HTTP for proxied participants (web-only
    /// assistants with a GET-only fetch tool). Binds 127.0.0.1; expose with
    /// `tailscale funnel <port>`. Each --proxy participant gets a capability
    /// key; identity comes from the key, never from a parameter.
    Serve {
        #[arg(long, default_value_t = 7171)]
        port: u16,
        /// Participant(s) that may write through this server (repeatable).
        #[arg(long = "proxy", required = true)]
        proxies: Vec<String>,
        /// Fixed capability key (single --proxy only; default: random).
        #[arg(long)]
        key: Option<String>,
    },
    /// Read a thread transcript (no-op turns omitted).
    Read {
        thread: String,
        /// Only the last N transcript lines.
        #[arg(long)]
        last: Option<usize>,
        /// From this 1-based transcript line to the end.
        #[arg(long)]
        from: Option<usize>,
    },
    /// Manage the machine-level space registry (~/.substrate/spaces.yaml) —
    /// the set of spaces every home-level agent registration can see.
    Spaces {
        #[command(subcommand)]
        action: SpacesAction,
    },
    /// Be an agent's hands: watch every space where <name> participates and
    /// run its configured command (from ~/.substrate/agents.yaml, or --exec)
    /// each time the floor reaches it. Runs until Ctrl-C.
    Attend {
        name: String,
        /// Override the agent's command for this run (via `sh -c`).
        #[arg(long)]
        exec: Option<String>,
    },
    /// Watch a thread and report floor changes. With --exec, run a
    /// command on each change (e.g. to nudge an agent harness). Exits when
    /// the thread ends.
    Watch {
        thread: String,
        /// Only report when the floor reaches this participant.
        #[arg(long = "for")]
        for_name: Option<String>,
        /// Command to run (via `sh -c`) on each reported change. Receives
        /// SUBSTRATE_SPACE, SUBSTRATE_THREAD, SUBSTRATE_TURN,
        /// SUBSTRATE_STATUS, and SUBSTRATE_TOPIC in the environment.
        #[arg(long)]
        exec: Option<String>,
    },
    /// Launch the TUI (the default when no subcommand is given).
    Tui {
        /// Who you are in the room. Optional if exactly one human is registered.
        #[arg(long)]
        name: Option<String>,
    },
}

#[derive(Subcommand)]
enum SpacesAction {
    /// List registered spaces.
    List,
    /// Register a space (label defaults to the directory name).
    Add {
        path: PathBuf,
        #[arg(long)]
        label: Option<String>,
    },
    /// Remove a space from the registry (the directory is untouched).
    Remove { label: String },
}

fn main() -> anyhow::Result<()> {
    let args = Args::parse();
    match args.command.unwrap_or(Command::Tui { name: None }) {
        Command::Init => cli::init(&args.space),
        Command::Add { name, kind } => cli::add(&args.space, &name, kind),
        Command::New {
            name,
            topic,
            moderator,
            turns,
        } => cli::new_thread(&args.space, &name, &topic, &moderator, &turns),
        Command::Status { thread } => cli::status(&args.space, thread.as_deref()),
        Command::Write {
            thread,
            author,
            message,
            stdin,
            file,
        } => cli::write(
            &args.space,
            &thread,
            &author,
            message.as_deref(),
            stdin,
            file.as_deref(),
        ),
        Command::Brief { thread, for_name } => {
            cli::brief(&args.space, &thread, for_name.as_deref())
        }
        Command::Serve { port, proxies, key } => cli::serve(&args.space, port, &proxies, key),
        Command::Read { thread, last, from } => cli::read(&args.space, &thread, last, from),
        Command::Spaces { action } => match action {
            SpacesAction::List => cli::spaces_list(),
            SpacesAction::Add { path, label } => cli::spaces_add(&path, label.as_deref()),
            SpacesAction::Remove { label } => cli::spaces_remove(&label),
        },
        Command::Attend { name, exec } => cli::attend(&name, exec.as_deref()),
        Command::Watch {
            thread,
            for_name,
            exec,
        } => cli::watch(&args.space, &thread, for_name.as_deref(), exec.as_deref()),
        Command::Tui { name } => run::run(args.space, name),
    }
}
