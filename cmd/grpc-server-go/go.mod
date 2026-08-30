module radixip-server

go 1.26.1

require (
	github.com/Mwangi-Derrick/radixip/lib/go/engine v0.0.0-00010101000000-000000000000
	github.com/Mwangi-Derrick/radixip/proto/radixip v0.0.0-00010101000000-000000000000
	github.com/prometheus/client_golang v1.24.0
	google.golang.org/grpc v1.82.1
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.0 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/redis/go-redis/v9 v9.21.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

// Point to local paths
replace github.com/Mwangi-Derrick/radixip/lib/go/engine => ../../lib/go/engine

replace github.com/Mwangi-Derrick/radixip/proto/radixip => ../../proto/radixip/v1
