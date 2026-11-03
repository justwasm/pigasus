package convstore

import (
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// Google-internal protobuf messages are decoded structurally: the wire format
// is self-describing enough to walk a message without its .proto, which is the
// only reason the reference's step payloads can be read at all. Field numbers
// and nesting were recovered by inspecting real conversation databases (see
// docs/cycles/cycle-120).
//
// Only the two wire types that occur in these messages are kept as values
// (varint and length-delimited); anything else is walked over.

type protoField struct {
	num  int
	wire int
	u    uint64
	data []byte
}

// protoFields decodes the top-level fields of b, stopping at the first
// malformed field. Truncation is normal here — several of the reference's
// payload fields are opaque blobs — so a short result is not an error.
//
// The wire format itself is decoded by protowire, the protobuf runtime's
// own codec; the walk below only says which fields to keep.
func protoFields(b []byte) []protoField {
	var out []protoField
	for i := 0; i < len(b); {
		num, wire, n := protowire.ConsumeTag(b[i:])
		if n < 0 {
			return out
		}
		i += n
		switch wire {
		case protowire.VarintType:
			v, m := protowire.ConsumeVarint(b[i:])
			if m < 0 {
				return out
			}
			i += m
			out = append(out, protoField{num: int(num), wire: 0, u: v})
		case protowire.BytesType:
			v, m := protowire.ConsumeBytes(b[i:])
			if m < 0 {
				return out
			}
			i += m
			out = append(out, protoField{num: int(num), wire: 2, data: v})
		case protowire.Fixed64Type, protowire.Fixed32Type:
			m := protowire.ConsumeFieldValue(num, wire, b[i:])
			if m < 0 {
				return out
			}
			i += m
		default:
			return out
		}
	}
	return out
}

// protoBytes returns the first length-delimited field with the given number.
func protoBytes(fs []protoField, num int) []byte {
	for _, f := range fs {
		if f.num == num && f.wire == 2 {
			return f.data
		}
	}
	return nil
}

// protoText returns the first length-delimited field with the given number as
// a string.
func protoText(fs []protoField, num int) string {
	if b := protoBytes(fs, num); b != nil {
		return string(b)
	}
	return ""
}

// protoVarint returns the first varint field with the given number.
func protoVarint(fs []protoField, num int) uint64 {
	for _, f := range fs {
		if f.num == num && f.wire == 0 {
			return f.u
		}
	}
	return 0
}

// protoDuration reads a google.protobuf.Duration: field 1 is the seconds,
// field 2 the nanoseconds.
func protoDuration(fs []protoField) time.Duration {
	secs := int64(protoVarint(fs, 1))
	nanos := int64(protoVarint(fs, 2))
	return time.Duration(secs)*time.Second + time.Duration(nanos)
}
