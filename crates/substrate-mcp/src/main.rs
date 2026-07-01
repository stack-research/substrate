mod server;
mod spaces;

use std::fs;
use std::path::PathBuf;

use anyhow::Context;
use clap::Parser;
use rmcp::transport::stdio;
use rmcp::ServiceExt;
use substrate_core::Name;

/// One local harness's door into substrate. A launch name is the default
/// participant, and identity-bearing tools may override it per call for
/// trusted multi-persona harnesses. One server can serve many spaces.
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

    /// Default participant name. If omitted, identity-bearing tools require a
    /// participant_name argument per call.
    #[arg(long)]
    name: Option<String>,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let args = Args::parse();
    let default_actor = args
        .name
        .as_deref()
        .map(Name::new)
        .transpose()
        .context("invalid participant name")?;

    // Log to a file, never stdout — the stdio transport owns stdout.
    let log_dir = substrate_core::home::substrate_home()
        .map(|home| home.join("logs"))
        .unwrap_or_else(std::env::temp_dir);
    fs::create_dir_all(&log_dir)?;
    let log_name = default_actor
        .as_ref()
        .map(Name::to_path_component)
        .unwrap_or_else(|| "no-default".to_string());
    let log_file = fs::File::create(log_dir.join(format!("mcp-{log_name}.log")))?;
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()),
        )
        .with_writer(std::sync::Mutex::new(log_file))
        .with_ansi(false)
        .init();

    let source = spaces::SpaceSource::new(args.spaces, args.spaces_file);
    tracing::info!(
        source = source.describe(),
        name = default_actor.as_ref().map(Name::as_str).unwrap_or("(none)"),
        "substrate-mcp serving"
    );
    let server = server::SubstrateServer::new(source, default_actor.as_ref().map(Name::as_str))?;

    let service = server.serve(stdio()).await?;
    service.waiting().await?;
    Ok(())
}
