module radixip-server

go 1.26.1

require (
	github.com/Mwangi-Derrick/radixip/lib/go/adapters/grpc-interceptor v0.0.0-00010101000000-000000000000
	github.com/Mwangi-Derrick/radixip/lib/go/engine v0.0.0-00010101000000-000000000000
	github.com/Mwangi-Derrick/radixip/proto/radixip v0.0.0-00010101000000-000000000000
	github.com/prometheus/client_golang v1.24.0
	google.golang.org/grpc v1.83.2
)

require (
	github.com/Mwangi-Derrick/radixip/lib/go/config v0.0.0-00010101000000-000000000000 // indirect
	github.com/Mwangi-Derrick/radixip/lib/go/policy v0.0.0-20260829024908-ab53302c83c8 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.0 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/redis/go-redis/v9 v9.21.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// Point to local paths
replace github.com/Mwangi-Derrick/radixip/lib/go/adapters/grpc-interceptor => ../../lib/go/adapters/interceptor

replace github.com/Mwangi-Derrick/radixip/lib/go/config => ../../lib/go/config

replace github.com/Mwangi-Derrick/radixip/lib/go/policy => ../../lib/go/policy

replace github.com/Mwangi-Derrick/radixip/lib/go/engine => ../../lib/go/engine

replace github.com/Mwangi-Derrick/radixip/proto/radixip => ../../proto/radixip/v1
