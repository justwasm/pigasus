package convstore

// Google-internal protobuf messages are decoded structurally: the wire format
// is self-describing enough to walk a message without its .proto, which is the
// only reason the reference's step payloads can be read at all. Field numbers
// and nesting were recovered by inspecting real conversation databases (see
// docs/cycles/cycle-120).
//
// Only the two wire types that occur in these messages are implemented
// (varint and length-delimited); anything else stops the walk.

type protoField struct {
	num  int
	wire int
	u    uint64
	data []byte
}

// pbVarint decodes a protobuf varint: 7-bit groups, least significant first.
// This is deliberately NOT readVarint from sqlite.go — SQLite's varints are
// big-endian, protobuf's are little-endian, and sharing the decoder silently
// mis-reads every multi-byte field number.
func pbVarint(b []byte, off int) (uint64, int) {
	var v uint64
	for i := 0; i < 10; i++ {
		if off+i >= len(b) {
			return 0, 0
		}
		c := b[off+i]
		v |= uint64(c&0x7f) << (7 * uint(i))
		if c < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}

// protoFields decodes the top-level fields of b, stopping at the first
// malformed field. Truncation is normal here — several of the reference's
// payload fields are opaque blobs — so a short result is not an error.
func protoFields(b []byte) []protoField {
	var out []protoField
	for i := 0; i < len(b); {
		key, n := pbVarint(b, i)
		if n == 0 || key == 0 {
			return out
		}
		i += n
		num, wire := int(key>>3), int(key&7)
		switch wire {
		case 0:
			v, m := pbVarint(b, i)
			if m == 0 {
				return out
			}
			i += m
			out = append(out, protoField{num: num, wire: 0, u: v})
		case 2:
			ln, m := pbVarint(b, i)
			if m == 0 || i+m+int(ln) > len(b) {
				return out
			}
			i += m
			out = append(out, protoField{num: num, wire: 2, data: b[i : i+int(ln)]})
			i += int(ln)
		case 1:
			if i+8 > len(b) {
				return out
			}
			i += 8
		case 5:
			if i+4 > len(b) {
				return out
			}
			i += 4
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
