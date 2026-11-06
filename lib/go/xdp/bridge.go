package xdp

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Bridge mirrors the existing Redis blocklist into the kernel maps.
// ART stays the source of truth; this is a second subscriber.
//
// ASSUMPTIONS (adapt to what RadixIP publishes today):
//   - channel carries "add <cidr>", "del <cidr>", or a bare "<cidr>" (= add)
//   - snapshot is a Redis SET of CIDRs / bare IPs
type Bridge struct {
	Redis       *redis.Client
	Loader      *Loader
	Channel     string // default "security:blocklist"
	SnapshotKey string // default "security:blocklist:set"
	sub         *redis.PubSub
}

// Start subscribes FIRST (so nothing published during seeding is lost), then
// seeds from the snapshot. Call before Loader.Attach. Errors are returned for
// logging; the caller attaches anyway unless fail_closed is set.
func (b *Bridge) Start(ctx context.Context) (seeded int, err error) {
	if b.Channel == "" {
		b.Channel = "security:blocklist"
	}
	if b.SnapshotKey == "" {
		b.SnapshotKey = "security:blocklist:set"
	}
	b.sub = b.Redis.Subscribe(ctx, b.Channel)
	if _, err = b.sub.Receive(ctx); err != nil {
		return 0, fmt.Errorf("subscribe: %w", err)
	}
	members, err := b.Redis.SMembers(ctx, b.SnapshotKey).Result()
	if err != nil {
		return 0, fmt.Errorf("snapshot: %w", err)
	}
	var prefixes []netip.Prefix
	for _, m := range members {
		p, perr := parsePrefix(m)
		if perr != nil {
			slog.Warn("seed skip", "entry", m, "err", perr)
			continue
		}
		prefixes = append(prefixes, p)
	}
	ok, errs := b.Loader.Seed(prefixes)
	for _, e := range errs {
		slog.Warn("seed rejected", "err", e)
	}
	return ok, nil
}

// Run applies live updates until ctx is done. Call after Attach.
func (b *Bridge) Run(ctx context.Context) error {
	if b.sub == nil {
		return fmt.Errorf("bridge: Start not called")
	}
	ch := b.sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return b.Close()
		case msg, ok := <-ch:
			if !ok {
				return fmt.Errorf("pubsub closed; map keeps enforcing without updates")
			}
			if err := b.apply(msg.Payload); err != nil {
				slog.Warn("ignored message", "payload", msg.Payload, "err", err)
			}
		}
	}
}

func (b *Bridge) Close() error {
	if b.sub == nil {
		return nil
	}
	return b.sub.Close()
}

func (b *Bridge) apply(payload string) error {
	f := strings.Fields(payload)
	if len(f) == 0 {
		return fmt.Errorf("empty message")
	}
	op, arg := "add", f[0]
	if f[0] == "add" || f[0] == "del" {
		if len(f) < 2 {
			return fmt.Errorf("missing cidr")
		}
		op, arg = f[0], f[1]
	}
	p, err := parsePrefix(arg)
	if err != nil {
		return err
	}
	if op == "del" {
		return b.Loader.Delete(p)
	}
	return b.Loader.Put(p)
}

func parsePrefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}
