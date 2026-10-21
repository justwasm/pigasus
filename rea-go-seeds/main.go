// pclnseeds recovers Go function names from a stripped binary's pclntab
// and writes a REA Ghidra seed file (address<TAB>function<TAB>name).
//
// Usage:
//
//	go run . /path/to/binary            # writes seeds.txt + funcs.txt next to the binary's directory
//	go run . /path/to/binary outdir     # explicit output directory
//
// Works with Mach-O (including Google-internal toolchain section names like
// __lrodata_gopcln), ELF (.gopclntab) and PE (.rdata heuristic via gosym on
// the pclntab section) without third-party dependencies.
package main

import (
	"debug/elf"
	"debug/gosym"
	"debug/macho"
	"debug/pe"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: pclnseeds <binary> [outdir]")
		os.Exit(2)
	}
	bin := os.Args[1]
	outdir := "."
	if len(os.Args) > 2 {
		outdir = os.Args[2]
	}

	pcln, text, err := loadPclntab(bin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	lt := gosym.NewLineTable(pcln, text)
	tab, err := gosym.NewTable(nil, lt)
	fmt.Fprintln(os.Stderr, "gosym debug: funcs", len(tab.Funcs), "err", err)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gosym:", err)
		os.Exit(1)
	}

	seen := map[uint64]bool{}
	type fn struct {
		entry uint64
		name  string
	}
	var fns []fn
	for _, f := range tab.Funcs {
		if seen[f.Entry] || f.Name == "" || strings.ContainsAny(f.Name, " \t\x00") || len(f.Name) > 2000 {
			continue
		}
		seen[f.Entry] = true
		fns = append(fns, fn{f.Entry, f.Name})
	}
	sort.Slice(fns, func(i, j int) bool { return fns[i].entry < fns[j].entry })

	seedsPath := filepath.Join(outdir, "seeds.txt")
	funcsPath := filepath.Join(outdir, "funcs.txt")
	sf, err := os.Create(seedsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ff, err := os.Create(funcsPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, f := range fns {
		fmt.Fprintf(sf, "0x%x\tfunction\t%s\n", f.entry, f.name)
		fmt.Fprintf(ff, "%x %s\n", f.entry, f.name)
	}
	sf.Close()
	ff.Close()
	fmt.Printf("recovered %d function names\nseeds: %s\nfuncs: %s\n", len(fns), seedsPath, funcsPath)
	fmt.Printf("\nnext: REA_GHIDRA_SEED_FILE=%s rea analyze %s --provider ghidra\n", seedsPath, bin)
}

// loadPclntab returns the pclntab bytes and the virtual address of the code
// section start (the base gosym needs for PC translation).
func loadPclntab(path string) ([]byte, uint64, error) {
	if f, err := macho.Open(path); err == nil {
		defer f.Close()
		var pcln []byte
		for _, name := range []string{"__gopclntab", "__lrodata_gopcln"} {
			if s := f.Section(name); s != nil {
				b, err := s.Data()
				if err != nil {
					return nil, 0, err
				}
				pcln = b
				break
			}
		}
		if pcln == nil {
			return nil, 0, fmt.Errorf("no pclntab section (tried __gopclntab, __lrodata_gopcln)")
		}
		s := f.Section("__text")
		if s == nil {
			return nil, 0, fmt.Errorf("no __text section")
		}
		return pcln, s.Addr, nil
	}
	if f, err := elf.Open(path); err == nil {
		defer f.Close()
		// Primary: .gopclntab section (upstream Go toolchain).
		// Fallback: pclntab magic embedded in .lrodata / .rodata / .data.ro /
		// .text — used by Google's internal Go toolchain where the linker
		// folds pclntab into .lrodata (the ELF analogue of Mach-O's
		// __lrodata_gopcln handled above).
		var pcln []byte
		var pclnVaddr uint64
		if s := f.Section(".gopclntab"); s != nil {
			b, err := s.Data()
			if err != nil {
				return nil, 0, err
			}
			pcln = b
			pclnVaddr = s.Addr
		} else {
			found := false
			for _, name := range []string{".lrodata", ".rodata", ".data.ro", ".text"} {
				s := f.Section(name)
				if s == nil {
					continue
				}
				b, err := s.Data()
				if err != nil || len(b) < 6 {
					continue
				}
				// pclntab magic (Go 1.20+): f1 ff ff ff 00 00
				if b[0] == 0xf1 && b[1] == 0xff && b[2] == 0xff && b[3] == 0xff && b[4] == 0x00 && b[5] == 0x00 {
					pcln = b
					pclnVaddr = s.Addr
					found = true
					break
				}
			}
			if !found {
				return nil, 0, fmt.Errorf("no .gopclntab section and no pclntab magic in .lrodata/.rodata/.data.ro/.text")
			}
		}
		t := f.Section(".text")
		if t == nil {
			return nil, 0, fmt.Errorf("no .text section")
		}
		_ = pclnVaddr // gosym needs the .text base, not the pclntab base
		return pcln, t.Addr, nil
	}
	if f, err := pe.Open(path); err == nil {
		defer f.Close()
		for _, s := range f.Sections {
			if s.Name == ".rdata" || s.Name == ".text" {
				b, err := s.Data()
				if err != nil {
					continue
				}
				if len(b) >= 4 && b[0] == 0xf1 && b[1] == 0xff && b[2] == 0xff && b[3] == 0xff {
					return b, uint64(s.VirtualAddress), nil
				}
			}
		}
		return nil, 0, fmt.Errorf("pclntab not found in PE sections")
	}
	return nil, 0, fmt.Errorf("unsupported binary format (not Mach-O/ELF/PE)")
}
