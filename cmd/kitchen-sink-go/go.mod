module kitchen-sink-go

go 1.26.1

require (
	github.com/Mwangi-Derrick/radixip/lib/go/config v0.0.0-00010101000000-000000000000 // indirect
	github.com/Mwangi-Derrick/radixip/lib/go/policy v0.0.0-20260829024908-ab53302c83c8 // indirect
)

// Point to local paths

replace github.com/Mwangi-Derrick/radixip/lib/go/adapters/grpc-interceptor => ../../lib/go/adapters/interceptor

replace github.com/Mwangi-Derrick/radixip/lib/go/adapters/gin => ../../lib/go/adapters/gin

replace github.com/Mwangi-Derrick/radixip/lib/go/adapters/echo => ../../lib/go/adapters/echo

replace github.com/Mwangi-Derrick/radixip/lib/go/adapters/fiber => ../../lib/go/adapters/fiber

replace github.com/Mwangi-Derrick/radixip/lib/go/engine => ../../lib/go/engine
