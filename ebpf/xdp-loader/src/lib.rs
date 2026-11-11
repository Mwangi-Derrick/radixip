use std::{
    collections::HashSet,
    ffi::CString,
    net::IpAddr,
    os::fd::{AsFd, AsRawFd},
    path::PathBuf,
    time::{Duration, Instant},
};

use anyhow::{anyhow, bail, Context, Result};
use futures_util::StreamExt;
use ipnet::{IpNet, Ipv4Net};
use libbpf_rs::{MapCore, Object, ObjectBuilder};
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
    Auto,
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

#[repr(C, packed)]
#[derive(Clone, Copy)]
pub struct LpmKeyV4 {
    pub prefixlen: u32,
    pub data: [u8; 4],
}

#[repr(C, packed)]
#[derive(Clone, Copy)]
pub struct LpmKeyV6 {
    pub prefixlen: u32,
    pub data: [u8; 16],
}

pub struct Maps {
    v4_fd: i32,
    v6_fd: i32,
    stats_fd: i32,
    entries: HashSet<IpNet>,
}

impl Maps {
    pub fn new(obj: &Object) -> Result<Self> {
        let v4 = obj
            .maps()
            .find(|m| m.name() == "blocklist_v4")
            .context("no blocklist_v4 map")?;
        let v6 = obj
            .maps()
            .find(|m| m.name() == "blocklist_v6")
            .context("no blocklist_v6 map")?;
        let stats = obj
            .maps()
            .find(|m| m.name() == "stats")
            .context("no stats map")?;

        Ok(Self {
            v4_fd: v4.as_fd().as_raw_fd(),
            v6_fd: v6.as_fd().as_raw_fd(),
            stats_fd: stats.as_fd().as_raw_fd(),
            entries: HashSet::new(),
        })
    }

    pub fn put(&mut self, net: IpNet) -> Result<()> {
        let value = 1u8;
        match net {
            IpNet::V4(prefix) => {
                let key = LpmKeyV4 {
                    prefixlen: prefix.prefix_len() as u32,
                    data: prefix.network().octets(),
                };
                unsafe {
                    let key_ptr = &key as *const _ as *const libc::c_void;
                    let val_ptr = &value as *const _ as *const libc::c_void;
                    let res = libc::syscall(
                        libc::SYS_bpf,
                        2, // BPF_MAP_UPDATE_ELEM
                        &bpf_map_update_attr {
                            map_fd: self.v4_fd as u32,
                            key: key_ptr as u64,
                            value: val_ptr as u64,
                            flags: 0,
                        },
                        std::mem::size_of::<bpf_map_update_attr>(),
                    );
                    if res < 0 {
                        bail!("BPF update v4 map error: {}", std::io::Error::last_os_error());
                    }
                }
            }
            IpNet::V6(prefix) => {
                let key = LpmKeyV6 {
                    prefixlen: prefix.prefix_len() as u32,
                    data: prefix.network().octets(),
                };
                unsafe {
                    let key_ptr = &key as *const _ as *const libc::c_void;
                    let val_ptr = &value as *const _ as *const libc::c_void;
                    let res = libc::syscall(
                        libc::SYS_bpf,
                        2, // BPF_MAP_UPDATE_ELEM
                        &bpf_map_update_attr {
                            map_fd: self.v6_fd as u32,
                            key: key_ptr as u64,
                            value: val_ptr as u64,
                            flags: 0,
                        },
                        std::mem::size_of::<bpf_map_update_attr>(),
                    );
                    if res < 0 {
                        bail!("BPF update v6 map error: {}", std::io::Error::last_os_error());
                    }
                }
            }
        }
        self.entries.insert(net);
        Ok(())
    }

    pub fn delete(&mut self, net: IpNet) {
        match net {
            IpNet::V4(prefix) => {
                let key = LpmKeyV4 {
                    prefixlen: prefix.prefix_len() as u32,
                    data: prefix.network().octets(),
                };
                unsafe {
                    let key_ptr = &key as *const _ as *const libc::c_void;
                    let _ = libc::syscall(
                        libc::SYS_bpf,
                        3, // BPF_MAP_DELETE_ELEM
                        &bpf_map_delete_attr {
                            map_fd: self.v4_fd as u32,
                            key: key_ptr as u64,
                        },
                        std::mem::size_of::<bpf_map_delete_attr>(),
                    );
                }
            }
            IpNet::V6(prefix) => {
                let key = LpmKeyV6 {
                    prefixlen: prefix.prefix_len() as u32,
                    data: prefix.network().octets(),
                };
                unsafe {
                    let key_ptr = &key as *const _ as *const libc::c_void;
                    let _ = libc::syscall(
                        libc::SYS_bpf,
                        3, // BPF_MAP_DELETE_ELEM
                        &bpf_map_delete_attr {
                            map_fd: self.v6_fd as u32,
                            key: key_ptr as u64,
                        },
                        std::mem::size_of::<bpf_map_delete_attr>(),
                    );
                }
            }
        }
        self.entries.remove(&net);
    }

    pub fn stat(&self, index: u32) -> u64 {
        let ncpus = match num_cpus() {
            Ok(n) => n,
            Err(_) => 1,
        };
        let mut values = vec![0u64; ncpus];
        unsafe {
            let key_ptr = &index as *const _ as *const libc::c_void;
            let val_ptr = values.as_mut_ptr() as *mut libc::c_void;
            let res = libc::syscall(
                libc::SYS_bpf,
                1, // BPF_MAP_LOOKUP_ELEM
                &bpf_map_lookup_attr {
                    map_fd: self.stats_fd as u32,
                    key: key_ptr as u64,
                    value: val_ptr as u64,
                    flags: 0,
                },
                std::mem::size_of::<bpf_map_lookup_attr>(),
            );
            if res == 0 {
                values.iter().sum()
            } else {
                0
            }
        }
    }
}

#[repr(C)]
struct bpf_map_lookup_attr {
    map_fd: u32,
    key: u64,
    value: u64,
    flags: u64,
}

#[repr(C)]
struct bpf_map_update_attr {
    map_fd: u32,
    key: u64,
    value: u64,
    flags: u64,
}

#[repr(C)]
struct bpf_map_delete_attr {
    map_fd: u32,
    key: u64,
}

fn num_cpus() -> Result<usize> {
    let count = unsafe { libc::sysconf(libc::_SC_NPROCESSORS_ONLN) };
    if count > 0 {
        Ok(count as usize)
    } else {
        Ok(1)
    }
}

pub fn if_nametoindex(iface: &str) -> Result<u32> {
    let c_str = CString::new(iface)?;
    let idx = unsafe { libc::if_nametoindex(c_str.as_ptr()) };
    if idx == 0 {
        bail!("invalid interface {iface}");
    }
    Ok(idx)
}

pub fn attach_xdp(prog_fd: i32, ifindex: u32, mode: Mode) -> Result<u32> {
    const XDP_FLAGS_SKB_MODE: u32 = 1 << 1;
    const XDP_FLAGS_DRV_MODE: u32 = 1 << 2;

    match mode {
        Mode::Skb => {
            let res = unsafe {
                libbpf_sys::bpf_xdp_attach(ifindex as i32, prog_fd, XDP_FLAGS_SKB_MODE, std::ptr::null())
            };
            if res < 0 {
                bail!("bpf_xdp_attach SKB failed: {}", std::io::Error::last_os_error());
            }
            Ok(XDP_FLAGS_SKB_MODE)
        }
        Mode::Driver => {
            let res = unsafe {
                libbpf_sys::bpf_xdp_attach(ifindex as i32, prog_fd, XDP_FLAGS_DRV_MODE, std::ptr::null())
            };
            if res < 0 {
                warn!("XDP DRV mode failed; falling back to SKB");
                let fallback = unsafe {
                    libbpf_sys::bpf_xdp_attach(ifindex as i32, prog_fd, XDP_FLAGS_SKB_MODE, std::ptr::null())
                };
                if fallback < 0 {
                    bail!("bpf_xdp_attach fallback to SKB failed: {}", std::io::Error::last_os_error());
                }
                Ok(XDP_FLAGS_SKB_MODE)
            } else {
                Ok(XDP_FLAGS_DRV_MODE)
            }
        }
        Mode::Auto => {
            let res = unsafe {
                libbpf_sys::bpf_xdp_attach(ifindex as i32, prog_fd, XDP_FLAGS_DRV_MODE, std::ptr::null())
            };
            if res < 0 {
                let fallback = unsafe {
                    libbpf_sys::bpf_xdp_attach(ifindex as i32, prog_fd, XDP_FLAGS_SKB_MODE, std::ptr::null())
                };
                if fallback < 0 {
                    bail!("bpf_xdp_attach auto SKB failed: {}", std::io::Error::last_os_error());
                }
                Ok(XDP_FLAGS_SKB_MODE)
            } else {
                Ok(XDP_FLAGS_DRV_MODE)
            }
        }
    }
}

pub fn detach_xdp(ifindex: u32, flags: u32) {
    unsafe {
        let _ = libbpf_sys::bpf_xdp_detach(ifindex as i32, flags, std::ptr::null());
    }
}

pub async fn start(config: Config) -> Result<Runtime> {
    let started = Instant::now();
    let ifindex = if_nametoindex(&config.iface)?;

    let mut builder = ObjectBuilder::default();
    let open_obj = builder
        .open_memory(BPF_OBJ)
        .context("open embedded BPF object with libbpf")?;
    let obj = open_obj.load().context("load BPF object with libbpf")?;

    let mut maps = Maps::new(&obj)?;

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
        bail!("xdp.fail_closed is set and blocklist snapshot could not be read; refusing to attach");
    }

    let prog = obj
        .progs()
        .find(|p| p.name() == "radixip_xdp")
        .context("prog radixip_xdp not found")?;

    let prog_fd = prog.as_fd().as_raw_fd();
    let flags = attach_xdp(prog_fd, ifindex, config.mode)?;

    info!(
        "libbpf: XDP attached on {} (flags: {:#x}); seed phase took {:?}",
        config.iface,
        flags,
        started.elapsed()
    );

    let (shutdown, mut shutdown_rx) = oneshot::channel();
    // In libbpf, once the program is attached to the interface and FDs are extracted,
    // the kernel holds references to the attached program and maps.
    drop(obj);

    let task = tokio::spawn(async move {
        let mut tick = tokio::time::interval(Duration::from_secs(10));
        let mut stream = pubsub
            .as_mut()
            .map(|subscription| subscription.on_message());

        loop {
            tokio::select! {
                _ = &mut shutdown_rx => {
                    info!("stopping libbpf XDP runtime");
                    detach_xdp(ifindex, flags);
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
