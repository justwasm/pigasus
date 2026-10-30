// Package implicit / protobuf.go — a minimal protobuf writer and reader.
//
// The reference's implicit store holds google-internal messages
// (exa.jetski_cortex_pb.ImplicitTrajectory and friends) whose .proto the
// clone does not carry. The wire format is self-describing enough to write
// and read the handful of fields the store needs, so this file implements
// only what those messages use: varints and length-delimited fields.
//
// The reader keeps every field's raw bytes, so a field the clone does not
// understand can be written back unchanged when the trajectory is rewritten.
package implicit

// field is one decoded field of a protobuf message.
type field struct {
	// num is the field number.
	num int

	// wire is the wire type: 0 (varint), 1 (64-bit), 2 (length-delimited)
	// or 5 (32-bit).
	wire int

	// u is the value of a varint field.
	u uint64

	// data is the payload of a length-delimited field, or the raw bytes of
	// a fixed-width one.
	data []byte
}

// appendVarint appends a base-128 varint.
func appendVarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

// appendTag appends a field's tag.
func appendTag(dst []byte, num, wire int) []byte {
	return appendVarint(dst, uint64(num)<<3|uint64(wire))
}

// appendVarintField appends a varint field.
func appendVarintField(dst []byte, num int, v uint64) []byte {
	dst = appendTag(dst, num, 0)
	return appendVarint(dst, v)
}

// appendBoolField appends a bool field.
func appendBoolField(dst []byte, num int, v bool) []byte {
	if !v {
		return dst
	}
	return appendVarintField(dst, num, 1)
}

// appendBytesField appends a length-delimited field.
func appendBytesField(dst []byte, num int, b []byte) []byte {
	dst = appendTag(dst, num, 2)
	dst = appendVarint(dst, uint64(len(b)))
	return append(dst, b...)
}

// appendStringField appends a string field.
func appendStringField(dst []byte, num int, s string) []byte {
	return appendBytesField(dst, num, []byte(s))
}

// appendMessageField appends a nested message.
func appendMessageField(dst []byte, num int, msg []byte) []byte {
	return appendBytesField(dst, num, msg)
}

// decodeVarint decodes a base-128 varint at the head of b. It returns the
// value and the number of bytes read; a zero count means the input was
// truncated.
func decodeVarint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < 10; i++ {
		if i >= len(b) {
			return 0, 0
		}
		c := b[i]
		v |= uint64(c&0x7f) << (7 * uint(i))
		if c < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}

// parseFields decodes the top-level fields of b. Truncation is not an
// error: the walk stops at the first malformed field and returns what it
// decoded.
func parseFields(b []byte) []field {
	var out []field
	for i := 0; i < len(b); {
		key, n := decodeVarint(b[i:])
		if n == 0 || key == 0 {
			return out
		}
		i += n
		num, wire := int(key>>3), int(key&7)
		switch wire {
		case 0:
			v, m := decodeVarint(b[i:])
			if m == 0 {
				return out
			}
			i += m
			out = append(out, field{num: num, wire: wire, u: v})
		case 1:
			if i+8 > len(b) {
				return out
			}
			out = append(out, field{num: num, wire: wire, data: b[i : i+8]})
			i += 8
		case 2:
			ln, m := decodeVarint(b[i:])
			if m == 0 || i+m+int(ln) > len(b) {
				return out
			}
			i += m
			out = append(out, field{num: num, wire: wire, data: b[i : i+int(ln)]})
			i += int(ln)
		case 5:
			if i+4 > len(b) {
				return out
			}
			out = append(out, field{num: num, wire: wire, data: b[i : i+4]})
			i += 4
		default:
			// A group or an unknown wire type: the walk cannot continue.
			return out
		}
	}
	return out
}

// fieldBytes writes a decoded field back out, unchanged.
func fieldBytes(f field) []byte {
	switch f.wire {
	case 0:
		return appendVarintField(nil, f.num, f.u)
	case 1, 5:
		out := appendTag(nil, f.num, f.wire)
		return append(out, f.data...)
	default:
		return appendBytesField(nil, f.num, f.data)
	}
}

// firstString returns the payload of the first length-delimited field with
// the given number.
func firstString(fields []field, num int) (string, bool) {
	for _, f := range fields {
		if f.num == num && f.wire == 2 {
			return string(f.data), true
		}
	}
	return "", false
}

// firstMessage returns the payload of the first length-delimited field with
// the given number.
func firstMessage(fields []field, num int) ([]byte, bool) {
	for _, f := range fields {
		if f.num == num && f.wire == 2 {
			return f.data, true
		}
	}
	return nil, false
}

// firstVarint returns the value of the first varint field with the given
// number.
func firstVarint(fields []field, num int) (uint64, bool) {
	for _, f := range fields {
		if f.num == num && f.wire == 0 {
			return f.u, true
		}
	}
	return 0, false
}
