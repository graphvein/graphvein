package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestTargets pins the release matrix: six systems, each once.
func TestTargets(t *testing.T) {
	want := []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"}
	if !reflect.DeepEqual(Targets, want) {
		t.Fatalf("Targets = %v, want %v", Targets, want)
	}
}

// fakeRelease packs a fake binary per target into a new folder.
func fakeRelease(t *testing.T, root string) (string, []string) {
	t.Helper()
	var args []string
	for _, target := range Targets {
		bin := filepath.Join(root, "bin", strings.ReplaceAll(target, "/", "-"))
		if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, []byte("binary for "+target), 0o755); err != nil {
			t.Fatal(err)
		}
		args = append(args, target+"="+bin)
	}
	out := t.TempDir()
	names, err := Pack(Options{Version: "v0.1.0", Date: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC), Out: out, Root: root}, args)
	if err != nil {
		t.Fatal(err)
	}
	return out, names
}

func fakeRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range ExtraFiles {
		if err := os.WriteFile(filepath.Join(root, f), []byte(f+" text\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestPack checks the six archives, their contents and modes, and that
// SHA256SUMS lists each archive once with its digest.
func TestPack(t *testing.T) {
	root := fakeRoot(t)
	out, names := fakeRelease(t, root)
	want := []string{
		"graphvein-v0.1.0-darwin-amd64.tar.gz",
		"graphvein-v0.1.0-darwin-arm64.tar.gz",
		"graphvein-v0.1.0-linux-amd64.tar.gz",
		"graphvein-v0.1.0-linux-arm64.tar.gz",
		"graphvein-v0.1.0-windows-amd64.zip",
		"graphvein-v0.1.0-windows-arm64.zip",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("archives = %v, want %v", names, want)
	}
	sums, err := os.ReadFile(filepath.Join(out, SumsFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n")
	if len(lines) != len(want) {
		t.Fatalf("SHA256SUMS has %d lines:\n%s", len(lines), sums)
	}
	for i, line := range lines {
		sum, name, ok := strings.Cut(line, "  ")
		if !ok || name != want[i] || len(sum) != 64 {
			t.Fatalf("SHA256SUMS line %d = %q", i+1, line)
		}
		data, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != sum {
			t.Errorf("%s: SHA256SUMS digest does not match", name)
		}
	}
	files, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(want)+1 {
		t.Errorf("output folder holds %d files, want %d archives and SHA256SUMS", len(files), len(want))
	}

	base := "graphvein-v0.1.0-linux-arm64/"
	got := readTarGz(t, filepath.Join(out, "graphvein-v0.1.0-linux-arm64.tar.gz"))
	wantTar := map[string]string{
		base:                         "dir 755",
		base + "graphvein":           "755 binary for linux/arm64",
		base + "LICENSE":             "644 LICENSE text\n",
		base + "NOTICE":              "644 NOTICE text\n",
		base + "THIRD_PARTY_NOTICES": "644 THIRD_PARTY_NOTICES text\n",
		base + "README.md":           "644 README.md text\n",
	}
	if !reflect.DeepEqual(got, wantTar) {
		t.Errorf("tar entries = %v, want %v", got, wantTar)
	}
	base = "graphvein-v0.1.0-windows-amd64/"
	got = readZip(t, filepath.Join(out, "graphvein-v0.1.0-windows-amd64.zip"))
	wantZip := map[string]string{
		base:                         "dir 755",
		base + "graphvein.exe":       "755 binary for windows/amd64",
		base + "LICENSE":             "644 LICENSE text\n",
		base + "NOTICE":              "644 NOTICE text\n",
		base + "THIRD_PARTY_NOTICES": "644 THIRD_PARTY_NOTICES text\n",
		base + "README.md":           "644 README.md text\n",
	}
	if !reflect.DeepEqual(got, wantZip) {
		t.Errorf("zip entries = %v, want %v", got, wantZip)
	}
}

// TestPackReproducible packs the same inputs twice: the bytes, and so
// SHA256SUMS, must be equal.
func TestPackReproducible(t *testing.T) {
	root := fakeRoot(t)
	first, _ := fakeRelease(t, root)
	second, _ := fakeRelease(t, root)
	a, err := os.ReadFile(filepath.Join(first, SumsFile))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(second, SumsFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("two packs differ:\n%s\n%s", a, b)
	}
}

// TestPackRefuses checks the inputs Pack rejects.
func TestPackRefuses(t *testing.T) {
	root := fakeRoot(t)
	bin := filepath.Join(root, "LICENSE")
	when := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		version string
		args    []string
	}{
		"bad version":     {"v0.1.0/x", []string{"linux/amd64=" + bin}},
		"no binaries":     {"v0.1.0", nil},
		"no target":       {"v0.1.0", []string{bin}},
		"duplicate":       {"v0.1.0", []string{"linux/amd64=" + bin, "linux/amd64=" + bin}},
		"missing binary":  {"v0.1.0", []string{"linux/amd64=" + filepath.Join(root, "absent")}},
		"empty arch part": {"v0.1.0", []string{"linux/=" + bin}},
	}
	for name, c := range cases {
		if _, err := Pack(Options{Version: c.version, Date: when, Out: t.TempDir(), Root: root}, c.args); err == nil {
			t.Errorf("%s: Pack succeeded", name)
		}
	}
	if err := os.Remove(filepath.Join(root, "NOTICE")); err != nil {
		t.Fatal(err)
	}
	if _, err := Pack(Options{Version: "v0.1.0", Date: when, Out: t.TempDir(), Root: root}, []string{"linux/amd64=" + bin}); err == nil {
		t.Error("Pack succeeded without NOTICE")
	}
}

func describe(mode os.FileMode, isDir bool, data []byte) string {
	if isDir {
		return fmt.Sprintf("dir %o", mode.Perm())
	}
	return fmt.Sprintf("%o %s", mode.Perm(), data)
}

func readTarGz(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	got := map[string]string{}
	var order []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" {
			t.Errorf("%s: owner %d/%d %q/%q", h.Name, h.Uid, h.Gid, h.Uname, h.Gname)
		}
		if !h.ModTime.Equal(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)) {
			t.Errorf("%s: time %v", h.Name, h.ModTime)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got[h.Name] = describe(os.FileMode(h.Mode), h.Typeflag == tar.TypeDir, data)
		order = append(order, h.Name)
	}
	if len(order) == 0 || !strings.HasSuffix(order[0], "/") {
		t.Errorf("tar order = %v, want the folder first", order)
	}
	return got
}

func readZip(t *testing.T, path string) map[string]string {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		got[f.Name] = describe(f.Mode(), f.Mode().IsDir(), data)
	}
	return got
}

// TestWriteSums collects the archives of a full pack in a new folder, as
// the release workflow does with the archives of its per-target jobs:
// SHA256SUMS must equal the one Pack wrote.
func TestWriteSums(t *testing.T) {
	root := fakeRoot(t)
	packed, names := fakeRelease(t, root)
	collected := t.TempDir()
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(packed, n))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(collected, n), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := WriteSums("v0.1.0", collected)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, names) {
		t.Errorf("WriteSums names = %v, want %v", got, names)
	}
	a, err := os.ReadFile(filepath.Join(packed, SumsFile))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(collected, SumsFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("SHA256SUMS differs:\nPack:\n%s\nWriteSums:\n%s", a, b)
	}

	// A second run finds SHA256SUMS itself, which is not an archive.
	if _, err := WriteSums("v0.1.0", collected); err == nil {
		t.Error("WriteSums accepted a folder that already holds SHA256SUMS")
	}
	if err := os.Remove(filepath.Join(collected, SumsFile)); err != nil {
		t.Fatal(err)
	}
	// Another version's names, a missing archive and a stray file fail.
	if _, err := WriteSums("v0.2.0", collected); err == nil {
		t.Error("WriteSums accepted archives of another version")
	}
	if err := os.WriteFile(filepath.Join(collected, "stray.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteSums("v0.1.0", collected); err == nil {
		t.Error("WriteSums accepted a stray file")
	}
	if err := os.Remove(filepath.Join(collected, "stray.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(collected, names[0])); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteSums("v0.1.0", collected); err == nil {
		t.Error("WriteSums accepted a missing archive")
	}
	if _, err := WriteSums("bad/version", collected); err == nil {
		t.Error("WriteSums accepted a bad version")
	}
}

// TestArchiveName pins the archive names the release workflow collects.
func TestArchiveName(t *testing.T) {
	if got := ArchiveName("v0.1.0", "windows", "arm64"); got != "graphvein-v0.1.0-windows-arm64.zip" {
		t.Errorf("windows: %s", got)
	}
	if got := ArchiveName("v0.1.0", "linux", "amd64"); got != "graphvein-v0.1.0-linux-amd64.tar.gz" {
		t.Errorf("linux: %s", got)
	}
}
