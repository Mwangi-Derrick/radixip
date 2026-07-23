module radixip-server

go 1.26.1

require (
	github.com/lib/pq v1.10.9
	github.com/opensearch-project/opensearch-go/v3 v3.2.0
	github.com/redis/go-redis/v9 v9.21.0
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.57.0
	go.opentelemetry.io/otel v1.31.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.31.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.31.0
	go.opentelemetry.io/otel/sdk v1.31.0
	go.opentelemetry.io/otel/trace v1.31.0
	google.golang.org/grpc v1.67.0
	google.golang.org/protobuf v1.35.1
)

require (
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.5.0 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/uuid v1.4.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.31.0 // indirect
	github.com/pborman/uuid v1.2.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/redis/go-redis/v9 v9.21.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/net v0.35.0 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	golang.org/x/tools v0.34.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260214193041-cd1c82337326 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260214193041-cd1c82337326 // indirect
	golang.org/x/mod v0.25.0 // indirect
)

// Point to local paths
replace github.com/Mwangi-Derrick/radixip/lib/go => ../../lib/go

replace github.com/Mwangi-Derrick/radixip/proto/radixip => ../../proto/radixip/v1
