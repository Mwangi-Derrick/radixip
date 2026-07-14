package go

// EngineVariant represents the type of engine to use
type EngineVariant string

const (
	EngineStandard    EngineVariant = "standard"
	EngineConcurrent  EngineVariant = "concurrent"
	EngineLockFree    EngineVariant = "lockfree"
	EngineAdaptive    EngineVariant = "adaptive"
)

// NodeVariant represents the type of node implementation
type NodeVariant string

const (
	NodeNormal   NodeVariant = "normal"
	NodeAtomic   NodeVariant = "atomic"
	NodeLockFree NodeVariant = "lockfree"
	NodePadded   NodeVariant = "padded"
)

