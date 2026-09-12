package odjsonrt

import "encoding/binary"

// load64 and load32 read a little-endian word at b[i:]. The exact extent
// matters: a sub-slice of eight bytes is one bounds test, where an open
// ended one is that test plus the one encoding/binary makes on the length
// of what it was handed, which the loop bound of a scan has already
// settled. In the word loops of the scanners that is the difference
// between one bounds test per word and two, and the word loops are where
// the decoder spends its time. Reading the word in place through unsafe
// measured the same as this on amd64, so the loads stay portable.
func load64(b []byte, i int) uint64 { return binary.LittleEndian.Uint64(b[i : i+8]) }
func load32(b []byte, i int) uint32 { return binary.LittleEndian.Uint32(b[i : i+4]) }
