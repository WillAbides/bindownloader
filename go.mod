module github.com/willabides/bindown/v3

go 1.14

require (
	github.com/alecthomas/kong v0.2.3
	github.com/andybalholm/brotli v1.0.0 // indirect
	github.com/frankban/quicktest v1.4.2 // indirect
	github.com/mholt/archiver/v3 v3.3.0
	github.com/pierrec/lz4 v2.3.0+incompatible // indirect
	github.com/stretchr/testify v1.4.0
	github.com/udhos/equalfile v0.3.0
	gopkg.in/yaml.v2 v2.2.4
)

replace github.com/alecthomas/kong => github.com/willabides/kong v0.2.3-0.20200313223825-65cdca836316
