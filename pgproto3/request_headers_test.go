package pgproto3_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgproto3"
)

func TestRequestHeadersRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		msg  pgproto3.RequestHeaders
	}{
		{
			name: "empty",
			msg:  pgproto3.RequestHeaders{Headers: []pgproto3.Header{}},
		},
		{
			name: "single",
			msg: pgproto3.RequestHeaders{Headers: []pgproto3.Header{
				{Key: "otel.traceparent", Value: "00-aabbccddeeff00112233445566778899-0123456789abcdef-01"},
			}},
		},
		{
			name: "multi",
			msg: pgproto3.RequestHeaders{Headers: []pgproto3.Header{
				{Key: "otel.traceparent", Value: "00-aabbccddeeff00112233445566778899-0123456789abcdef-01"},
				{Key: "otel.tracestate", Value: "vendor=value"},
				{Key: "app.user_id", Value: "42"},
			}},
		},
		{
			name: "long key + value",
			msg: pgproto3.RequestHeaders{Headers: []pgproto3.Header{
				{Key: strings.Repeat("k", 64), Value: strings.Repeat("v", 256)},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := tc.msg.Encode(nil)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			// Strip the 5-byte header (1-byte type + 4-byte length) ---
			// Decode operates on the body only.
			if len(encoded) < 5 {
				t.Fatalf("encoded too short: %d bytes", len(encoded))
			}
			if encoded[0] != 'M' {
				t.Fatalf("expected 'M' type identifier, got %q", encoded[0])
			}
			body := encoded[5:]

			var decoded pgproto3.RequestHeaders
			if err := decoded.Decode(body); err != nil {
				t.Fatalf("Decode: %v", err)
			}
			// Normalize nil vs empty slice for reflect.DeepEqual.
			want := tc.msg.Headers
			if want == nil {
				want = []pgproto3.Header{}
			}
			got := decoded.Headers
			if got == nil {
				got = []pgproto3.Header{}
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("round-trip mismatch:\n want %#v\n  got %#v", want, got)
			}
		})
	}
}

func TestRequestHeadersEncodeRejectsEmbeddedNUL(t *testing.T) {
	t.Parallel()
	msg := pgproto3.RequestHeaders{Headers: []pgproto3.Header{
		{Key: "bad\x00key", Value: "v"},
	}}
	if _, err := msg.Encode(nil); err == nil {
		t.Fatalf("expected error for NUL in key, got nil")
	}
}

func TestRequestHeadersDecodeRejectsTrailingBytes(t *testing.T) {
	t.Parallel()
	// Build a valid 1-entry message body, then append a stray byte.
	body := []byte{0, 1, 'k', 0, 'v', 0, 'x'}
	var msg pgproto3.RequestHeaders
	if err := msg.Decode(body); err == nil {
		t.Fatalf("expected error for trailing bytes, got nil")
	}
}
