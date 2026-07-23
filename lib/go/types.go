package radixip

import (
	"encoding/json"
	"fmt"
	"net"
)

// IpNetwork represents a CIDR network (similar to ipnetwork::IpNetwork)
type IpNetwork struct {
	IP   net.IP
	Mask net.IPMask
}

// Metadata stores user payload at a terminal prefix.
// The string map keeps the ABI and Redis payloads simple while still allowing
// callers to attach labels such as "action", "asn", "country", or "reason".
type Metadata struct {
	Value      string            `json:"value"`
	Attributes map[string]string `json:"attributes"`
}

// NewMetadata creates a new Metadata instance with the given value.
func NewMetadata(value string) *Metadata {
	return &Metadata{
		Value:      value,
		Attributes: make(map[string]string),
	}
}

// WithAttribute adds a key-value attribute to the metadata.
// Returns the Metadata pointer for method chaining.
func (m *Metadata) WithAttribute(key, value string) *Metadata {
	m.Attributes[key] = value
	return m
}

// FromString creates a Metadata from a string value.
// This is the equivalent of the From<&str> and From<String> implementations.
func FromString(value string) *Metadata {
	return NewMetadata(value)
}

// SubnetRule represents a rule that maps a subnet prefix to metadata.
type SubnetRule struct {
	Prefix   *net.IPNet `json:"prefix"`
	Metadata *Metadata  `json:"metadata"`
}

// NewSubnetRule creates a new SubnetRule.
func NewSubnetRule(prefix *net.IPNet, metadata *Metadata) *SubnetRule {
	return &SubnetRule{
		Prefix:   prefix,
		Metadata: metadata,
	}
}

// UnmarshalJSON implements custom JSON unmarshaling for SubnetRule.
func (r *SubnetRule) UnmarshalJSON(data []byte) error {
	type Alias SubnetRule
	aux := &struct {
		Prefix string `json:"prefix"`
		*Alias
	}{
		Alias: (*Alias)(r),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	_, ipnet, err := net.ParseCIDR(aux.Prefix)
	if err != nil {
		return fmt.Errorf("invalid prefix: %w", err)
	}
	r.Prefix = ipnet
	return nil
}

// MarshalJSON implements custom JSON marshaling for SubnetRule.
func (r *SubnetRule) MarshalJSON() ([]byte, error) {
	type Alias SubnetRule
	return json.Marshal(&struct {
		Prefix string `json:"prefix"`
		*Alias
	}{
		Prefix: r.Prefix.String(),
		Alias:  (*Alias)(r),
	})
}

// EngineStats tracks performance statistics for the rule engine.
type EngineStats struct {
	Inserts  int `json:"inserts"`
	Lookups  int `json:"lookups"`
	Hits     int `json:"hits"`
	Misses   int `json:"misses"`
	Removals int `json:"removals"`
	Size     int `json:"size"`
}
