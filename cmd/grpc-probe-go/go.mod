module grpc-probe-go

go 1.26.1

require (
	github.com/Mwangi-Derrick/radixip/proto/radixip v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.83.2
)

replace github.com/Mwangi-Derrick/radixip/proto/radixip => ../../proto/radixip/v1
