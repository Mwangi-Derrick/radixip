module grpc-probe-go

go 1.27.2

require (
	github.com/Mwangi-Derrick/radixip/proto/radixip v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.83.2
)

require (
	github.com/go-logr/logr v1.4.4 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
)

replace github.com/Mwangi-Derrick/radixip/proto/radixip => ../../proto/radixip/v1
