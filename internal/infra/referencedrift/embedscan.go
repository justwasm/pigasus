package referencedrift

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	fileSize = 48
	hashSize = 16
	maxName  = 4096
)

type section struct {
	name  string
	addr  uint64
	size  uint64
	bytes []byte
}

type image struct {
	path     string
	sections []section
}

func isMachO(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false
	}
	return binary.LittleEndian.Uint32(magic[:]) == 0xFEEDFACF ||
		binary.BigEndian.Uint32(magic[:]) == 0xFEEDFACF
}

func openImage(path string) (*image, error) {
	img := &image{path: path}
	if isMachO(path) {
		f, err := macho.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		for _, s := range f.Sections {
			data, err := s.Data()
			if err != nil {
				continue
			}
			img.sections = append(img.sections, section{
				name:  s.Name,
				addr:  s.Addr,
				size:  uint64(len(data)),
				bytes: data,
			})
		}
		return img, nil
	}

	f, err := elf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	for _, s := range f.Sections {
		if s.Type != elf.SHT_PROGBITS && s.Type != elf.SHT_NOBITS {
			continue
		}
		data, err := s.Data()
		if err != nil {
			continue
		}
		img.sections = append(img.sections, section{
			name:  s.Name,
			addr:  s.Addr,
			size:  uint64(len(data)),
			bytes: data,
		})
	}
	return img, nil
}

func (img *image) readAt(vaddr uint64, n int) ([]byte, bool) {
	for _, s := range img.sections {
		if len(s.bytes) == 0 {
			continue
		}
		if vaddr < s.addr || vaddr >= s.addr+s.size {
			continue
		}
		off := vaddr - s.addr
		if off+uint64(n) > s.size {
			return nil, false
		}
		return s.bytes[off : off+uint64(n)], true
	}
	return nil, false
}

type embedEntry struct {
	name string
	data []byte
	hash [hashSize]byte
	dir  bool
}

func (img *image) scan() []embedEntry {
	var out []embedEntry
	for _, s := range img.sections {
		if len(s.bytes) < fileSize {
			continue
		}
		for off := 0; off+fileSize <= len(s.bytes); off += 8 {
			e, ok := img.readRecord(s, off)
			if !ok {
				continue
			}
			out = append(out, e)
		}
	}
	return out
}

func (img *image) readRecord(s section, off int) (embedEntry, bool) {
	b := s.bytes[off : off+fileSize]
	namePtr := binary.LittleEndian.Uint64(b[0:8])
	nameLen := binary.LittleEndian.Uint64(b[8:16])
	dataPtr := binary.LittleEndian.Uint64(b[16:24])
	dataLen := binary.LittleEndian.Uint64(b[24:32])

	if nameLen == 0 || nameLen > maxName {
		return embedEntry{}, false
	}
	nameBytes, ok := img.readAt(namePtr, int(nameLen))
	if !ok || !printable(nameBytes) {
		return embedEntry{}, false
	}
	name := string(nameBytes)
	if !plausibleName(name) {
		return embedEntry{}, false
	}

	var data []byte
	if dataLen > 0 {
		data, ok = img.readAt(dataPtr, int(dataLen))
		if !ok {
			return embedEntry{}, false
		}
	} else {
		data = []byte{}
	}

	want := b[32 : 32+hashSize]
	if strings.HasSuffix(name, "/") {
		if dataLen != 0 || dataPtr != 0 || !isZeroHash(want) {
			return embedEntry{}, false
		}
		return embedEntry{name: name, dir: true}, true
	}
	hashes := embedHashes(data)
	if !bytes.Equal(want, hashes[0]) && !bytes.Equal(want, hashes[1]) {
		return embedEntry{}, false
	}
	var h [hashSize]byte
	copy(h[:], want)
	return embedEntry{name: name, data: data, hash: h}, true
}

func embedHashes(data []byte) [][]byte {
	small := sha256.Sum256(data)
	small[0] ^= 0xff

	h := sha256.New()
	h.Write([]byte{1})
	h.Write(data)
	large := h.Sum(nil)

	return [][]byte{small[:hashSize], large[:hashSize]}
}

func isZeroHash(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

func printable(b []byte) bool {
	for _, c := range b {
		if c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

func plausibleName(name string) bool {
	if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return false
	}
	if strings.ContainsAny(name, " \t\n\r") {
		return false
	}
	return true
}

// findZIPArchives counts embedded ZIP archives inside path.
func findZIPArchives(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	size := int64(len(data))
	sig := []byte{0x50, 0x4b, 0x05, 0x06}

	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	for idx := 0; idx+22 <= len(data); {
		pos := bytes.Index(data[idx:], sig)
		if pos == -1 {
			break
		}
		off := int64(idx + pos)
		if off+22 > size {
			break
		}
		rec := data[off : off+22]
		disk := binary.LittleEndian.Uint16(rec[4:6])
		cdDisk := binary.LittleEndian.Uint16(rec[6:8])
		nEntries := binary.LittleEndian.Uint16(rec[10:12])
		cdSize := int64(binary.LittleEndian.Uint32(rec[12:16]))
		cdOffset := int64(binary.LittleEndian.Uint32(rec[16:20]))
		commentLen := int64(binary.LittleEndian.Uint16(rec[20:22]))

		if disk != 0 || cdDisk != 0 || nEntries == 0 || cdSize == 0 || cdOffset == 0 {
			idx = int(off) + 4
			continue
		}
		start := off - cdSize - cdOffset
		length := off + 22 + commentLen - start
		if start < 0 || length <= 0 || off+22+commentLen > size {
			idx = int(off) + 4
			continue
		}
		sr := io.NewSectionReader(f, start, length)
		if _, err := zip.NewReader(sr, length); err == nil {
			count++
			idx = int(off + 22 + commentLen)
			continue
		}
		idx = int(off) + 4
	}
	return count, nil
}

// ScanEmbeddedAssets scans a binary and returns the count of embed.FS records,
// the total bytes in files, and the count of embedded ZIP archives.
func ScanEmbeddedAssets(binPath string) (count int, totalBytes int64, zipCount int, err error) {
	img, err := openImage(binPath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("open image %s: %w", binPath, err)
	}
	entries := img.scan()
	var total int64
	for _, e := range entries {
		if !e.dir {
			total += int64(len(e.data))
		}
	}
	zips, _ := findZIPArchives(binPath)
	return len(entries), total, zips, nil
}
