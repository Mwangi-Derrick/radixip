fn main() -> Result<(), Box<dyn std::error::Error>> {
    let protoc = protoc_bin_vendored::protoc_bin_path()?;
    std::env::set_var("PROTOC", protoc);
    tonic_build::compile_protos("../../proto/radixip/v1/radixip.proto")?;

    // libelf (pulled in by xdp-loader / libbpf-sys) requires zstd for compressed ELF sections.
    #[cfg(target_os = "linux")]
    println!("cargo:rustc-link-lib=zstd");

    Ok(())
}
