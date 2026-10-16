module github.com/mazrean/odjson/tools

go 1.27

tool (
	github.com/mazrean/odjson/tools/apicompat
	github.com/mazrean/odjson/tools/lint
)

require (
	golang.org/x/exp v0.0.0-20261014143641-f0251eb4aa3e
	golang.org/x/tools v0.52.0
	honnef.co/go/tools v0.8.1
)

require (
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
)
