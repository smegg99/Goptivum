module github.com/smegg99/goptivum/solveclient

go 1.27

require (
	github.com/smegg99/arrango v0.0.0
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12
)

require (
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

// Arrango is unpublished. Local checkout until it is tagged; this must not ship.
replace github.com/smegg99/arrango => ../../Arrango
