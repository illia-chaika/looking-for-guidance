// Command winres compiles the icons named in a Windows resource script into the COFF object that the Go linker puts
// into a Windows executable, so Explorer shows the app's icon:
//
//	go run ./tools/winres helper/windows.rc helper/resource_windows_amd64.syso
//
// It stands in for windres, which the original project ran on a Windows build machine and macOS lacks. It reads only
// the statement helper/windows.rc uses, `<id> ICON "<file.ico>"`, and stops at anything else. The output depends on
// nothing but the inputs.
package main

import (
	"bytes"
	"cmp"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	langEnUS    = 0x409
	// Missing from debug/pe.
	relAMD64Addr32NB = 3 // IMAGE_REL_AMD64_ADDR32NB: the target's address relative to the image base
	symClassStatic   = 3 // IMAGE_SYM_CLASS_STATIC
)

type resource struct {
	typ, id uint16
	data    []byte
}

func main() {
	if len(os.Args) != 3 {
		fail(errors.New("usage: go run ./tools/winres <script.rc> <output.syso>"))
	}
	res, err := readScript(os.Args[1])
	if err != nil {
		fail(err)
	}
	obj, err := object(res)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(os.Args[2], obj, 0o644); err != nil {
		fail(err)
	}
}

// readScript returns the script's icon groups and their images. File names are relative to the script.
func readScript(path string) ([]resource, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var res []resource
	images := 0
	for n, line := range strings.Split(string(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		var id uint16
		var file string
		if _, err := fmt.Sscanf(line, "%d ICON %q", &id, &file); err != nil {
			return nil, fmt.Errorf("%s:%d: only `<id> ICON \"<file>\"` is supported: %s", path, n+1, line)
		}
		ico, err := os.ReadFile(filepath.Join(filepath.Dir(path), file))
		if err != nil {
			return nil, err
		}
		group, icons, err := splitIcon(ico, uint16(images+1))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		images += len(icons)
		res = append(append(res, icons...), resource{rtGroupIcon, id, group})
	}
	return res, nil
}

// splitIcon cuts an .ico file into its images, numbered from first, and the group that lists them: the file's own
// directory with each image's file offset replaced by its number.
func splitIcon(ico []byte, first uint16) (group []byte, images []resource, err error) {
	le := binary.LittleEndian
	if len(ico) < 6 || le.Uint16(ico) != 0 || le.Uint16(ico[2:]) != 1 {
		return nil, nil, errors.New("not an icon file")
	}
	n := int(le.Uint16(ico[4:]))
	if len(ico) < 6+16*n {
		return nil, nil, errors.New("the icon file is cut short")
	}
	group = slices.Clone(ico[:6])
	for i := range n {
		entry := ico[6+16*i : 6+16*(i+1)]
		size, offset := uint64(le.Uint32(entry[8:])), uint64(le.Uint32(entry[12:]))
		if offset+size > uint64(len(ico)) {
			return nil, nil, errors.New("the icon file is cut short")
		}
		id := first + uint16(i)
		images = append(images, resource{rtIcon, id, ico[offset : offset+size]})
		group = le.AppendUint16(append(group, entry[:12]...), id)
	}
	return group, images, nil
}

// object lays the resources out as Windows reads them, a sorted directory of types, one of IDs per type and one of
// languages per ID, then the data entries and the data, and wraps that in a COFF object with one .rsrc section. A data
// entry holds its data's offset in the section, and a relocation has the linker add the section's address.
func object(res []resource) ([]byte, error) {
	slices.SortFunc(res, func(a, b resource) int { return cmp.Or(cmp.Compare(a.typ, b.typ), cmp.Compare(a.id, b.id)) })
	var types []uint16
	var counts []int
	for i, r := range res {
		if i > 0 && res[i-1].typ == r.typ && res[i-1].id == r.id {
			return nil, fmt.Errorf("resource type %d has ID %d twice", r.typ, r.id)
		}
		if i == 0 || res[i-1].typ != r.typ {
			types, counts = append(types, r.typ), append(counts, 0)
		}
		counts[len(counts)-1]++
	}

	dirSize := func(entries int) int { return 16 + 8*entries }
	off := dirSize(len(types))
	idDirs := make([]int, len(types))
	for i, n := range counts {
		idDirs[i], off = off, off+dirSize(n)
	}
	langDirs, entries, data := make([]int, len(res)), make([]int, len(res)), make([]int, len(res))
	for i := range res {
		langDirs[i], off = off, off+dirSize(1)
	}
	for i := range res {
		entries[i], off = off, off+16
	}
	for i, r := range res {
		off = (off + 7) &^ 7
		data[i], off = off, off+len(r.data)
	}

	sect := make([]byte, (off+7)&^7)
	le := binary.LittleEndian
	dir := func(at, entries int) { le.PutUint16(sect[at+14:], uint16(entries)) } // NumberOfIdEntries
	entry := func(at int, id uint16, target int, subdir bool) {
		le.PutUint32(sect[at:], uint32(id))
		v := uint32(target)
		if subdir {
			v |= 1 << 31
		}
		le.PutUint32(sect[at+4:], v)
	}
	dir(0, len(types))
	k := 0
	for i, typ := range types {
		entry(dirSize(i), typ, idDirs[i], true)
		dir(idDirs[i], counts[i])
		for j := range counts[i] {
			entry(idDirs[i]+dirSize(j), res[k].id, langDirs[k], true)
			dir(langDirs[k], 1)
			entry(langDirs[k]+dirSize(0), langEnUS, entries[k], false)
			k++
		}
	}
	relocs := make([]pe.Reloc, len(res))
	for i, r := range res {
		le.PutUint32(sect[entries[i]:], uint32(data[i]))
		le.PutUint32(sect[entries[i]+4:], uint32(len(r.data)))
		copy(sect[data[i]:], r.data)
		relocs[i] = pe.Reloc{VirtualAddress: uint32(entries[i]), SymbolTableIndex: 0, Type: relAMD64Addr32NB}
	}

	const headers = 20 + 40 // the file header and one section header
	relocsAt := headers + len(sect)
	symbolsAt := relocsAt + 10*len(relocs)
	name := [8]byte{'.', 'r', 's', 'r', 'c'}
	var out bytes.Buffer
	for _, v := range []any{
		pe.FileHeader{Machine: pe.IMAGE_FILE_MACHINE_AMD64, NumberOfSections: 1, PointerToSymbolTable: uint32(symbolsAt), NumberOfSymbols: 1},
		pe.SectionHeader32{Name: name, SizeOfRawData: uint32(len(sect)), PointerToRawData: headers, PointerToRelocations: uint32(relocsAt),
			NumberOfRelocations: uint16(len(relocs)), Characteristics: pe.IMAGE_SCN_CNT_INITIALIZED_DATA | pe.IMAGE_SCN_MEM_READ},
		sect,
		relocs,
		// The section's own symbol, which the relocations name. The Go linker knows it by class, type 0 and the dot.
		pe.COFFSymbol{Name: name, SectionNumber: 1, StorageClass: symClassStatic},
		uint32(4), // an empty string table: just its own length
	} {
		if err := binary.Write(&out, le, v); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "winres:", err)
	os.Exit(1)
}
