// Package direct holds the same vendored payload types as package gen, with
// odjson's -direct functions generated at -escape-html=false.
//
// It exists to answer one question gen cannot. gen is generated with the
// default -escape-html, so its MarshalT writes odjsonrt.ModeV2HTML, while the
// json-v2 row beside it writes ModeV2: encoding/json/v2 does not escape '<',
// '>' and '&'. Those two rows produce different bytes, so their ratio is not
// the cost of the entry point -- it also carries the escaping. Turning
// -escape-html off here puts MarshalT on ModeV2, the exact bytes
// encoding/json/v2 writes, and TestDirectMatchesJSONV2 requires them to be
// equal. Only then does the gap between the two rows mean what it looks like.
//
// gen cannot simply be regenerated that way: its encoding-json rows are
// supposed to produce encoding/json's bytes, and they would stop.
//
// The package is deliberately out of the README chart and out of bench.yml.
// It answers a question about odjson's own API, not about where odjson sits
// among the host libraries, which is what the chart is for.
package direct

//go:generate go run github.com/mazrean/odjson -direct -escape-html=false
