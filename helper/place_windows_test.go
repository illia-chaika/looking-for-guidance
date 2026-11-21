package main

import (
	"debug/pe"
	"path/filepath"
	"testing"
)

// TestBuiltZipUnpacks feeds the ZIP that tools/build-windows.sh makes to the updater's own unpacking. It runs when the
// ZIP is in dist/: put it there on a Windows machine and run `go test -run TestBuiltZipUnpacks ./helper`.
func TestBuiltZipUnpacks(t *testing.T) {
	zipPath := filepath.Join("..", "dist", releaseAsset)
	if !isFile(zipPath) {
		t.Skipf("%s is not built", zipPath)
	}
	exe, err := extractApp(zipPath, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f, err := pe.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	opt, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 || opt.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI {
		t.Fatalf("%s is not a 64-bit Windows program without a console", exe)
	}
	if opt.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE].Size == 0 {
		t.Fatalf("%s has no icon", exe)
	}
}
