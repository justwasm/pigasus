// Command embedextract pulls the static assets that a Go binary carries
// inside it back out again.
//
// It is a research tool for this repository. The Antigravity IDE's
// `language_server` embeds two kinds of payload: the file systems built with
// //go:embed, and the web frontend it serves to its own UI, which is a ZIP
// archive stored verbatim in the binary. Neither can be read with the
// ordinary tooling, so this tool takes both apart.
//
// # //go:embed file systems
//
// `embed.FS` is
//
//	type FS struct { files *[]file }
//	type file struct {
//		name string
//		data string
//		hash [16]byte // truncated SHA256 hash
//	}
//
// so the compiler emits, for every embedded file, a 48-byte record (8+8+8+8+16
// on a 64-bit target): a string header for the name, a string header for the
// bytes, and the hash of the content.
//
// The hash is not a plain SHA-256. cmd/internal/hash has two variants and the
// compiler picks between them by size:
//
//	<= 1 KiB: hash.Sum32 - sha256(content) with the first byte flipped
//	>  1 KiB: hash.New32 - sha256(0x01 || content)
//
// Both are truncated to 16 bytes.
//
// The binaries here are stripped, so the symbol table cannot be used to find
// the array. Instead this tool scans every section for records that *verify*:
// the name must be printable, the two string headers must point at real bytes
// inside the image, and the hash of the referenced bytes must equal the
// stored hash. That last check makes a false positive essentially impossible -
// a 48-byte window has to hash correctly by accident.
//
// # ZIP archives
//
// A ZIP file stored inside another file is found through its end-of-central-
// directory record (`PK\x05\x06`). The record carries the size and the offset
// of the central directory, both relative to the start of the archive, so the
// archive's own start is `eocd - cdSize - cdOffset`. Every candidate is then
// opened with archive/zip, which rejects the false positives.
//
// # Usage
//
//	embedextract -bin <binary> -list
//	embedextract -bin <binary> -embed-out <dir> -zip-out <dir> -zip-index 1
//
// With -list nothing is written. -embed-out receives the //go:embed file
// systems, -zip-out the entries of the selected ZIP archive; when the binary
// holds more than one archive, -zip-index picks one of them (0-based, in
// file-offset order).
//
// It understands Mach-O and ELF; the reference binaries here are Mach-O arm64,
// but the CLI's Linux builds are ELF and the layout is identical.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// fileSize is the size of one embed.file record on a 64-bit target:
	// name string (16) + data string (16) + hash (16).
	fileSize = 48

	// hashSize is the size of the stored hash: sha256 truncated to 16 bytes.
	hashSize = 16

	// maxName is the longest embedded path this tool will consider. Real
	// embed paths are short; anything longer is a false positive.
	maxName = 4096
)

// section is one loadable chunk of the image.
type section struct {
	name  string
	addr  uint64
	off   uint64
	size  uint64
	bytes []byte
}

// image is the opened binary plus a vaddr -> bytes lookup.
type image struct {
	path     string
	sections []section
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
				continue // zero-fill sections have no file bytes
			}
			img.add(s.Name, s.Addr, uint64(s.Offset), data)
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
		img.add(s.Name, s.Addr, 0, data)
	}
	return img, nil
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
	// 0xFEEDFACF is Mach-O 64-bit in either byte order.
	return binary.LittleEndian.Uint32(magic[:]) == 0xFEEDFACF ||
		binary.BigEndian.Uint32(magic[:]) == 0xFEEDFACF
}

func (img *image) add(name string, addr, off uint64, data []byte) {
	img.sections = append(img.sections, section{name: name, addr: addr, off: off, size: uint64(len(data)), bytes: data})
}

// readAt returns n bytes at the virtual address vaddr, or false if the range
// is not backed by file bytes.
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

// entry is one recovered embed.file record.
type entry struct {
	name string
	data []byte
	hash [hashSize]byte
	addr uint64 // where the record itself lives
	dir  bool   // a directory entry: no content, zero hash
}

// scan walks every section looking for verifiable embed.file records.
func (img *image) scan(minSize int) []entry {
	var out []entry
	for _, s := range img.sections {
		if len(s.bytes) < fileSize {
			continue
		}
		for off := 0; off+fileSize <= len(s.bytes); off += 8 {
			e, ok := img.readRecord(s, off)
			if !ok {
				continue
			}
			if len(e.data) < minSize {
				continue
			}
			out = append(out, e)
		}
	}
	return out
}

// readRecord validates the 48 bytes at s[off:] as one embed.file record.
func (img *image) readRecord(s section, off int) (entry, bool) {
	b := s.bytes[off : off+fileSize]
	namePtr := binary.LittleEndian.Uint64(b[0:8])
	nameLen := binary.LittleEndian.Uint64(b[8:16])
	dataPtr := binary.LittleEndian.Uint64(b[16:24])
	dataLen := binary.LittleEndian.Uint64(b[24:32])

	if nameLen == 0 || nameLen > maxName {
		return entry{}, false
	}
	nameBytes, ok := img.readAt(namePtr, int(nameLen))
	if !ok || !printable(nameBytes) {
		return entry{}, false
	}
	name := string(nameBytes)
	if !plausibleName(name) {
		return entry{}, false
	}

	var data []byte
	if dataLen > 0 {
		data, ok = img.readAt(dataPtr, int(dataLen))
		if !ok {
			return entry{}, false
		}
	} else {
		data = []byte{}
	}

	want := b[32 : 32+hashSize]
	if strings.HasSuffix(name, "/") {
		// A directory entry carries no content and a zero hash.
		if dataLen != 0 || dataPtr != 0 || !isZeroHash(want) {
			return entry{}, false
		}
		return entry{name: name, dir: true, addr: s.addr + uint64(off)}, true
	}
	hashes := embedHashes(data)
	if !bytesEqual(want, hashes[0]) && !bytesEqual(want, hashes[1]) {
		return entry{}, false
	}
	var h [hashSize]byte
	copy(h[:], want)
	return entry{name: name, data: data, hash: h, addr: s.addr + uint64(off)}, true
}

// embedHashes returns the two hashes cmd/compile may have stored for data.
// Files of at most 1 KiB go through hash.Sum32 (a SHA-256 whose first byte is
// flipped); larger files go through hash.New32 (a SHA-256 of a 0x01 prefix
// followed by the content). Both are truncated to 16 bytes.
func embedHashes(data []byte) [][]byte {
	small := sha256.Sum256(data)
	small[0] ^= 0xff

	h := sha256.New()
	h.Write([]byte{1})
	h.Write(data)
	large := h.Sum(nil)

	return [][]byte{small[:hashSize], large[:hashSize]}
}

// isZeroHash reports whether b is 16 zero bytes - the marker the compiler
// writes for a directory entry.
func isZeroHash(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// printable reports whether b is plausible text.
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

// plausibleName filters out Go string constants that happen to hash into a
// matching record. An embedded path is a relative file name; it does not have
// to carry an extension (the JSON-schema metaschemas are embedded as
// "metaschemas/draft-04/schema"). The hash check downstream is what really
// decides, so this filter only has to throw away the obvious noise.
func plausibleName(name string) bool {
	if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return false
	}
	if strings.ContainsAny(name, " \t\n\r") {
		return false
	}
	return true
}

// archive is one ZIP file found inside the binary.
type archive struct {
	index   int
	offset  int64  // file offset of the archive's first byte
	length  int64  // its length in bytes
	digest  string // sha256 of the archive as stored
	entries []*zip.File
}

// findArchives looks for ZIP archives stored in the file. The end-of-central-
// directory record carries the size and the offset of the central directory,
// both relative to the start of the archive, so the archive's own start is
// eocd - cdSize - cdOffset.
func findArchives(path string) ([]archive, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()

	var out []archive
	const eocdSig = 0x06054b50
	for off := int64(0); off+22 <= size; {
		buf := make([]byte, 4)
		if _, err := f.ReadAt(buf, off); err != nil {
			break
		}
		if binary.LittleEndian.Uint32(buf) != eocdSig {
			off++
			continue
		}
		rec := make([]byte, 22)
		if _, err := f.ReadAt(rec, off); err != nil {
			break
		}
		disk := binary.LittleEndian.Uint16(rec[4:6])
		cdDisk := binary.LittleEndian.Uint16(rec[6:8])
		nEntries := binary.LittleEndian.Uint16(rec[10:12])
		cdSize := int64(binary.LittleEndian.Uint32(rec[12:16]))
		cdOffset := int64(binary.LittleEndian.Uint32(rec[16:20]))
		commentLen := int64(binary.LittleEndian.Uint16(rec[20:22]))

		if disk != 0 || cdDisk != 0 || nEntries == 0 || cdSize == 0 || cdOffset == 0 {
			off += 4
			continue
		}
		start := off - cdSize - cdOffset
		length := off + 22 + commentLen - start
		if start < 0 || length <= 0 || off+22+commentLen > size {
			off += 4
			continue
		}
		if a, ok := openArchive(f, start, length); ok {
			out = append(out, a)
			off = off + 22 + commentLen
			continue
		}
		off += 4
	}
	for i := range out {
		out[i].index = i
	}
	return out, nil
}

// openArchive opens the byte range as a ZIP file and returns nil when it is
// not one.
func openArchive(f *os.File, start, length int64) (archive, bool) {
	sr := io.NewSectionReader(f, start, length)
	r, err := zip.NewReader(sr, length)
	if err != nil {
		return archive{}, false
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, io.NewSectionReader(f, start, length)); err != nil {
		return archive{}, false
	}
	return archive{
		offset:  start,
		length:  length,
		digest:  fmt.Sprintf("%x", sum.Sum(nil)),
		entries: r.File,
	}, true
}

// writeFile writes data to dst, creating the parent directories.
func writeFile(dst string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// safeJoin joins a slash-separated archive path onto a directory, refusing
// anything that would escape it.
func safeJoin(dir, name string) (string, error) {
	clean := path.Clean("/" + strings.ReplaceAll(name, `\`, "/"))
	return filepath.Join(dir, filepath.FromSlash(clean)), nil
}

// extract writes every file of the archive under dir.
func (a archive) extract(path, dir string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r, err := zip.NewReader(io.NewSectionReader(f, a.offset, a.length), a.length)
	if err != nil {
		return err
	}
	for _, e := range r.File {
		if e.FileInfo().IsDir() {
			continue
		}
		rc, err := e.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		dst, err := safeJoin(dir, e.Name)
		if err != nil {
			return err
		}
		if err := writeFile(dst, data); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	var (
		binPath  = flag.String("bin", "", "the binary to unpack")
		list     = flag.Bool("list", false, "only list what was found")
		embedOut = flag.String("embed-out", "", "directory to write the //go:embed file systems into")
		zipOut   = flag.String("zip-out", "", "directory to write the embedded ZIP archives into")
		zipIndex = flag.Int("zip-index", -1, "which ZIP archive to write (0-based, in file-offset order)")
		minSize  = flag.Int("min-size", 0, "ignore embedded files smaller than this many bytes")
	)
	flag.Parse()

	if *binPath == "" {
		fmt.Fprintln(os.Stderr, "embedextract: -bin is required")
		os.Exit(2)
	}
	if !*list && *embedOut == "" && *zipOut == "" {
		fmt.Fprintln(os.Stderr, "embedextract: -embed-out or -zip-out is required unless -list is given")
		os.Exit(2)
	}

	// ── the //go:embed file systems ───────────────────────────────────────
	img, err := openImage(*binPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "embedextract:", err)
		os.Exit(1)
	}
	entries := img.scan(*minSize)
	sort.Slice(entries, func(i, j int) bool { return entries[i].addr < entries[j].addr })

	fmt.Printf("== //go:embed file systems: %d records\n", len(entries))
	embedTotal := 0
	for _, e := range entries {
		if e.dir {
			fmt.Printf("   %10s  %s/\n", "dir", e.name)
			continue
		}
		embedTotal += len(e.data)
		fmt.Printf("   %10d  %s  sha256=%x\n", len(e.data), e.name, e.hash[:8])
	}
	fmt.Printf("   total: %d files, %d bytes\n", len(entries), embedTotal)

	if *embedOut != "" {
		for _, e := range entries {
			if e.dir || len(e.data) == 0 {
				continue
			}
			dst, err := safeJoin(*embedOut, e.name)
			if err != nil {
				fmt.Fprintln(os.Stderr, "embedextract:", err)
				os.Exit(1)
			}
			if err := writeFile(dst, e.data); err != nil {
				fmt.Fprintln(os.Stderr, "embedextract:", err)
				os.Exit(1)
			}
		}
		fmt.Printf("   wrote %s\n", *embedOut)
	}

	// ── the embedded ZIP archives ─────────────────────────────────────────
	archives, err := findArchives(*binPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "embedextract:", err)
		os.Exit(1)
	}
	fmt.Printf("== embedded ZIP archives: %d\n", len(archives))
	for _, a := range archives {
		var raw, unz int64
		for _, e := range a.entries {
			raw += int64(e.CompressedSize64)
			unz += int64(e.UncompressedSize64)
		}
		first := ""
		if len(a.entries) > 0 {
			first = a.entries[0].Name
		}
		fmt.Printf("   [%d] offset=0x%x size=%d entries=%d (raw %d -> %d) sha256=%s first=%q\n",
			a.index, a.offset, a.length, len(a.entries), raw, unz, a.digest[:16], first)
	}

	if *zipOut != "" {
		if *zipIndex < 0 {
			if len(archives) != 1 {
				fmt.Fprintf(os.Stderr, "embedextract: %d archives found, -zip-index is required\n", len(archives))
				os.Exit(2)
			}
			*zipIndex = 0
		}
		if *zipIndex >= len(archives) {
			fmt.Fprintf(os.Stderr, "embedextract: no archive with index %d\n", *zipIndex)
			os.Exit(2)
		}
		a := archives[*zipIndex]
		if err := a.extract(*binPath, *zipOut); err != nil {
			fmt.Fprintln(os.Stderr, "embedextract:", err)
			os.Exit(1)
		}
		fmt.Printf("   wrote %s (archive %d)\n", *zipOut, a.index)
	}
}
