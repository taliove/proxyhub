// Command archive creates a deterministic tar.gz release archive from a
// directory. Every entry is sorted by name and stamped with a fixed
// SOURCE_DATE_EPOCH so the same input tree produces a byte-identical
// archive. Used by scripts/release/package.sh.
package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type releaseFile struct {
	name string
	path string
	mode os.FileMode
	size int64
}

func main() {
	input := flag.String("input", "", "directory containing archive files")
	output := flag.String("output", "", "archive output path (.tar.gz)")
	epoch := flag.Int64("epoch", 0, "Unix timestamp used for every archive entry")
	flag.Parse()

	if *input == "" || *output == "" || *epoch <= 0 {
		fmt.Fprintln(os.Stderr, "archive: -input, -output and a positive -epoch are required")
		os.Exit(2)
	}

	if err := run(*input, *output, *epoch); err != nil {
		_ = os.Remove(*output)
		fmt.Fprintf(os.Stderr, "archive: %v\n", err)
		os.Exit(1)
	}
}

func run(input, output string, epoch int64) error {
	files, err := releaseFiles(input)
	if err != nil {
		return err
	}
	return writeTarGz(output, files, time.Unix(epoch, 0).UTC())
}

func releaseFiles(root string) ([]releaseFile, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read input directory: %w", err)
	}
	files := make([]releaseFile, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("input %s is not a regular file", entry.Name())
		}
		files = append(files, releaseFile{
			name: entry.Name(),
			path: filepath.Join(root, entry.Name()),
			mode: info.Mode().Perm(),
			size: info.Size(),
		})
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("input directory %s is empty", root)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

func writeTarGz(output string, files []releaseFile, stamp time.Time) (returnErr error) {
	out, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer func() {
		if err := out.Close(); err != nil && returnErr == nil {
			returnErr = fmt.Errorf("close archive: %w", err)
		}
	}()

	gz, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}
	gz.Header.ModTime = stamp
	gz.Header.OS = 255 // unknown OS, avoids host leakage
	defer func() {
		if err := gz.Close(); err != nil && returnErr == nil {
			returnErr = fmt.Errorf("finish gzip stream: %w", err)
		}
	}()

	tw := tar.NewWriter(gz)
	defer func() {
		if err := tw.Close(); err != nil && returnErr == nil {
			returnErr = fmt.Errorf("finish tar stream: %w", err)
		}
	}()

	for _, f := range files {
		if err := writeEntry(tw, f, stamp); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(tw *tar.Writer, f releaseFile, stamp time.Time) error {
	header := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     f.name,
		Size:     f.size,
		Mode:     int64(f.mode),
		ModTime:  stamp,
		Uid:      0,
		Gid:      0,
		Uname:    "root",
		Gname:    "root",
		Format:   tar.FormatUSTAR,
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write header for %s: %w", f.name, err)
	}
	src, err := os.Open(f.path)
	if err != nil {
		return fmt.Errorf("open %s: %w", f.name, err)
	}
	defer src.Close()
	if _, err := io.Copy(tw, src); err != nil {
		return fmt.Errorf("write %s: %w", f.name, err)
	}
	return nil
}
