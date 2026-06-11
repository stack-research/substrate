mod server;
mod spaces;

use std::fs;
use std::path::PathBuf;

use anyhow::Context;
use clap::Parser;
use rmcp::transport::stdio;
use rmcp::ServiceExt;

/// One agent's door into substrate. Identity is fixed at launch: tool calls
/// can never write as anyone else. One server can serve many spaces.
#[derive(Parser)]
#[command(name = "substrate-mcp")]
struct Args {
    /// Space directory; repeat for several spaces (labels = directory names).
    /// When omitted, spaces come from the registry file.
    #[arg(long = "space")]
    spaces: Vec<PathBuf>,

    /// Registry file mapping labels to space paths
    /// (default: ~/.substrate/spaces.yaml).
    #[arg(long)]
    spaces_file: Option<PathBuf>,

    /// Who this process is in the room(s) — a registered participant name.
    #[arg(long)]
    name: String,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = Args::parse();

    // Log to a file, never stdout — the stdio transport owns stdout.
    let log_dir = substrate_core::home::substrate_home()
        .map(|home| home.join("logs"))
        .unwrap_or_else(std::env::temp_dir);
    fs::create_dir_all(&log_dir)?;
    let log_file = fs::File::create(log_dir.join(format!("mcp-{}.log", args.name)))?;
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()),
        )
        .with_writer(std::sync::Mutex::new(log_file))
        .with_ansi(false)
        .init();

    let source = spaces::SpaceSource::new(args.spaces, args.spaces_file);
    tracing::info!(source = source.describe(), name = %args.name, "substrate-mcp serving");
    let server =
        server::SubstrateServer::new(source, &args.name).context("invalid participant name")?;

    let service = server.serve(stdio()).await?;
    service.waiting().await?;
    Ok(())
}
