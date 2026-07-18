#pragma once
// RadixEngine.hpp - Idiomatic C++ RAII wrapper for RadixIP
//
// #include "RadixEngine.hpp"
// Link: -L. -lradixip
//
// Example:
//   radixip::Engine e;
//   e.insert("10.0.0.0/8", {{"action", "allow"}});
//   auto result = e.lookup("10.1.2.3");
//   if (result) std::cout << result->value;

#pragma once
#include <optional>
#include <stdexcept>
#include <string>
#include <unordered_map>

#include "radixip.h"

// nlohmann/json is the de-facto C++ JSON library.
// If you don't have it, replace the json parsing with a simpler approach.
#ifdef RADIXIP_USE_NLOHMANN_JSON
#  include <nlohmann/json.hpp>
#endif

namespace radixip {

// ---------------------------------------------------------------------------
// Metadata type
// ---------------------------------------------------------------------------

struct Metadata {
    std::string                                      value;
    std::unordered_map<std::string, std::string>     attributes;
};

// ---------------------------------------------------------------------------
// Engine
// ---------------------------------------------------------------------------

class Engine {
public:
    /// Construct a new balanced radix engine.
    Engine() {
        handle_ = radix_engine_new();
        if (!handle_) {
            throw std::runtime_error("radixip: failed to create engine");
        }
    }

    ~Engine() {
        if (handle_) {
            radix_engine_free(handle_);
            handle_ = nullptr;
        }
    }

    // Non-copyable; use std::shared_ptr<Engine> for shared ownership.
    Engine(const Engine&)            = delete;
    Engine& operator=(const Engine&) = delete;

    // Moveable
    Engine(Engine&& other) noexcept : handle_(other.handle_) {
        other.handle_ = nullptr;
    }
    Engine& operator=(Engine&& other) noexcept {
        if (this != &other) {
            if (handle_) radix_engine_free(handle_);
            handle_ = other.handle_;
            other.handle_ = nullptr;
        }
        return *this;
    }

    // -----------------------------------------------------------------------
    // Mutations
    // -----------------------------------------------------------------------

    /// Insert a prefix with a simple JSON-serialised metadata dict.
    void insert(const std::string& cidr,
                const std::unordered_map<std::string, std::string>& attrs = {},
                const std::string& value = "") {
        // Build a minimal JSON string: {"value":"...","attributes":{...}}
        std::string json = R"({"value":")" + value + R"(","attributes":{)";
        bool first = true;
        for (const auto& [k, v] : attrs) {
            if (!first) json += ',';
            json += '"' + k + R"(":")" + v + '"';
            first = false;
        }
        json += "}}";

        int rc = radix_engine_insert(handle_, cidr.c_str(), json.c_str());
        if (rc != 0) {
            throw std::runtime_error("radixip: insert failed for " + cidr);
        }
    }

    /// Remove a prefix. Returns true if the entry existed.
    bool remove(const std::string& cidr) {
        return radix_engine_remove(handle_, cidr.c_str()) == 0;
    }

    /// Remove all entries.
    void clear() {
        radix_engine_clear(handle_);
    }

    // -----------------------------------------------------------------------
    // Queries
    // -----------------------------------------------------------------------

    /// Longest-prefix match. Returns nullopt if no prefix covers the IP.
    std::optional<Metadata> lookup(const std::string& ip) const {
        char* raw = radix_engine_match(handle_, ip.c_str());
        if (!raw) return std::nullopt;

        // Parse the returned JSON string back into a Metadata struct.
        std::string json_str(raw);
        radix_engine_free_string(raw);

        Metadata meta;

#ifdef RADIXIP_USE_NLOHMANN_JSON
        auto j = nlohmann::json::parse(json_str, nullptr, /*throw_on_error=*/false);
        if (!j.is_discarded()) {
            meta.value = j.value("value", "");
            if (j.contains("attributes") && j["attributes"].is_object()) {
                for (auto& [k, v] : j["attributes"].items()) {
                    meta.attributes[k] = v.get<std::string>();
                }
            }
        }
#else
        // Minimal fallback: treat entire JSON as value string
        meta.value = json_str;
#endif

        return meta;
    }

    /// Returns true if any stored prefix covers the IP.
    bool contains(const std::string& ip) const {
        return radix_engine_contains(handle_, ip.c_str());
    }

    /// Number of stored prefixes.
    size_t size() const {
        return radix_engine_size(handle_);
    }

    // -----------------------------------------------------------------------
    // Metadata
    // -----------------------------------------------------------------------

    static std::string version() {
        return radix_engine_version();
    }

private:
    RadixEngineHandle* handle_ = nullptr;
};

} // namespace radixip
