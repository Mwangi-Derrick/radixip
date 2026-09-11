# Redis cache sync

This guide shows the intended shared-cache flow in RadixIP: keep the hot `Match()` path local, and use Redis for cross-instance cache invalidation and propagation.

## Start Redis

```bash
docker compose up -d redis
```

## Rust example

```rust
use ipnetwork::IpNetwork;
use radixip::redis::{RedisCacheUpdate, RedisClient, RedisConfig};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let client = RedisClient::new(RedisConfig {
        url: "redis://127.0.0.1:6379".to_string(),
        pool_size: 10,
        connect_timeout: std::time::Duration::from_secs(5),
        max_retries: 3,
    })
    .await?;

    let prefix: IpNetwork = "172.16.0.0/16".parse()?;
    let update = RedisCacheUpdate::Insert {
        prefix,
        metadata: serde_json::json!({
            "value": "allow",
            "attributes": { "region": "eu-west" }
        }),
    };

    client.publish_json("radixip:updates", &update).await?;

    let raw = client.get_sync("radixip:lookup:172.16.0.10")?;
    println!("redis value: {:?}", raw);
    Ok(())
}
```

## Python example

```python
from radixip import RadixEngine

engine = RadixEngine(
    variant="standard",
    cache=True,
    max_entries=50000,
    ttl_seconds=3600,
    redis_url="redis://127.0.0.1:6379",
    redis_channel="radixip:updates",
)

engine.insert("10.0.0.0/8", {"value": "allow", "attributes": {"region": "us-east"}})
print(engine.lookup("10.0.0.42"))
```

## Node example

```javascript
const { RadixIP } = require('radixip');

const engine = new RadixIP({
  variant: 'concurrent',
  cache_enabled: true,
  cache_max_entries: 50000,
  cache_ttl_seconds: 3600,
  redis_url: 'redis://127.0.0.1:6379',
  redis_channel: 'radixip:updates',
});

engine.insert('10.0.0.0/8', { value: 'allow', attributes: { region: 'us-east' } });
console.log(engine.lookup('10.0.0.42'));
```

## Why this design matters

The cache stays local to each instance for maximum performance, but Redis acts as the shared propagation layer. That means a change made by one process is visible to the others without forcing every lookup to hit Redis directly.

The deep system reasoning for this design lives in [architecture.md](./architecture.md) and the broader guide set in this directory.
