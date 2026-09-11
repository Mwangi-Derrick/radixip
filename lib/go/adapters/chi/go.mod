module github.com/Mwangi-Derrick/radixip/lib/go/adapters/chi

go 1.26.7

require (
	github.com/Mwangi-Derrick/radixip/lib/go/config v0.0.0-00010101000000-000000000000
	github.com/Mwangi-Derrick/radixip/lib/go/policy v0.0.0-20260829024908-ab53302c83c8
	github.com/go-chi/chi/v5 v5.3.2 // indirec
)


replace github.com/Mwangi-Derrick/radixip/lib/go/config => ../../config
replace github.com/Mwangi-Derrick/radixip/lib/go/policy => ../../policy
