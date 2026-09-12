package odjsonrt

import "testing"

func TestCapHint(t *testing.T) {
	var h CapHint
	if got := CapFor[int](&h); got != minCap {
		t.Fatalf("zero hint: cap %d, want %d", got, minCap)
	}
	h.Record(2)
	if got := CapFor[int](&h); got != minCap {
		t.Fatalf("hint below minimum: cap %d, want %d", got, minCap)
	}
	h.Record(100)
	if got := CapFor[int](&h); got != 100 {
		t.Fatalf("after 100: cap %d, want 100", got)
	}
	// Shorter, but not by four times: the hint stands.
	h.Record(30)
	if got := CapFor[int](&h); got != 100 {
		t.Fatalf("after 30: cap %d, want 100", got)
	}
	// A quarter or less: the hint comes down to twice the length.
	h.Record(20)
	if got := CapFor[int](&h); got != 40 {
		t.Fatalf("after 20: cap %d, want 40", got)
	}
	h.Record(3)
	if got := CapFor[int](&h); got != 6 {
		t.Fatalf("after 3: cap %d, want 6", got)
	}
	h.Record(1)
	if got := CapFor[int](&h); got != minCap {
		t.Fatalf("after 1: cap %d, want %d", got, minCap)
	}
}

func TestCapHintBound(t *testing.T) {
	type big [1 << 16]byte
	var h CapHint
	h.Record(1 << 20)
	if got := CapFor[big](&h); got != maxCapBytes/(1<<16) {
		t.Fatalf("large elements: cap %d, want %d", got, maxCapBytes/(1<<16))
	}
	if got := CapFor[byte](&h); got != 1<<20 {
		t.Fatalf("byte elements: cap %d, want %d", got, 1<<20)
	}
	type huge [maxCapBytes * 2]byte
	if got := CapFor[huge](&h); got != minCap {
		t.Fatalf("element above the bound: cap %d, want %d", got, minCap)
	}
	if got := CapFor[struct{}](&h); got != 1<<20 {
		t.Fatalf("zero size elements: cap %d, want %d", got, 1<<20)
	}
}
