module github.com/mazrean/odjson

go 1.27

require (
	golang.org/x/exp v0.0.0-20260908205506-85c1c2202aba
	golang.org/x/tools v0.50.0
	honnef.co/go/tools v0.8.1
)

require (
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
)

tool (
	github.com/mazrean/odjson/tools/apicompat
	github.com/mazrean/odjson/tools/lint
)
