package radixip

// RadixConfig holds the runtime configuration for RadixIP
type RadixConfig struct {
	EngineVariant    EngineVariant
	NodeVariant      NodeVariant
	CacheEnabled     bool
	CacheMaxEntries  int
	CacheTTLSeconds  *uint64
	Redis            *RedisConfig // nil if Redis is not enabled
	RedisChannel     string
	NumShards        *int
	EnableStats      bool
	
	// Split Plane Architecture Config
	EnableSplitPlane bool
	WriteCompressed  bool // true = CompressedTree, false = UncompressedTree
	ReadCompressed   bool // true = CompressedTree, false = UncompressedTree
}

func ptrUint64(v uint64) *uint64 {
	return &v
}

// Default creates a new RadixConfig with default values
func DefaultRadixConfig() *RadixConfig {
	return &RadixConfig{
		EngineVariant:    EngineConcurrent,
		NodeVariant:      NodeAtomic,
		CacheEnabled:     true,
		CacheMaxEntries:  10000,
		CacheTTLSeconds:  ptrUint64(3600),
		Redis:            nil,
		RedisChannel:     "radixip:updates",
		NumShards:        nil,
		EnableStats:      true,
		EnableSplitPlane: false,
		WriteCompressed:  false, // Control plane defaults to uncompressed
		ReadCompressed:   true,  // Data plane defaults to compressed
	}
}

// New creates a new RadixConfig with default values
func NewRadixConfig() *RadixConfig {
	return DefaultRadixConfig()
}
