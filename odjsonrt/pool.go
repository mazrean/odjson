package odjsonrt

import "sync"

// defaultBufferSize is the capacity of a freshly allocated encode buffer.
const defaultBufferSize = 512

// maxPooledBufferSize bounds the capacity of buffers that are kept around by
// the pool, so that a single huge document does not pin memory forever.
const maxPooledBufferSize = 1 << 20

var bufferPool sync.Pool

// AcquireBuffer returns an empty byte slice with spare capacity, taken from an
// internal pool. Generated MarshalJSON methods use it as the destination for
// the Append* helpers and hand it back with [ReleaseBuffer] once the encoded
// bytes have been copied out.
func AcquireBuffer() []byte {
	if v := bufferPool.Get(); v != nil {
		p := v.(*[]byte)
		b := *p
		return b[:0]
	}
	return make([]byte, 0, defaultBufferSize)
}

// ReleaseBuffer returns a buffer obtained from [AcquireBuffer] to the pool.
// The caller must not use b afterwards. Oversized buffers are dropped instead
// of being retained.
func ReleaseBuffer(b []byte) {
	if cap(b) == 0 || cap(b) > maxPooledBufferSize {
		return
	}
	b = b[:0]
	bufferPool.Put(&b)
}
