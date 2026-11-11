use std::{env, fs, path::PathBuf};

fn main() {
    let manifest_dir = PathBuf::from(env::var_os("CARGO_MANIFEST_DIR").unwrap());
    let object = manifest_dir.join("../../lib/go/xdp/xdpblocklist_bpfel.o");
    let output = PathBuf::from(env::var_os("OUT_DIR").unwrap()).join("radixip-xdp");

    println!("cargo:rerun-if-changed={}", object.display());
    if object.is_file() {
        fs::copy(&object, &output).expect("copy xdpblocklist_bpfel.o");
    } else {
        panic!("Missing lib/go/xdp/xdpblocklist_bpfel.o; run `go generate ./lib/go/xdp/...` first");
    }
}
