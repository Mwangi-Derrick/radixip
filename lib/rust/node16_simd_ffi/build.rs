// build.rs — generate node16_simd.h from the #[no_mangle] extern "C" exports.
//
// cbindgen reads this crate's source and emits a minimal C header that CGo
// (and any other C consumer) can #include.  The header is written to:
//
//   $CARGO_MANIFEST_DIR/../../../lib/vendor/include/node16_simd.h
//
// so it lands in the canonical vendor directory regardless of which platform
// or CI runner builds the crate.

fn main() {
    let crate_dir = std::env::var("CARGO_MANIFEST_DIR").unwrap();
    let out_header =
        std::path::PathBuf::from(&crate_dir).join("../../../lib/vendor/include/node16_simd.h");

    // Ensure the target directory exists.
    if let Some(parent) = out_header.parent() {
        std::fs::create_dir_all(parent).expect("failed to create vendor/include dir");
    }

    // If the header is already present (committed to the repo), skip
    // regeneration.  cbindgen struggles to resolve the cross-crate #[path]
    // include after the engine was moved to lib/rust/engine/, and the header
    // content is stable — it only needs updating when the extern "C" signature
    // changes, at which point the developer should delete the file and rebuild.
    if out_header.exists() {
        println!("cargo:warning=node16_simd.h already exists, skipping cbindgen regeneration.");
        return;
    }

    cbindgen::Builder::new()
        .with_crate(&crate_dir)
        .with_language(cbindgen::Language::C)
        .with_include_guard("NODE16_SIMD_FFI_H")
        .with_documentation(true)
        .generate()
        .expect("cbindgen failed — is it installed? (`cargo install cbindgen`)")
        .write_to_file(out_header);
}

