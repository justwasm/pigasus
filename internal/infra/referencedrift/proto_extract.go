package referencedrift

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// ExtractProtoDescriptor extracts a named FileDescriptorProto from a compiled binary.
func ExtractProtoDescriptor(binPath string, protoName string) (*descriptorpb.FileDescriptorProto, error) {
	data, err := os.ReadFile(binPath)
	if err != nil {
		return nil, fmt.Errorf("read binary %s: %w", binPath, err)
	}

	starts := findDescriptorStarts(data, protoName)
	if len(starts) == 0 {
		return nil, fmt.Errorf("descriptor %q not found in %s", protoName, binPath)
	}

	for _, start := range starts {
		end := walkDescriptorEnd(data, start)
		if end <= start {
			continue
		}
		var fd descriptorpb.FileDescriptorProto
		if err := proto.Unmarshal(data[start:end], &fd); err == nil && fd.GetName() == protoName {
			return &fd, nil
		}
	}

	return nil, fmt.Errorf("descriptor %q found but failed to unmarshal from %s", protoName, binPath)
}

func findDescriptorStarts(data []byte, name string) []int {
	target := []byte(name)
	var out []int
	idx := 0
	for {
		k := bytes.Index(data[idx:], target)
		if k < 0 {
			break
		}
		actualK := idx + k
		i := actualK - 1
		for i >= 0 && (data[i]&0x80 != 0) {
			i--
		}
		if i >= 1 && data[i-1] == 0x0a {
			ln, n := binary.Uvarint(data[i:])
			if n > 0 && int(ln) == len(target) && i+n == actualK {
				out = append(out, i-1)
			}
		}
		idx = actualK + 1
	}
	return out
}

func walkDescriptorEnd(data []byte, start int) int {
	n := len(data)
	i := start
	limit := 16 * 1024 * 1024
	for i < n && i-start < limit {
		tag, varLen := binary.Uvarint(data[i:])
		if varLen <= 0 {
			return i
		}
		field := tag >> 3
		wt := tag & 7
		if field == 0 {
			return i
		}
		j := i + varLen
		switch wt {
		case 0: // varint
			_, vLen := binary.Uvarint(data[j:])
			if vLen <= 0 {
				return i
			}
			j += vLen
		case 1: // 64-bit
			j += 8
		case 2: // length-delimited
			ln, lLen := binary.Uvarint(data[j:])
			if lLen <= 0 {
				return i
			}
			j += lLen + int(ln)
		case 5: // 32-bit
			j += 4
		default:
			return i
		}
		if j <= i || j > n {
			return i
		}
		i = j
	}
	return i
}
