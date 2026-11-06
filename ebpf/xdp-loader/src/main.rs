use std::{net::IpAddr, path::PathBuf};

use anyhow::{Context, Result};
use ipnet::IpNet;
use log::{error, info};
use xdp_loader::{start, Config, Mode, Runtime};

fn env_or(key: &str, fallback: &str) -> String {
    std::env::var(key).unwrap_or_else(|_| fallback.to_owned())
}

fn config_from_env() -> Result<Option<Config>> {
    if env_or("RADIXIP_XDP_ENABLED", "false") != "true" {
        return Ok(None);
    }

    let never_block = env_or("RADIXIP_XDP_NEVER_BLOCK", "")
        .split(',')
        .filter(|value| !value.trim().is_empty())
        .map(|value| {
            value
                .trim()
                .parse::<IpNet>()
                .context("parse never-block prefix")
        })
        .collect::<Result<Vec<_>>>()?;
    let mode = match env_or("RADIXIP_XDP_MODE", "skb").as_str() {
        "skb" => Mode::Skb,
        "drv" => Mode::Driver,
        other => anyhow::bail!("unsupported RADIXIP_XDP_MODE {other:?}; expected skb or drv"),
    };

    Ok(Some(Config {
        iface: env_or("RADIXIP_XDP_IFACE", "eth0"),
        mode,
        pin_dir: PathBuf::from(env_or("RADIXIP_XDP_PIN_DIR", "/sys/fs/bpf/radixip/v1")),
        redis_url: env_or("RADIXIP_REDIS_URL", "redis://127.0.0.1:6379"),
        channel: env_or("RADIXIP_XDP_CHANNEL", "security:blocklist"),
        snapshot_key: env_or("RADIXIP_XDP_SNAPSHOT_KEY", "security:blocklist:set"),
        fail_closed: env_or("RADIXIP_XDP_FAIL_CLOSED", "false") == "true",
        min_v4: env_or("RADIXIP_XDP_MIN_V4", "8").parse().unwrap_or(8),
        min_v6: env_or("RADIXIP_XDP_MIN_V6", "16").parse().unwrap_or(16),
        never_block,
    }))
}

#[tokio::main]
async fn main() {
    env_logger::init();
    let config = match config_from_env() {
        Ok(Some(config)) => config,
        Ok(None) => {
            info!("XDP disabled; running userspace-only");
            std::future::pending::<()>().await;
            return;
        }
        Err(error) => {
            error!("invalid XDP configuration; running userspace-only: {error:#}");
            std::future::pending::<()>().await;
            return;
        }
    };

    let runtime: Option<Runtime> = match start(config).await {
        Ok(runtime) => Some(runtime),
        Err(error) => {
            error!("XDP unavailable; running userspace-only: {error:#}");
            None
        }
    };

    if let Err(error) = tokio::signal::ctrl_c().await {
        error!("failed to listen for shutdown signal: {error}");
    }
    if let Some(runtime) = runtime {
        if let Err(error) = runtime.shutdown().await {
            error!("XDP shutdown failed: {error:#}");
        }
    }
}
