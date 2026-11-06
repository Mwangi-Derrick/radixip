package config

import "testing"

func TestXDPConfigDefaultsAndRedisOverrides(t *testing.T) {
	cfg, err := ParseYAML([]byte(`radixip:
  xdp:
    enabled: true
    redis:
      url: redis://cache.internal:6379
      channel: security:flagged
      snapshot_key: security:flagged:set
`))
	if err != nil {
		t.Fatal(err)
	}

	xdp := cfg.RadixIP.XDP
	if !xdp.Enabled || xdp.Interface != "eth0" || xdp.Mode != "skb" {
		t.Fatalf("unexpected XDP defaults: %+v", xdp)
	}
	if xdp.Redis.URL != "redis://cache.internal:6379" || xdp.Redis.Channel != "security:flagged" || xdp.Redis.SnapshotKey != "security:flagged:set" {
		t.Fatalf("unexpected XDP Redis config: %+v", xdp.Redis)
	}
}

func TestXDPConfigRejectsUnsupportedMode(t *testing.T) {
	_, err := ParseYAML([]byte(`radixip:
  xdp:
    enabled: true
    mode: offload
`))
	if err == nil {
		t.Fatal("expected unsupported XDP mode to be rejected")
	}
}
