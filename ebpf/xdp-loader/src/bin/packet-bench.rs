use std::{
    env,
    net::IpAddr,
    os::fd::{AsFd, AsRawFd},
    path::PathBuf,
    process::{Command, Stdio},
    time::Instant,
};

use anyhow::{bail, Context, Result};
use ipnet::IpNet;
use libbpf_rs::ObjectBuilder;
use xdp_loader::{attach_xdp, detach_xdp, if_nametoindex, Maps, Mode};

static BPF_OBJ: &[u8] = include_bytes!(concat!(env!("OUT_DIR"), "/radixip-xdp"));

fn resolve_sender_bin(explicit: &str) -> Result<PathBuf> {
    if !explicit.is_empty() {
        let p = PathBuf::from(explicit);
        if p.is_file() {
            return Ok(p);
        }
    }

    if let Ok(env_path) = env::var("XDP_SENDER_BIN") {
        let p = PathBuf::from(env_path);
        if p.is_file() {
            return Ok(p);
        }
    }

    // Try relative to current working directory
    let cwd_candidate = PathBuf::from("bin/go-xdp-packet-bench");
    if cwd_candidate.is_file() {
        return Ok(cwd_candidate.canonicalize().unwrap_or(cwd_candidate));
    }

    // Try relative to current executable's directory
    if let Ok(exe_path) = env::current_exe() {
        if let Some(exe_dir) = exe_path.parent() {
            let exe_rel = exe_dir.join("go-xdp-packet-bench");
            if exe_rel.is_file() {
                return Ok(exe_rel);
            }
            // If in target/release or target/debug, check ../../bin/go-xdp-packet-bench
            let target_rel = exe_dir.join("../../bin/go-xdp-packet-bench");
            if target_rel.is_file() {
                return Ok(target_rel.canonicalize().unwrap_or(target_rel));
            }
        }
    }

    bail!("could not locate go-xdp-packet-bench; pass -sender-bin or ensure bin/go-xdp-packet-bench exists")
}

fn main() -> Result<()> {
    let args: Vec<String> = env::args().collect();
    let mut iface = String::new();
    let mut source = String::new();
    let mut destination = String::new();
    let mut client_netns = String::new();
    let mut mode_str = String::from("skb");
    let mut sender_bin_arg = String::new();
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
                    mode_str = args[i].clone();
                }
            }
            "-duration" => {
                i += 1;
                if i < args.len() {
                    duration_secs = args[i].parse().unwrap_or(5);
                }
            }
            "-sender-bin" => {
                i += 1;
                if i < args.len() {
                    sender_bin_arg = args[i].clone();
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
    let sender_bin = resolve_sender_bin(&sender_bin_arg)?;

    let prefix: IpNet = match source_ip {
        IpAddr::V4(addr) => IpNet::V4(ipnet::Ipv4Net::new(addr, 32)?),
        IpAddr::V6(addr) => IpNet::V6(ipnet::Ipv6Net::new(addr, 128)?),
    };

    let ifindex = if_nametoindex(&iface)?;
    let mut builder = ObjectBuilder::default();
    let open_obj = builder
        .open_memory(BPF_OBJ)
        .context("open embedded BPF object with libbpf")?;
    let obj = open_obj.load().context("load BPF object with libbpf")?;

    let mut maps = Maps::new(&obj)?;
    maps.put(prefix)?;

    let prog = obj
        .progs()
        .find(|p| p.name() == "radixip_xdp")
        .context("prog radixip_xdp not found")?;

    let mode = match mode_str.as_str() {
        "drv" => Mode::Driver,
        _ => Mode::Skb,
    };

    let prog_fd = prog.as_fd().as_raw_fd();
    let flags = attach_xdp(prog_fd, ifindex, mode)?;

    // stat indices: 0 = v4 drop, 1 = v4 pass, 2 = v6 drop, 3 = v6 pass, 4 = parse error
    let drop_idx = if source_ip.is_ipv4() { 0 } else { 2 };
    let before_drops = maps.stat(drop_idx);
    let before_pass = maps.stat(1) + maps.stat(3);
    let before_parse_err = maps.stat(4);

    let start = Instant::now();
    let status = Command::new("ip")
        .args([
            "netns",
            "exec",
            &client_netns,
            sender_bin.to_str().context("non-utf8 sender_bin path")?,
            "-sender-only",
            "-source",
            &source_ip.to_string(),
            "-destination",
            &dest_ip.to_string(),
            "-duration",
            &duration_secs.to_string(),
        ])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .status();

    let elapsed = start.elapsed();
    let after_drops = maps.stat(drop_idx);
    let after_pass = maps.stat(1) + maps.stat(3);
    let after_parse_err = maps.stat(4);

    detach_xdp(ifindex, flags);

    let dropped = after_drops.saturating_sub(before_drops);
    let duration_secs_f64 = elapsed.as_secs_f64();
    let pps = if duration_secs_f64 > 0.0 {
        (dropped as f64) / duration_secs_f64
    } else {
        0.0
    };

    if dropped == 0 {
        bail!("Rust libbpf XDP recorded 0 drops; check interface/rules");
    }

    println!(
        "runtime=rust requested_mode={} attached_mode={} iface={} source={} destination={} duration_seconds={:.3} xdp_dropped_packets={} xdp_drop_pps={:.0}",
        mode_str, mode_str, iface, source_ip, dest_ip, duration_secs_f64, dropped, pps
    );
    println!(
        "xdp_pass_delta={} xdp_parse_error_delta={} generator_exit={}",
        after_pass.saturating_sub(before_pass),
        after_parse_err.saturating_sub(before_parse_err),
        status.map(|s| s.code().unwrap_or(-1)).unwrap_or(-1)
    );

    Ok(())
}
