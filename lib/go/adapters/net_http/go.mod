module github.com/Mwangi-Derrick/radixip/lib/go/adapters/net_http

go 1.26.7

require (
	github.com/Mwangi-Derrick/radixip/lib/go/config v0.0.0-00010101000000-000000000000
	github.com/Mwangi-Derrick/radixip/lib/go/policy v0.0.0-20260829024908-ab53302c83c8
)

require (
	github.com/Mwangi-Derrick/radixip/lib/go/engine v0.0.0-20260909000955-faa0c37ce016 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/redis/go-redis/v9 v9.21.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/Mwangi-Derrick/radixip/lib/go/config => ../../config

replace github.com/Mwangi-Derrick/radixip/lib/go/policy => ../../policy
