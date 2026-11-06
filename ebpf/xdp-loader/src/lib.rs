use std::{
    collections::HashSet,
    net::IpAddr,
    path::PathBuf,
    time::{Duration, Instant},
};

use anyhow::{anyhow, bail, Context, Result};
use aya::{
    maps::{
        lpm_trie::{Key, LpmTrie},
        MapData, PerCpuArray,
    },
    programs::{Xdp, XdpFlags},
    EbpfLoader,
};
use futures_util::StreamExt;
use ipnet::{IpNet, Ipv4Net};
use log::{info, warn};
use redis::aio::PubSub;
use tokio::{sync::oneshot, task::JoinHandle};

static BPF_OBJ: &[u8] = include_bytes!(concat!(env!("OUT_DIR"), "/radixip-xdp"));

#[derive(Clone, Debug)]
pub struct Config {
    pub iface: String,
    pub mode: Mode,
    pub pin_dir: PathBuf,
    pub redis_url: String,
    pub channel: String,
    pub snapshot_key: String,
    pub fail_closed: bool,
    pub min_v4: u8,
    pub min_v6: u8,
    pub never_block: Vec<IpNet>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum Mode {
    Skb,
    Driver,
}

pub struct Runtime {
    shutdown: Option<oneshot::Sender<()>>,
    task: Option<JoinHandle<()>>,
}

impl Runtime {
    pub async fn shutdown(mut self) -> Result<()> {
        if let Some(shutdown) = self.shutdown.take() {
            let _ = shutdown.send(());
        }
        if let Some(task) = self.task.take() {
            task.await.context("join XDP runtime")?;
        }
        Ok(())
    }
}

impl Drop for Runtime {
    fn drop(&mut self) {
        if let Some(shutdown) = self.shutdown.take() {
            let _ = shutdown.send(());
        }
    }
}

struct Maps {
    v4: LpmTrie<MapData, [u8; 4], u8>,
    v6: LpmTrie<MapData, [u8; 16], u8>,
    stats: PerCpuArray<MapData, u64>,
    entries: HashSet<IpNet>,
}

impl Maps {
    fn put(&mut self, net: IpNet) -> Result<()> {
        match net {
            IpNet::V4(prefix) => {
                self.v4.insert(
                    &Key::new(prefix.prefix_len() as u32, prefix.network().octets()),
                    1u8,
                    0,
                )?;
            }
            IpNet::V6(prefix) => {
                self.v6.insert(
                    &Key::new(prefix.prefix_len() as u32, prefix.network().octets()),
                    1u8,
                    0,
                )?;
            }
        }
        self.entries.insert(net);
        Ok(())
    }

    fn delete(&mut self, net: IpNet) {
        let _ = match net {
            IpNet::V4(prefix) => self.v4.remove(&Key::new(
                prefix.prefix_len() as u32,
                prefix.network().octets(),
            )),
            IpNet::V6(prefix) => self.v6.remove(&Key::new(
                prefix.prefix_len() as u32,
                prefix.network().octets(),
            )),
        };
        self.entries.remove(&net);
    }

    fn stat(&self, index: u32) -> u64 {
        self.stats
            .get(&index, 0)
            .map(|values| values.iter().sum())
            .unwrap_or(0)
    }
}

/// Load maps, subscribe and seed from Redis, then attach the XDP program.
/// The returned handle owns the attachment and background synchronization task.
pub async fn start(config: Config) -> Result<Runtime> {
    let started = Instant::now();
    std::fs::create_dir_all(&config.pin_dir).with_context(|| {
        format!(
            "create pin directory {:?} (is bpffs mounted?)",
            config.pin_dir
        )
    })?;

    let mut ebpf = EbpfLoader::new()
        .map_pin_path(&config.pin_dir)
        .load(BPF_OBJ)
        .context(
            "load eBPF object (kernel too old, missing capabilities, or pinned map mismatch)",
        )?;

    let mut maps = Maps {
        v4: LpmTrie::try_from(
            ebpf.take_map("blocklist_v4")
                .context("no blocklist_v4 map")?,
        )?,
        v6: LpmTrie::try_from(
            ebpf.take_map("blocklist_v6")
                .context("no blocklist_v6 map")?,
        )?,
        stats: PerCpuArray::try_from(ebpf.take_map("stats").context("no stats map")?)?,
        entries: HashSet::new(),
    };

    let client = redis::Client::open(config.redis_url.as_str())?;
    let mut pubsub: Option<PubSub> = match client.get_async_pubsub().await {
        Ok(mut pubsub) => match pubsub.subscribe(&config.channel).await {
            Ok(()) => Some(pubsub),
            Err(error) => {
                warn!("Redis subscribe failed for {}: {error}", config.channel);
                None
            }
        },
        Err(error) => {
            warn!("Redis pub/sub unavailable: {error}");
            None
        }
    };

    let mut seeded = false;
    match client.get_multiplexed_async_connection().await {
        Ok(mut connection) => {
            match redis::cmd("SMEMBERS")
                .arg(&config.snapshot_key)
                .query_async::<Vec<String>>(&mut connection)
                .await
            {
                Ok(items) => {
                    let (mut accepted, mut rejected) = (0usize, 0usize);
                    for item in items {
                        match parse_net(&item)
                            .and_then(|net| normalize(&config, net))
                            .and_then(|net| maps.put(net))
                        {
                            Ok(()) => accepted += 1,
                            Err(error) => {
                                rejected += 1;
                                warn!("skip blocklist seed {item}: {error}");
                            }
                        }
                    }
                    info!("seeded {accepted} blocklist prefixes ({rejected} rejected)");
                    seeded = true;
                }
                Err(error) => warn!("Redis snapshot read failed: {error}"),
            }
        }
        Err(error) => warn!("Redis unavailable for XDP seed: {error}"),
    }

    if !seeded && config.fail_closed {
        bail!(
            "xdp.fail_closed is set and blocklist snapshot could not be read; refusing to attach"
        );
    }
    if !seeded {
        warn!("attaching with an unseeded map; userspace remains the only active filter");
    }

    let program: &mut Xdp = ebpf
        .program_mut("radixip_xdp")
        .context("program radixip_xdp not found")?
        .try_into()?;
    program.load().context("verifier rejected XDP program")?;
    let flags = match config.mode {
        Mode::Skb => XdpFlags::SKB_MODE,
        Mode::Driver => XdpFlags::DRV_MODE,
    };
    let link = program
        .attach(&config.iface, flags)
        .with_context(|| format!("attach XDP to {} ({:?})", config.iface, config.mode))?;
    info!(
        "XDP attached on {} ({:?}); seed phase took {:?}",
        config.iface,
        config.mode,
        started.elapsed()
    );

    let (shutdown, mut shutdown_rx) = oneshot::channel();
    let task = tokio::spawn(async move {
        let _link = link;
        let _ebpf = ebpf;
        let mut tick = tokio::time::interval(Duration::from_secs(10));
        let mut stream = pubsub
            .as_mut()
            .map(|subscription| subscription.on_message());

        loop {
            tokio::select! {
                _ = &mut shutdown_rx => {
                    info!("stopping embedded XDP runtime");
                    break;
                }
                _ = tick.tick() => {
                    info!(
                        "XDP stats: v4 drop={} pass={} | v6 drop={} pass={} | parse_err={} | entries={}",
                        maps.stat(0), maps.stat(1), maps.stat(2), maps.stat(3), maps.stat(4), maps.entries.len()
                    );
                }
                message = async {
                    match stream.as_mut() {
                        Some(messages) => messages.next().await,
                        None => std::future::pending().await,
                    }
                } => match message {
                    Some(message) => match message.get_payload::<String>() {
                        Ok(payload) => if let Err(error) = apply_event(&config, &mut maps, &payload) {
                            warn!("ignored XDP Redis payload {payload:?}: {error}");
                        },
                        Err(error) => warn!("invalid XDP Redis payload: {error}"),
                    },
                    None => {
                        warn!("Redis pub/sub closed; XDP map remains active without updates");
                        stream = None;
                    }
                }
            }
        }
    });

    Ok(Runtime {
        shutdown: Some(shutdown),
        task: Some(task),
    })
}

fn normalize(config: &Config, net: IpNet) -> Result<IpNet> {
    let net = net.trunc();
    let net = match net {
        IpNet::V6(prefix) if prefix.prefix_len() >= 96 => match prefix.network().to_ipv4_mapped() {
            Some(address) => IpNet::V4(Ipv4Net::new(address, prefix.prefix_len() - 96)?),
            None => IpNet::V6(prefix),
        },
        other => other,
    };
    let minimum = if net.addr().is_ipv4() {
        config.min_v4
    } else {
        config.min_v6
    };
    if net.prefix_len() < minimum {
        bail!("{net}: prefix shorter than minimum /{minimum}");
    }
    for protected in &config.never_block {
        let overlaps = match (net, *protected) {
            (IpNet::V4(candidate), IpNet::V4(protected)) => {
                candidate.contains(&protected) || protected.contains(&candidate)
            }
            (IpNet::V6(candidate), IpNet::V6(protected)) => {
                candidate.contains(&protected) || protected.contains(&candidate)
            }
            _ => false,
        };
        if overlaps {
            bail!("{net}: overlaps never-block entry {protected}");
        }
    }
    Ok(net)
}

fn parse_net(value: &str) -> Result<IpNet> {
    let value = value.trim();
    if value.contains('/') {
        return Ok(value.parse()?);
    }
    Ok(IpNet::from(value.parse::<IpAddr>()?))
}

fn apply_event(config: &Config, maps: &mut Maps, payload: &str) -> Result<()> {
    let mut fields = payload.split_whitespace();
    let first = fields.next().ok_or_else(|| anyhow!("empty message"))?;
    let (operation, cidr) = match first {
        "add" | "del" => (first, fields.next().ok_or_else(|| anyhow!("missing CIDR"))?),
        _ => ("add", first),
    };
    let net = normalize(config, parse_net(cidr)?)?;
    if operation == "add" {
        maps.put(net)
    } else {
        maps.delete(net);
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::{normalize, parse_net, Config, Mode};
    use ipnet::IpNet;
    use std::path::PathBuf;

    fn config(never_block: Vec<IpNet>) -> Config {
        Config {
            iface: "eth0".into(),
            mode: Mode::Skb,
            pin_dir: PathBuf::from("/tmp/radixip-test-bpffs"),
            redis_url: "redis://127.0.0.1:6379".into(),
            channel: "security:blocklist".into(),
            snapshot_key: "security:blocklist:set".into(),
            fail_closed: false,
            min_v4: 8,
            min_v6: 16,
            never_block,
        }
    }

    #[test]
    fn mapped_ipv6_prefix_normalizes_to_ipv4() {
        let net = parse_net("::ffff:198.51.100.0/120").unwrap();
        let normalized = normalize(&config(Vec::new()), net).unwrap();
        assert_eq!(normalized, "198.51.100.0/24".parse().unwrap());
    }

    #[test]
    fn rejects_too_broad_and_protected_prefixes() {
        let protected = "10.0.0.0/8".parse().unwrap();
        let config = config(vec![protected]);

        for value in ["0.0.0.0/0", "10.1.0.0/16", "10.0.0.0/7"] {
            let net = parse_net(value).unwrap();
            assert!(
                normalize(&config, net).is_err(),
                "{value} should be rejected"
            );
        }
    }

    #[test]
    fn bare_ip_becomes_a_host_prefix() {
        assert_eq!(
            parse_net("2001:db8::1").unwrap(),
            "2001:db8::1/128".parse().unwrap()
        );
    }
}
