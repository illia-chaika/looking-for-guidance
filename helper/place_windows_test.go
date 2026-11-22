package main

import (
	"archive/zip"
	"bytes"
	"debug/pe"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
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
	direct, err := os.ReadFile(filepath.Join("..", "dist", "looking-for-guidance-Windows.exe"))
	if err != nil {
		t.Fatal(err)
	}
	unpacked, err := os.ReadFile(exe)
	if err != nil || !bytes.Equal(direct, unpacked) {
		t.Fatalf("the standalone and update executables differ: %v", err)
	}
	module, err := windows.LoadLibraryEx(exe, 0, windows.LOAD_LIBRARY_AS_DATAFILE)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.FreeLibrary(module)
	if _, err := windows.FindResource(module, windows.ResourceID(1), windows.RT_GROUP_ICON); err != nil {
		t.Fatalf("the release has no Explorer icon: %v", err)
	}
	info, err := windows.FindResource(module, windows.ResourceID(1), windows.RT_RCDATA)
	if err != nil {
		t.Fatal(err)
	}
	notices, err := windows.LoadResourceData(module, info)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	entries, err := fs.ReadDir(z, ".")
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	for _, entry := range entries {
		if entry.Name() == appName {
			continue
		}
		data, err := fs.ReadFile(z, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&want, "%s\n\n%s\n\n", entry.Name(), data)
	}
	if want.Len() == 0 || !bytes.Equal(notices, want.Bytes()) {
		t.Fatal("embedded notices differ from the licences distributed in the ZIP")
	}
}
