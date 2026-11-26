// Command release packs built graphvein binaries into the release archives
// and writes SHA256SUMS over them. scripts/release-archives.sh builds the
// binaries and runs it; the release workflow (.github/workflows/release.yml)
// runs that script on a tag.
//
//	go run ./scripts/release -list
//	go run ./scripts/release -version v0.1.0 -date 2026-11-20T10:00:00Z -out dist \
//	    linux/amd64=build/linux-amd64/graphvein windows/amd64=build/windows-amd64/graphvein.exe
//
// Each archive holds one folder, graphvein-VERSION-GOOS-GOARCH, with the
// binary, LICENSE, NOTICE, THIRD_PARTY_NOTICES and README.md from the
// repository root: .zip for Windows, .tar.gz for every other system. The
// archives are reproducible: entries are written in a fixed order with
// fixed modes, no owner, and the -date as their time (the release uses the
// tagged commit's time), so the same binaries give the same bytes.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Targets is the release build matrix: every GOOS/GOARCH pair a release
// ships a binary for. scripts/release-archives.sh reads it with -list, so
// the matrix is defined once and tested here.
var Targets = []string{
	"darwin/amd64",
	"darwin/arm64",
	"linux/amd64",
	"linux/arm64",
	"windows/amd64",
	"windows/arm64",
}

// ExtraFiles are the files next to the binary in every archive, relative to
// the repository root: the licence and notices must travel with the binary
// (ADR 0080), and the README says how to verify and run it.
var ExtraFiles = []string{"LICENSE", "NOTICE", "THIRD_PARTY_NOTICES", "README.md"}

// SumsFile is the checksum file written next to the archives, in the format
// `sha256sum -c` reads.
const SumsFile = "SHA256SUMS"

// versionRe keeps the version usable in a file name.
var versionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

func main() {
	list := flag.Bool("list", false, "print the release targets, one GOOS/GOARCH per line, and exit")
	version := flag.String("version", "", "release version, for example v0.1.0")
	date := flag.String("date", "", "time stamp of every archive entry (RFC 3339)")
	out := flag.String("out", "", "folder that receives the archives and SHA256SUMS")
	root := flag.String("root", ".", "repository root holding the extra files")
	flag.Parse()
	if *list {
		for _, t := range Targets {
			fmt.Println(t)
		}
		return
	}
	when, err := time.Parse(time.RFC3339, *date)
	if err != nil {
		slog.Error("release", "err", fmt.Errorf("parsing -date: %w", err))
		os.Exit(2)
	}
	names, err := Pack(Options{Version: *version, Date: when, Out: *out, Root: *root}, flag.Args())
	if err != nil {
		slog.Error("release", "err", err)
		os.Exit(1)
	}
	for _, n := range names {
		fmt.Println(filepath.Join(*out, n))
	}
}

// Options are the inputs every archive shares.
type Options struct {
	Version string
	Date    time.Time
	Out     string // folder for the archives and SHA256SUMS
	Root    string // repository root holding ExtraFiles
}

// Pack writes one archive per "GOOS/GOARCH=binary" argument into o.Out and
// SHA256SUMS over them, and returns the archive names in SHA256SUMS order.
func Pack(o Options, binaries []string) ([]string, error) {
	if !versionRe.MatchString(o.Version) {
		return nil, fmt.Errorf("version %q: use letters, digits and . _ + -", o.Version)
	}
	if o.Out == "" {
		return nil, errors.New("no output folder (-out)")
	}
	if len(binaries) == 0 {
		return nil, errors.New("no binaries to pack")
	}
	if err := os.MkdirAll(o.Out, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", o.Out, err)
	}
	seen := map[string]bool{}
	var names []string
	for _, arg := range binaries {
		target, bin, ok := strings.Cut(arg, "=")
		goos, goarch, okTarget := strings.Cut(target, "/")
		if !ok || !okTarget || goos == "" || goarch == "" || bin == "" {
			return nil, fmt.Errorf("argument %q: want GOOS/GOARCH=binary", arg)
		}
		if seen[target] {
			return nil, fmt.Errorf("target %s given twice", target)
		}
		seen[target] = true
		name, err := packOne(o, goos, goarch, bin)
		if err != nil {
			return nil, fmt.Errorf("packing %s: %w", target, err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var sums strings.Builder
	for _, n := range names {
		sum, err := fileSHA256(filepath.Join(o.Out, n))
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&sums, "%s  %s\n", sum, n)
	}
	if err := os.WriteFile(filepath.Join(o.Out, SumsFile), []byte(sums.String()), 0o644); err != nil {
		return nil, fmt.Errorf("writing %s: %w", SumsFile, err)
	}
	return names, nil
}

// entry is one file of an archive, in archive order.
type entry struct {
	name string // path inside the archive folder
	src  string
	mode os.FileMode
}

func packOne(o Options, goos, goarch, bin string) (string, error) {
	base := fmt.Sprintf("graphvein-%s-%s-%s", o.Version, goos, goarch)
	exe := "graphvein"
	if goos == "windows" {
		exe += ".exe"
	}
	entries := []entry{{name: exe, src: bin, mode: 0o755}}
	for _, f := range ExtraFiles {
		entries = append(entries, entry{name: f, src: filepath.Join(o.Root, f), mode: 0o644})
	}
	for _, e := range entries {
		if fi, err := os.Stat(e.src); err != nil {
			return "", fmt.Errorf("reading %s: %w", e.src, err)
		} else if !fi.Mode().IsRegular() {
			return "", fmt.Errorf("%s is not a regular file", e.src)
		}
	}
	if goos == "windows" {
		name := base + ".zip"
		return name, writeFile(filepath.Join(o.Out, name), func(w io.Writer) error { return writeZip(w, base, o.Date, entries) })
	}
	name := base + ".tar.gz"
	return name, writeFile(filepath.Join(o.Out, name), func(w io.Writer) error { return writeTarGz(w, base, o.Date, entries) })
}

func writeFile(path string, write func(io.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	if err := write(f); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}
	return nil
}

func writeTarGz(w io.Writer, base string, when time.Time, entries []entry) error {
	// A zero gzip header (no name, no time) keeps the bytes reproducible.
	gz, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(gz)
	when = when.UTC()
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: base + "/", Mode: 0o755, ModTime: when, Format: tar.FormatUSTAR}); err != nil {
		return err
	}
	for _, e := range entries {
		data, err := os.ReadFile(e.src)
		if err != nil {
			return fmt.Errorf("reading %s: %w", e.src, err)
		}
		h := &tar.Header{Typeflag: tar.TypeReg, Name: base + "/" + e.name, Mode: int64(e.mode), Size: int64(len(data)), ModTime: when, Format: tar.FormatUSTAR}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func writeZip(w io.Writer, base string, when time.Time, entries []entry) error {
	zw := zip.NewWriter(w)
	when = when.UTC()
	dir := &zip.FileHeader{Name: base + "/", Method: zip.Store, Modified: when}
	dir.SetMode(os.ModeDir | 0o755)
	if _, err := zw.CreateHeader(dir); err != nil {
		return err
	}
	for _, e := range entries {
		data, err := os.ReadFile(e.src)
		if err != nil {
			return fmt.Errorf("reading %s: %w", e.src, err)
		}
		h := &zip.FileHeader{Name: base + "/" + e.name, Method: zip.Deflate, Modified: when}
		h.SetMode(e.mode)
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
	}
	return zw.Close()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
