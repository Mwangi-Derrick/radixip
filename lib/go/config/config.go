// Package config provides typed configuration structs for RadixIP.
//
// Supports loading from a YAML or TOML file, with sensible defaults and
// validation. The schema mirrors lib/rust/config/src/lib.rs exactly so that
// a single radixip.yaml works across both language stacks.
//
// # Example
//
//	cfg, err := config.LoadFromFile("radixip.yaml")
//	if err != nil { log.Fatal(err) }
//	fmt.Println(cfg.RadixIP.RateLimit.Capacity)
package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Top-level
// ---------------------------------------------------------------------------

// RadixIpConfig is the root configuration document.
type RadixIpConfig struct {
	RadixIP RootConfig `yaml:"radixip"`
}

// LoadFromFile reads and parses a YAML configuration file.
func LoadFromFile(path string) (*RadixIpConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return ParseYAML(raw)
}

// ParseYAML parses raw YAML bytes into a validated RadixIpConfig.
func ParseYAML(data []byte) (*RadixIpConfig, error) {
	var cfg RadixIpConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: yaml parse: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *RadixIpConfig) applyDefaults() {
	c.RadixIP.applyDefaults()
}

func (c *RadixIpConfig) validate() error {
	rl := &c.RadixIP.RateLimit
	if rl.Enabled {
		if rl.Capacity == 0 {
			return errors.New("config: rate_limit.capacity must be > 0")
		}
		if rl.RefillRate == 0 {
			return errors.New("config: rate_limit.refill_rate must be > 0")
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Root
// ---------------------------------------------------------------------------

// RootConfig is the `radixip:` block in the YAML.
type RootConfig struct {
	Engine     EngineConfig     `yaml:"engine"`
	Middleware MiddlewareConfig `yaml:"middleware"`
	Blocklist  BlocklistConfig  `yaml:"blocklist"`
	RateLimit  RateLimitConfig  `yaml:"rate_limit"`
	Metrics    MetricsConfig    `yaml:"metrics"`
}

func (r *RootConfig) applyDefaults() {
	r.Engine.applyDefaults()
	r.Middleware.applyDefaults()
	r.RateLimit.applyDefaults()
	r.Metrics.applyDefaults()
}

// ---------------------------------------------------------------------------
// Engine
// ---------------------------------------------------------------------------

// EngineConfig controls which RadixIP ART variant is instantiated.
type EngineConfig struct {
	// Variant: "concurrent" | "standard" | "lock_free" | "adaptive"
	Variant string `yaml:"variant"`
	// NodeVariant: "atomic" | "normal" | "padded" | "lock_free"
	NodeVariant string      `yaml:"node_variant"`
	NumShards   int         `yaml:"num_shards"`
	Cache       CacheConfig `yaml:"cache"`
}

func (e *EngineConfig) applyDefaults() {
	if e.Variant == "" {
		e.Variant = "concurrent"
	}
	if e.NodeVariant == "" {
		e.NodeVariant = "atomic"
	}
	if e.NumShards == 0 {
		e.NumShards = 16
	}
	e.Cache.applyDefaults()
}

// CacheConfig controls the lookup result cache layered on top of the ART.
type CacheConfig struct {
	Enabled    bool  `yaml:"enabled"`
	MaxEntries int   `yaml:"max_entries"`
	TTLSeconds *int  `yaml:"ttl_seconds"`
}

func (c *CacheConfig) applyDefaults() {
	if !c.Enabled && c.MaxEntries == 0 {
		c.Enabled = true
	}
	if c.MaxEntries == 0 {
		c.MaxEntries = 10_000
	}
	if c.TTLSeconds == nil {
		v := 3600
		c.TTLSeconds = &v
	}
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// MiddlewareConfig controls IP extraction and response codes.
type MiddlewareConfig struct {
	// IPSource: "x-forwarded-for" | "x-real-ip" | "remote-addr"
	IPSource       string         `yaml:"ip_source"`
	TrustedProxies []string       `yaml:"trusted_proxies"`
	Responses      ResponseConfig `yaml:"responses"`
}

func (m *MiddlewareConfig) applyDefaults() {
	if m.IPSource == "" {
		m.IPSource = "x-forwarded-for"
	}
	m.Responses.applyDefaults()
}

// ResponseConfig holds HTTP status codes for blocked/rate-limited responses.
type ResponseConfig struct {
	Blocked     int `yaml:"blocked"`
	RateLimited int `yaml:"rate_limited"`
}

func (r *ResponseConfig) applyDefaults() {
	if r.Blocked == 0 {
		r.Blocked = 403
	}
	if r.RateLimited == 0 {
		r.RateLimited = 429
	}
}

// ---------------------------------------------------------------------------
// Blocklist
// ---------------------------------------------------------------------------

// BlocklistConfig controls static blocklist loading.
type BlocklistConfig struct {
	Enabled bool              `yaml:"enabled"`
	Sources []BlocklistSource `yaml:"sources"`
}

// BlocklistSource describes a single source of blocked CIDRs.
type BlocklistSource struct {
	// Type: "inline" | "file"
	Type    string   `yaml:"type"`
	Subnets []string `yaml:"subnets"` // for type: inline
	Path    string   `yaml:"path"`    // for type: file
}

// ---------------------------------------------------------------------------
// Rate Limiting
// ---------------------------------------------------------------------------

// RateLimitConfig controls the token bucket rate limiter.
type RateLimitConfig struct {
	Enabled    bool            `yaml:"enabled"`
	Algorithm  string          `yaml:"algorithm"` // "token_bucket" (only current option)
	BucketMode BucketKeyMode   `yaml:"bucket_mode"`
	Capacity   uint64          `yaml:"capacity"`
	RefillRate uint64          `yaml:"refill_rate"`
	MaxBuckets uint64          `yaml:"max_buckets"`
	TTLSeconds uint32          `yaml:"ttl_seconds"`
}

func (r *RateLimitConfig) applyDefaults() {
	if r.Algorithm == "" {
		r.Algorithm = "token_bucket"
	}
	if r.Capacity == 0 {
		r.Capacity = 100
	}
	if r.RefillRate == 0 {
		r.RefillRate = 10
	}
	if r.MaxBuckets == 0 {
		r.MaxBuckets = 1_000_000
	}
	if r.TTLSeconds == 0 {
		r.TTLSeconds = 60
	}
	r.BucketMode.applyDefaults()
}

// BucketKeyMode controls how the rate limiter keys its buckets.
type BucketKeyMode struct {
	// Mode: "ip" | "subnet" | "both"
	Mode    string `yaml:"mode"`
	DepthV4 uint8  `yaml:"depth_v4"` // only for subnet/both, typically 24
	DepthV6 uint8  `yaml:"depth_v6"` // only for subnet/both, typically 48
}

func (b *BucketKeyMode) applyDefaults() {
	if b.Mode == "" {
		b.Mode = "ip"
	}
	if b.Mode != "ip" {
		if b.DepthV4 == 0 {
			b.DepthV4 = 24
		}
		if b.DepthV6 == 0 {
			b.DepthV6 = 48
		}
	}
}

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

// MetricsConfig controls Prometheus metrics exposure.
type MetricsConfig struct {
	Enabled        bool   `yaml:"enabled"`
	PrometheusPath string `yaml:"prometheus_path"`
}

func (m *MetricsConfig) applyDefaults() {
	if m.PrometheusPath == "" {
		m.PrometheusPath = "/metrics"
	}
}
