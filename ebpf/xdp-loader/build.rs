use std::{env, fs, path::PathBuf};

fn main() {
    let manifest_dir = PathBuf::from(env::var_os("CARGO_MANIFEST_DIR").unwrap());
    let object = manifest_dir.join("../../target/bpfel-unknown-none/release/radixip-xdp");
    let output = PathBuf::from(env::var_os("OUT_DIR").unwrap()).join("radixip-xdp");

    println!("cargo:rerun-if-changed={}", object.display());
    if object.is_file() {
        fs::copy(&object, &output).expect("copy the compiled XDP kernel object");
    } else {
        println!(
            "cargo:warning=XDP kernel object is missing; embedded XDP will fail open until `make -C ebpf kernel` builds it"
        );
        fs::write(&output, []).expect("write empty XDP object placeholder");
    }
}
