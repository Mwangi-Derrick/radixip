//! RadixIP XDP loader (Aya).
//!
//! Order is fixed by the operational contract (guide section 7):
//!   load (creates/reuses pinned maps) -> seed -> attach.
//! Every failure is fail-open: log, then park. XDP never blocks startup.
//!
//! Assumptions to adapt to what RadixIP already publishes:
//!   * channel  `security:blocklist` carries "add <cidr>", "del <cidr>" or a bare "<cidr>" (= add)
//!   * snapshot is a Redis SET at `security:blocklist:set` containing CIDRs / IPs
//!
//! Metrics: this scaffold only logs the stats-map counters every 10s. Wire
//! `Stats` into your existing Prometheus registry (section 7.5).

use std::{
    collections::HashSet,
    net::{IpAddr, Ipv4Addr},
    path::PathBuf,
    time::{Duration, Instant},
};

use anyhow::{anyhow, bail, Context, Result};
use aya::{
    include_bytes_aligned,
    maps::{
        lpm_trie::{Key, LpmTrie},
        MapData, PerCpuArray,
    },
    programs::{Xdp, XdpFlags},
    EbpfLoader,
};
use futures_util::StreamExt;
use ipnet::{IpNet, Ipv4Net};
use log::{error, info, warn};

// Built by `make kernel` first.
static BPF_OBJ: &[u8] =
    include_bytes_aligned!("../../xdp-kernel/target/bpfel-unknown-none/release/radixip-xdp");

struct Config {
    enabled: bool,
    iface: String,
    drv_mode: bool,
    pin_dir: PathBuf,
    redis_url: String,
    channel: String,
    snapshot_key: String,
    fail_closed: bool,
    min_v4: u8,
    min_v6: u8,
    never_block: Vec<IpNet>,
}

fn env_or(k: &str, d: &str) -> String {
    std::env::var(k).unwrap_or_else(|_| d.to_string())
}

impl Config {
    fn from_env() -> Self {
        let never_block = env_or("RADIXIP_XDP_NEVER_BLOCK", "")
            .split(',')
            .filter_map(|s| s.trim().parse::<IpNet>().ok())
            .collect();
        Self {
            enabled: env_or("RADIXIP_XDP_ENABLED", "false") == "true",
            iface: env_or("RADIXIP_XDP_IFACE", "eth0"),
            drv_mode: env_or("RADIXIP_XDP_MODE", "skb") == "drv",
            // Versioned: bump when key/value/max_entries change.
            pin_dir: env_or("RADIXIP_XDP_PIN_DIR", "/sys/fs/bpf/radixip/v1").into(),
            redis_url: env_or("RADIXIP_REDIS_URL", "redis://127.0.0.1:6379"),
            channel: env_or("RADIXIP_XDP_CHANNEL", "security:blocklist"),
            snapshot_key: env_or("RADIXIP_XDP_SNAPSHOT_KEY", "security:blocklist:set"),
            fail_closed: env_or("RADIXIP_XDP_FAIL_CLOSED", "false") == "true",
            min_v4: env_or("RADIXIP_XDP_MIN_V4", "8").parse().unwrap_or(8),
            min_v6: env_or("RADIXIP_XDP_MIN_V6", "16").parse().unwrap_or(16),
            never_block,
        }
    }
}

/// Normalize + self-lockout guard (section 7.7).
fn normalize(cfg: &Config, net: IpNet) -> Result<IpNet> {
    let net = net.trunc();
    // IPv4-mapped IPv6 (::ffff:a.b.c.d/N, N >= 96) -> IPv4 /N-96.
    let net = match net {
        IpNet::V6(n) if n.prefix_len() >= 96 => match n.network().to_ipv4_mapped() {
            Some(v4) => IpNet::V4(Ipv4Net::new(v4, n.prefix_len() - 96)?),
            None => IpNet::V6(n),
        },
        other => other,
    };
    let min = if net.addr().is_ipv4() { cfg.min_v4 } else { cfg.min_v6 };
    if net.prefix_len() < min {
        bail!("{net}: prefix shorter than minimum /{min}");
    }
    for nb in &cfg.never_block {
        let overlap = match (net, *nb) {
            (IpNet::V4(a), IpNet::V4(b)) => a.contains(&b) || b.contains(&a),
            (IpNet::V6(a), IpNet::V6(b)) => a.contains(&b) || b.contains(&a),
            _ => false,
        };
        if overlap {
            bail!("{net}: overlaps never-block entry {nb}");
        }
    }
    Ok(net)
}

fn parse_net(s: &str) -> Result<IpNet> {
    let s = s.trim();
    if s.contains('/') {
        return Ok(s.parse::<IpNet>()?);
    }
    Ok(IpNet::from(s.parse::<IpAddr>()?))
}

struct Maps {
    v4: LpmTrie<MapData, [u8; 4], u8>,
    v6: LpmTrie<MapData, [u8; 16], u8>,
    stats: PerCpuArray<MapData, u64>,
    // Loader-side entry tracking; the kernel has no cheap LPM count.
    set: HashSet<IpNet>,
}

impl Maps {
    fn put(&mut self, net: IpNet) -> Result<()> {
        match net {
            IpNet::V4(n) => {
                let key = Key::new(n.prefix_len() as u32, n.network().octets());
                self.v4.insert(&key, 1u8, 0)?;
            }
            IpNet::V6(n) => {
                let key = Key::new(n.prefix_len() as u32, n.network().octets());
                self.v6.insert(&key, 1u8, 0)?;
            }
        }
        self.set.insert(net);
        Ok(())
    }

    fn del(&mut self, net: IpNet) -> Result<()> {
        // Removing a key that is not present is not an error for us.
        let _ = match net {
            IpNet::V4(n) => self
                .v4
                .remove(&Key::new(n.prefix_len() as u32, n.network().octets())),
            IpNet::V6(n) => self
                .v6
                .remove(&Key::new(n.prefix_len() as u32, n.network().octets())),
        };
        self.set.remove(&net);
        Ok(())
    }

    fn stat(&self, idx: u32) -> u64 {
        self.stats
            .get(&idx, 0)
            .map(|v| v.iter().sum::<u64>())
            .unwrap_or(0)
    }
}

async fn apply_event(cfg: &Config, maps: &mut Maps, payload: &str) -> Result<()> {
    let mut it = payload.split_whitespace();
    let first = it.next().ok_or_else(|| anyhow!("empty message"))?;
    let (op, cidr) = match first {
        "add" | "del" => (first, it.next().ok_or_else(|| anyhow!("missing cidr"))?),
        _ => ("add", first),
    };
    let net = normalize(cfg, parse_net(cidr)?)?;
    if op == "add" {
        maps.put(net)
    } else {
        maps.del(net)
    }
}

async fn run(cfg: &Config) -> Result<()> {
    let started = Instant::now();

    // 1. LOAD. map_pin_path + `pinned` maps => reuse across restarts.
    std::fs::create_dir_all(&cfg.pin_dir)
        .with_context(|| format!("create pin dir {:?} (is bpffs mounted?)", cfg.pin_dir))?;
    let mut ebpf = EbpfLoader::new()
        .map_pin_path(&cfg.pin_dir)
        .load(BPF_OBJ)
        .context("load eBPF object (kernel too old, missing CAP_BPF, or pinned map mismatch)")?;

    let mut maps = Maps {
        v4: LpmTrie::try_from(ebpf.take_map("blocklist_v4").context("no blocklist_v4")?)?,
        v6: LpmTrie::try_from(ebpf.take_map("blocklist_v6").context("no blocklist_v6")?)?,
        stats: PerCpuArray::try_from(ebpf.take_map("stats").context("no stats")?)?,
        set: HashSet::new(),
    };

    // 2. SEED (subscribe first so nothing published during seed is lost).
    let client = redis::Client::open(cfg.redis_url.as_str())?;
    let mut seeded = false;
    let mut pubsub = None;
    match client.get_async_pubsub().await {
        Ok(mut ps) => {
            if ps.subscribe(&cfg.channel).await.is_ok() {
                pubsub = Some(ps);
            }
        }
        Err(e) => warn!("redis pubsub unavailable: {e}"),
    }
    match client.get_multiplexed_async_connection().await {
        Ok(mut con) => {
            match redis::cmd("SMEMBERS")
                .arg(&cfg.snapshot_key)
                .query_async::<Vec<String>>(&mut con)
                .await
            {
                Ok(items) => {
                    let (mut ok, mut bad) = (0usize, 0usize);
                    for s in items {
                        match parse_net(&s).and_then(|n| normalize(cfg, n)).and_then(|n| maps.put(n)) {
                            Ok(()) => ok += 1,
                            Err(e) => {
                                bad += 1;
                                warn!("seed skip {s}: {e}");
                            }
                        }
                    }
                    info!("seeded {ok} prefixes ({bad} rejected)");
                    seeded = true;
                }
                Err(e) => warn!("snapshot read failed: {e}"),
            }
        }
        Err(e) => warn!("redis unavailable for seed: {e}"),
    }
    if !seeded && cfg.fail_closed {
        bail!("fail_closed set and map not seeded; refusing to attach");
    }
    if !seeded {
        warn!("attaching with unseeded map (fail-open): userspace is the only filter until sync");
    }
    info!("seed phase took {:?}", started.elapsed());

    // 3. ATTACH.
    let prog: &mut Xdp = ebpf
        .program_mut("radixip_xdp")
        .context("program radixip_xdp not found")?
        .try_into()?;
    prog.load().context("verifier rejected program")?;
    let flags = if cfg.drv_mode { XdpFlags::DRV_MODE } else { XdpFlags::SKB_MODE };
    // Not pinning the link: if this process dies, the program detaches.
    let _link = prog
        .attach(&cfg.iface, flags)
        .with_context(|| format!("attach to {} ({})", cfg.iface, if cfg.drv_mode { "drv" } else { "skb" }))?;
    info!("XDP attached on {} ({})", cfg.iface, if cfg.drv_mode { "drv" } else { "skb" });

    // 4. SYNC LOOP.
    let mut tick = tokio::time::interval(Duration::from_secs(10));
    let mut stream = pubsub.as_mut().map(|p| p.on_message());
    loop {
        tokio::select! {
            _ = tick.tick() => {
                info!(
                    "xdp stats: v4 drop={} pass={} | v6 drop={} pass={} | parse_err={} | entries={}",
                    maps.stat(0), maps.stat(1), maps.stat(2), maps.stat(3), maps.stat(4), maps.set.len()
                );
            }
            msg = async {
                match stream.as_mut() {
                    Some(s) => s.next().await,
                    None => std::future::pending().await,
                }
            } => {
                match msg {
                    Some(m) => match m.get_payload::<String>() {
                        Ok(p) => if let Err(e) = apply_event(cfg, &mut maps, &p).await {
                            warn!("ignored message {p:?}: {e}");
                        },
                        Err(e) => warn!("bad payload: {e}"),
                    },
                    None => { warn!("pubsub closed; map keeps enforcing, no further updates"); stream = None; }
                }
            }
            _ = tokio::signal::ctrl_c() => { info!("shutting down"); return Ok(()); }
        }
    }
}

#[tokio::main]
async fn main() {
    env_logger::init();
    let cfg = Config::from_env();
    if !cfg.enabled {
        info!("XDP disabled (set RADIXIP_XDP_ENABLED=true); running userspace-only");
    } else if let Err(e) = run(&cfg).await {
        // Fail-open: log and PARK. Exiting would restart-loop the container.
        error!("XDP unavailable, continuing userspace-only: {e:#}");
    } else {
        return;
    }
    let _ = Ipv4Addr::LOCALHOST; // keep import tidy if you remove IPv4-specific code
    std::future::pending::<()>().await;
}