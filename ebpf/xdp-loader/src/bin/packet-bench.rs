use std::{
    env,
    net::IpAddr,
    process::{Command, Stdio},
    time::Instant,
};

use anyhow::{bail, Context, Result};
use aya::{
    maps::{
        lpm_trie::{Key, LpmTrie},
        MapData, PerCpuArray,
    },
    programs::{Xdp, XdpFlags},
    EbpfLoader,
};
use ipnet::IpNet;

static BPF_OBJ: &[u8] = include_bytes!(concat!(env!("OUT_DIR"), "/radixip-xdp"));

fn main() -> Result<()> {
    let args: Vec<String> = env::args().collect();
    let mut iface = String::new();
    let mut source = String::new();
    let mut destination = String::new();
    let mut client_netns = String::new();
    let mut mode = String::from("skb");
    let mut duration_secs: u64 = 5;

    let mut i = 1;
    while i < args.len() {
        match args[i].as_str() {
            "-iface" => {
                i += 1;
                if i < args.len() {
                    iface = args[i].clone();
                }
            }
            "-source" => {
                i += 1;
                if i < args.len() {
                    source = args[i].clone();
                }
            }
            "-destination" => {
                i += 1;
                if i < args.len() {
                    destination = args[i].clone();
                }
            }
            "-client-netns" => {
                i += 1;
                if i < args.len() {
                    client_netns = args[i].clone();
                }
            }
            "-mode" => {
                i += 1;
                if i < args.len() {
                    mode = args[i].clone();
                }
            }
            "-duration" => {
                i += 1;
                if i < args.len() {
                    duration_secs = args[i].parse().unwrap_or(5);
                }
            }
            _ => {}
        }
        i += 1;
    }

    if iface.is_empty() || source.is_empty() || destination.is_empty() || client_netns.is_empty() {
        bail!("missing required flags: -iface, -source, -destination, -client-netns");
    }

    let source_ip: IpAddr = source.parse().context("parse source IP")?;
    let dest_ip: IpAddr = destination.parse().context("parse destination IP")?;

    let prefix: IpNet = match source_ip {
        IpAddr::V4(addr) => IpNet::V4(ipnet::Ipv4Net::new(addr, 32)?),
        IpAddr::V6(addr) => IpNet::V6(ipnet::Ipv6Net::new(addr, 128)?),
    };

    let mut ebpf = EbpfLoader::new()
        .load(BPF_OBJ)
        .context("load eBPF object")?;

    let mut v4: LpmTrie<MapData, [u8; 4], u8> =
        LpmTrie::try_from(ebpf.take_map("blocklist_v4").context("no blocklist_v4")?)?;
    let mut v6: LpmTrie<MapData, [u8; 16], u8> =
        LpmTrie::try_from(ebpf.take_map("blocklist_v6").context("no blocklist_v6")?)?;
    let stats: PerCpuArray<MapData, u64> =
        PerCpuArray::try_from(ebpf.take_map("stats").context("no stats")?)?;

    match prefix {
        IpNet::V4(p) => {
            v4.insert(&Key::new(p.prefix_len() as u32, p.network().octets()), 1u8, 0)?;
        }
        IpNet::V6(p) => {
            v6.insert(&Key::new(p.prefix_len() as u32, p.network().octets()), 1u8, 0)?;
        }
    }

    let program: &mut Xdp = ebpf
        .program_mut("radixip_xdp")
        .context("radixip_xdp not found")?
        .try_into()?;
    program.load().context("verifier rejected XDP program")?;

    let (flags, attached_mode) = match mode.as_str() {
        "drv" => (XdpFlags::DRV_MODE, "drv"),
        _ => (XdpFlags::SKB_MODE, "skb"),
    };

    let link = program
        .attach(&iface, flags)
        .with_context(|| format!("attach XDP {mode} to {iface}"))?;

    let stat_fn = |idx: u32| -> u64 {
        stats.get(&idx, 0).map(|v| v.iter().sum()).unwrap_or(0)
    };

    // stat indices: 0 = v4 drop, 1 = v4 pass, 2 = v6 drop, 3 = v6 pass, 4 = parse error
    let drop_idx = if source_ip.is_ipv4() { 0 } else { 2 };
    let before_drops = stat_fn(drop_idx);
    let before_pass = stat_fn(1) + stat_fn(3);
    let before_parse_err = stat_fn(4);

    let start = Instant::now();
    let status = Command::new("ip")
        .args([
            "netns",
            "exec",
            &client_netns,
            "ping",
            "-f",
            "-n",
            "-q",
            "-w",
            &duration_secs.to_string(),
            "-I",
            &source_ip.to_string(),
            &dest_ip.to_string(),
        ])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();

    let elapsed = start.elapsed();
    let after_drops = stat_fn(drop_idx);
    let after_pass = stat_fn(1) + stat_fn(3);
    let after_parse_err = stat_fn(4);

    let dropped = after_drops.saturating_sub(before_drops);
    let duration_secs_f64 = elapsed.as_secs_f64();
    let pps = if duration_secs_f64 > 0.0 {
        (dropped as f64) / duration_secs_f64
    } else {
        0.0
    };

    if dropped == 0 {
        bail!("Rust XDP recorded 0 drops; check interface/rules");
    }

    println!(
        "runtime=rust requested_mode={} attached_mode={} iface={} source={} destination={} duration_seconds={:.3} xdp_dropped_packets={} xdp_drop_pps={:.0}",
        mode, attached_mode, iface, source_ip, dest_ip, duration_secs_f64, dropped, pps
    );
    println!(
        "xdp_pass_delta={} xdp_parse_error_delta={} generator_exit={}",
        after_pass.saturating_sub(before_pass),
        after_parse_err.saturating_sub(before_parse_err),
        status.map(|s| s.code().unwrap_or(-1)).unwrap_or(-1)
    );

    drop(link);
    Ok(())
}
