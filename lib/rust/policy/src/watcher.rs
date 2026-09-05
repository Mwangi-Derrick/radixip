//! Hot-reload watcher for `radixip.yaml` / any supported config file.
//!
//! Uses the [`notify`] crate to listen for file-system events and atomically
//! swaps the active [`PolicyState`] via [`arc_swap::ArcSwap`].
//!
//! # Design
//!
//! ```text
//! ┌──────────────┐    fs event    ┌─────────────┐    ArcSwap::store
//! │  notify      │ ─────────────► │  watcher    │ ────────────────►  PolicyState
//! │  background  │                │  goroutine  │                     (Engine + Limiter)
//! │  thread      │                └─────────────┘
//! └──────────────┘
//!
//! request hot path:  watcher.state().load() → &PolicyState  (wait-free)
//! ```

use crate::auto_ban::AutoBanTracker;
use crate::limiter::TokenBucketLimiter;
use crate::route_trie::RouteTrie;
use arc_swap::ArcSwap;
use notify::{
    event::{ModifyKind, RenameMode},
    Event, EventKind, RecommendedWatcher, RecursiveMode, Watcher,
};
use radixip::RadixEngine;
use radixip_config::{RadixIpConfig, RateLimitConfig};
use std::{
    path::{Path, PathBuf},
    sync::Arc,
    time::Duration,
};
use tracing::{error, info, warn};

// PolicyState — the thing we hot-swap

/// The complete live state managed by the hot-reload watcher.
pub struct PolicyState {
    pub limiter: TokenBucketLimiter,
    pub route_trie: Option<RouteTrie>,
    pub auto_ban: Option<AutoBanTracker>,
    pub config: Arc<RadixIpConfig>,
}

impl PolicyState {
    pub fn from_config_with_engine(cfg: Arc<RadixIpConfig>, engine: Arc<Box<dyn RadixEngine>>) -> Self {
        let rl: RateLimitConfig = cfg.radixip.rate_limit.clone();

        // Build route trie from config if enabled.
        let route_trie = if cfg.radixip.rate_limit_routes.enabled {
            let mut trie = RouteTrie::new();
            for route in &cfg.radixip.rate_limit_routes.routes {
                let methods: Vec<&str> = route.methods.iter().map(|s| s.as_str()).collect();
                trie.insert(&route.path, &methods, route.rate_limit.clone());
            }
            Some(trie)
        } else {
            None
        };

        // Build auto-ban tracker if enabled.
        let auto_ban = if cfg.radixip.auto_ban.enabled {
            Some(AutoBanTracker::new(&cfg.radixip.auto_ban, engine))
        } else {
            None
        };

        Self {
            limiter: TokenBucketLimiter::new(rl),
            route_trie,
            auto_ban,
            config: cfg,
        }
    }

    /// Convenience constructor for contexts without an engine (auto_ban will be None).
    pub fn from_config(cfg: Arc<RadixIpConfig>) -> Self {
        let rl: RateLimitConfig = cfg.radixip.rate_limit.clone();

        // Build route trie from config if enabled.
        let route_trie = if cfg.radixip.rate_limit_routes.enabled {
            let mut trie = RouteTrie::new();
            for route in &cfg.radixip.rate_limit_routes.routes {
                let methods: Vec<&str> = route.methods.iter().map(|s| s.as_str()).collect();
                trie.insert(&route.path, &methods, route.rate_limit.clone());
            }
            Some(trie)
        } else {
            None
        };

        Self {
            limiter: TokenBucketLimiter::new(rl),
            route_trie,
            auto_ban: None,
            config: cfg,
        }
    }
}

// ConfigWatcher

/// Watches a config file and keeps an [`ArcSwap<PolicyState>`] up-to-date.
///
/// Drop the `ConfigWatcher` to stop the background watcher thread.
pub struct ConfigWatcher {
    state: Arc<ArcSwap<PolicyState>>,
    /// Kept alive so the watcher thread doesn't stop.
    _watcher: RecommendedWatcher,
}

impl ConfigWatcher {
    /// Create a new watcher for `path`.  The file is parsed immediately; an
    /// error here means the initial config is invalid.
    pub fn new(path: impl AsRef<Path>) -> Result<Self, Box<dyn std::error::Error>> {
        let path = path.as_ref().to_path_buf();

        // Initial parse.
        let cfg = Arc::new(RadixIpConfig::from_file(&path)?);
        let state = Arc::new(ArcSwap::from_pointee(PolicyState::from_config(cfg)));

        let state_clone = Arc::clone(&state);
        let path_clone = path.clone();

        let mut watcher = notify::recommended_watcher(move |res: notify::Result<Event>| {
            Self::handle_event(&path_clone, &state_clone, res);
        })?;

        // Watch the parent directory so we catch editor-rename patterns.
        let dir = path.parent().unwrap_or_else(|| Path::new("."));
        watcher.watch(dir, RecursiveMode::NonRecursive)?;

        info!("radixip config watcher: watching {:?}", path);

        Ok(Self {
            state,
            _watcher: watcher,
        })
    }

    /// Returns a guard to the current [`PolicyState`]. This is a wait-free
    /// pointer load — safe on every request with zero lock contention.
    pub fn state(&self) -> arc_swap::Guard<Arc<PolicyState>> {
        self.state.load()
    }

    fn handle_event(path: &PathBuf, state: &Arc<ArcSwap<PolicyState>>, res: notify::Result<Event>) {
        let event = match res {
            Ok(e) => e,
            Err(e) => {
                warn!("radixip config watcher: notify error: {e}");
                return;
            }
        };

        // Only react to events on our file.
        let is_our_file = event.paths.iter().any(|p| p == path);
        if !is_our_file {
            return;
        }

        let interesting = matches!(
            event.kind,
            EventKind::Modify(ModifyKind::Data(_))
                | EventKind::Modify(ModifyKind::Name(RenameMode::To))
                | EventKind::Create(_)
        );

        if !interesting {
            return;
        }

        // Debounce: tiny sleep to let the editor finish writing.
        std::thread::sleep(Duration::from_millis(50));

        match RadixIpConfig::from_file(path) {
            Ok(new_cfg) => {
                let new_state = PolicyState::from_config(Arc::new(new_cfg));
                state.store(Arc::new(new_state));
                info!("radixip config watcher: hot-reloaded {:?} ✓", path);
            }
            Err(e) => {
                error!(
                    "radixip config watcher: reload failed (keeping old config): {e}"
                );
            }
        }
    }
}
