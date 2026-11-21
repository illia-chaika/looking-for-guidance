package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestIconReachesTheExecutable compiles helper/windows.rc, has the Go linker put the object into a small Windows
// program, and finds each image of helper/assets/tray.ico in the program's resources the way Windows looks them up.
func TestIconReachesTheExecutable(t *testing.T) {
	res, err := readScript(filepath.Join("..", "..", "helper", "windows.rc"))
	if err != nil {
		t.Fatal(err)
	}
	obj, err := object(res)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, text := range map[string]string{
		"go.mod":                  "module iconcheck\n\ngo 1.21\n",
		"main.go":                 "package main\n\nfunc main() {}\n",
		"rsrc_windows_amd64.syso": string(obj),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	build := exec.Command("go", "build", "-o", "check.exe", ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	f, err := pe.Open(filepath.Join(dir, "check.exe"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rva := f.OptionalHeader.(*pe.OptionalHeader64).DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].VirtualAddress
	var rsrc []byte
	for _, s := range f.Sections {
		if s.VirtualAddress == rva {
			if rsrc, err = s.Data(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if rsrc == nil {
		t.Fatal("the program has no resources")
	}
	le := binary.LittleEndian
	// find returns where the directory entry with this ID points, or 0 with no such entry.
	find := func(dir, id uint32) uint32 {
		n := uint32(le.Uint16(rsrc[dir+12:])) + uint32(le.Uint16(rsrc[dir+14:]))
		for i := range n {
			if e := dir + 16 + 8*i; le.Uint32(rsrc[e:]) == id {
				return le.Uint32(rsrc[e+4:]) &^ (1 << 31)
			}
		}
		return 0
	}
	resource := func(typ, id uint16) []byte {
		t.Helper()
		ids := find(0, uint32(typ))
		langs := find(ids, uint32(id))
		if ids == 0 || langs == 0 {
			t.Fatalf("no resource %d of type %d", id, typ)
		}
		entry := le.Uint32(rsrc[langs+20:]) // the first language
		at, size := le.Uint32(rsrc[entry:])-rva, le.Uint32(rsrc[entry+4:])
		return rsrc[at : at+size]
	}

	ico, err := os.ReadFile(filepath.Join("..", "..", "helper", "assets", "tray.ico"))
	if err != nil {
		t.Fatal(err)
	}
	group := resource(rtGroupIcon, 1) // windows.rc: 1 ICON "assets/tray.ico"
	n := int(le.Uint16(ico[4:]))
	if !bytes.Equal(group[:6], ico[:6]) || len(group) != 6+14*n {
		t.Fatalf("the icon group does not list the %d images of tray.ico", n)
	}
	for i := range n {
		want, got := ico[6+16*i:], group[6+14*i:]
		if !bytes.Equal(got[:12], want[:12]) {
			t.Errorf("image %d: the group describes it as % x, tray.ico as % x", i, got[:12], want[:12])
		}
		size, offset := le.Uint32(want[8:]), le.Uint32(want[12:])
		if !bytes.Equal(resource(rtIcon, le.Uint16(got[12:])), ico[offset:offset+size]) {
			t.Errorf("image %d differs from tray.ico", i)
		}
	}
}
