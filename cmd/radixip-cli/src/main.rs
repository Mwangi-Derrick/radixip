use clap::{Parser, Subcommand};
use radixip_core::tree::CompressedTree;
use radixip_core::{NodeVariant, RadixEngine, StandardEngine};
use std::net::IpAddr;

#[derive(Parser)]
#[command(name = "radixip")]
#[command(about = "High-performance IP subnet matching", long_about = None)]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Insert a subnet with metadata
    Insert { subnet: String, metadata: String },
    /// Match an IP address
    Match { ip: String },
    /// Show engine stats
    Stats,
    /// Clear all subnets
    Clear,
}

fn main() {
    tracing_subscriber::fmt::init();
    let cli = Cli::parse();
    let engine = StandardEngine::new(CompressedTree::new(NodeVariant::NormalRadixNode));

    match cli.command {
        Commands::Insert { subnet, metadata } => {
            // Insert logic
        }
        Commands::Match { ip } => {
            // Match logic
        }
        Commands::Stats => {
            println!("Size: {}", engine.size());
        }
        Commands::Clear => {
            engine.clear();
        }
    }
}
