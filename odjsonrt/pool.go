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

// Buffer is a pooled scratch buffer handed out by [GetBuffer].
//
// It exists so that returning a buffer to the pool costs nothing: putting a
// []byte into a sync.Pool boxes the slice header on every call, while putting
// back the same *Buffer that was taken out does not allocate at all.
type Buffer struct {
	// B is the scratch space. Callers reslice it and store the result back
	// before releasing the buffer.
	B []byte
}

var bufferRefPool sync.Pool

// GetBuffer returns a buffer whose B field has spare capacity, sized by
// whatever the previous user of that buffer needed.
func GetBuffer() *Buffer {
	if v := bufferRefPool.Get(); v != nil {
		b := v.(*Buffer)
		b.B = b.B[:0]
		return b
	}
	return &Buffer{B: make([]byte, 0, defaultBufferSize)}
}

// PutBuffer returns a buffer obtained from [GetBuffer]. Oversized buffers are
// dropped rather than retained.
func PutBuffer(b *Buffer) {
	if cap(b.B) > maxPooledBufferSize {
		return
	}
	b.B = b.B[:0]
	bufferRefPool.Put(b)
}
