package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	var dir string
	var out string
	flag.StringVar(&dir, "dir", "", "directory containing per-protocol registry json files")
	flag.StringVar(&out, "out", "", "output zip path")
	flag.Parse()
	if dir == "" || out == "" {
		fatalf("-dir and -out are required")
	}
	if err := pack(dir, out); err != nil {
		fatalf("%v", err)
	}
}

func pack(dir string, out string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return fmt.Errorf("no protocol json files found in %s", dir)
	}

	temp := out + ".tmp"
	target, err := os.Create(temp)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(target)
	for _, file := range files {
		if err := addFile(writer, file); err != nil {
			_ = writer.Close()
			_ = target.Close()
			_ = os.Remove(temp)
			return err
		}
	}
	if err := writer.Close(); err != nil {
		_ = target.Close()
		_ = os.Remove(temp)
		return err
	}
	if err := target.Close(); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return os.Rename(temp, out)
}

func addFile(writer *zip.Writer, path string) error {
	name := filepath.Base(path)
	if strings.TrimSuffix(name, filepath.Ext(name)) == "" {
		return fmt.Errorf("invalid protocol file %s", path)
	}
	header := &zip.FileHeader{
		Name:     name,
		Method:   zip.Deflate,
		Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	header.SetMode(0o644)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = io.Copy(entry, source)
	return err
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
