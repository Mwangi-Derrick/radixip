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

    // parse_deps = false: cbindgen only inspects *this* crate's own lib.rs
    // for #[no_mangle] exports.  We don't want it to follow the #[path]
    // include into lib/rust/engine/src/art/ — that would require a fully
    // resolved Rust parse of the engine crate, which is unnecessary and was
    // the source of the "ParseCannotOpenFile" error after the engine was
    // moved from lib/rust/ to lib/rust/engine/.
    cbindgen::Builder::new()
        .with_crate(&crate_dir)
        .with_parse_deps(false)
        .with_language(cbindgen::Language::C)
        .with_include_guard("NODE16_SIMD_FFI_H")
        .with_documentation(true)
        .generate()
        .expect("cbindgen failed — is it installed? (`cargo install cbindgen`)")
        .write_to_file(out_header);
}
