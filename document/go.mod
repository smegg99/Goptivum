module github.com/smegg99/goptivum/document

go 1.27

require (
	filippo.io/age v1.3.2
	github.com/klauspost/compress v1.20.0
	github.com/smegg99/arrango v0.0.0
	google.golang.org/protobuf v1.36.12
)

require (
	filippo.io/hpke v0.4.0 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

// Arrango is unpublished. Local checkout until it is tagged; this must not ship.
replace github.com/smegg99/arrango => ../../Arrango
