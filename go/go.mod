module github.com/bwplotka/sink/go/sink

go 1.22.6

require (
	github.com/nelkinda/health-go v0.0.1
	github.com/oklog/run v1.1.0
	github.com/planetscale/vtprotobuf v0.6.0
	github.com/prometheus/client_golang v1.20.6-0.20241021131810-bffa92259bd6
	github.com/stretchr/testify v1.9.0
	google.golang.org/protobuf v1.34.2
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/klauspost/compress v1.17.10 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nelkinda/http-go v0.0.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.60.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	golang.org/x/sys v0.25.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/prometheus/client_golang => ../../client_golang
)

