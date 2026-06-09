package pgproto3

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5/internal/pgio"
)

// RequestHeaders ('M') is a frontend-only message carrying per-operation
// key/value metadata --- e.g. W3C trace context --- to be applied by the
// server at the next Query / Parse / Bind / Execute boundary.
//
// Wire format:
//
//	'M' (1 byte)
//	int32 message length (including the length field itself)
//	int16 entry count
//	for each entry:
//	    NUL-terminated key
//	    NUL-terminated value
//
// The frontend may send multiple M frames before the next P/B/E; the
// server's deferred-apply dispatcher walks them in receipt order.
//
// Server-side support is gated on the _pq_.headers=1 startup parameter
// being honored by the backend. Without the gate, sending M is a
// protocol violation and the server will close the connection.
//
// See the PostgreSQL "core: protocol headers" patch for the
// authoritative server-side definition.
type RequestHeaders struct {
	Headers []Header
}

// Header is one (key, value) pair carried by a RequestHeaders message.
// Both fields are UTF-8 / 7-bit-ASCII strings with no embedded NULs;
// the wire format uses NUL as the terminator.
type Header struct {
	Key   string
	Value string
}

// Frontend identifies RequestHeaders as sendable by a PostgreSQL frontend.
func (*RequestHeaders) Frontend() {}

// Decode decodes src into dst. src must contain the complete message
// body --- everything after the 1-byte type identifier and 4-byte
// length prefix.
func (dst *RequestHeaders) Decode(src []byte) error {
	if len(src) < 2 {
		return &invalidMessageFormatErr{messageType: "RequestHeaders"}
	}
	n := int(binaryBigEndianUint16(src[0:2]))
	rp := 2

	headers := make([]Header, n)
	for i := 0; i < n; i++ {
		kEnd := bytes.IndexByte(src[rp:], 0)
		if kEnd < 0 {
			return &invalidMessageFormatErr{messageType: "RequestHeaders"}
		}
		headers[i].Key = string(src[rp : rp+kEnd])
		rp += kEnd + 1
		vEnd := bytes.IndexByte(src[rp:], 0)
		if vEnd < 0 {
			return &invalidMessageFormatErr{messageType: "RequestHeaders"}
		}
		headers[i].Value = string(src[rp : rp+vEnd])
		rp += vEnd + 1
	}
	if rp != len(src) {
		// Trailing bytes after the declared entries: malformed frame.
		return &invalidMessageFormatErr{messageType: "RequestHeaders"}
	}
	dst.Headers = headers
	return nil
}

// Encode encodes src into dst. dst is appended-to and the result
// returned, including the 1-byte type identifier and 4-byte length.
func (src *RequestHeaders) Encode(dst []byte) ([]byte, error) {
	if len(src.Headers) > math.MaxUint16 {
		return nil, fmt.Errorf("RequestHeaders: %d entries exceeds wire limit of %d",
			len(src.Headers), math.MaxUint16)
	}
	// Reject embedded NULs --- the wire format uses NUL as the key/value
	// terminator. Catching this here gives the caller a meaningful Go
	// error rather than a malformed frame on the wire.
	for i, h := range src.Headers {
		if bytes.IndexByte([]byte(h.Key), 0) >= 0 {
			return nil, fmt.Errorf("RequestHeaders: entry %d key contains NUL", i)
		}
		if bytes.IndexByte([]byte(h.Value), 0) >= 0 {
			return nil, fmt.Errorf("RequestHeaders: entry %d value contains NUL", i)
		}
	}

	dst, sp := beginMessage(dst, 'M')
	dst = pgio.AppendUint16(dst, uint16(len(src.Headers)))
	for _, h := range src.Headers {
		dst = append(dst, h.Key...)
		dst = append(dst, 0)
		dst = append(dst, h.Value...)
		dst = append(dst, 0)
	}
	return finishMessage(dst, sp)
}

// MarshalJSON implements encoding/json.Marshaler.
func (src RequestHeaders) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type    string
		Headers []Header
	}{
		Type:    "RequestHeaders",
		Headers: src.Headers,
	})
}

// binaryBigEndianUint16 reads a big-endian uint16 without dragging in
// encoding/binary just for this one call site. Same convention as the
// pgio helpers used elsewhere in pgproto3.
func binaryBigEndianUint16(b []byte) uint16 {
	if len(b) < 2 {
		// Caller is responsible for length checks; defensive panic
		// would mask the real bug, so we silently zero-pad here.
		return 0
	}
	return uint16(b[0])<<8 | uint16(b[1])
}

// errInvalidRequestHeadersMessage is exported in case downstream
// callers want to type-assert against it. Currently unused by the
// pgproto3 package itself.
var errInvalidRequestHeadersMessage = errors.New("invalid RequestHeaders message")

var _ = errInvalidRequestHeadersMessage
