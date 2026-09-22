package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestEngineVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "go.mod")
	const content = `module example.com/x

go 1.26.2

require (
	github.com/odevine/mimic/engine v0.3.0
	github.com/oov/psd v0.0.0-20260818185439-a5d50ec0acac
)
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := engineVersion(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "v0.3.0" {
		t.Fatalf("engineVersion = %q, want v0.3.0", got)
	}

	if _, err := engineVersion(filepath.Join(dir, "missing.mod")); err == nil {
		t.Fatal("expected an error reading a missing go.mod")
	}
}

func TestWriteBundle(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "manifest.json"), `{"template":"normal"}`)
	writeFile(t, filepath.Join(src, "bundle.json"), `{"stale":true}`)
	writeFile(t, filepath.Join(src, ".DS_Store"), "junk")
	writeFile(t, filepath.Join(src, "background", "any.png"), "PNGDATA")

	out := filepath.Join(t.TempDir(), "normal.mimic")
	b := Bundle{Format: 1, Template: "normal", Version: "1.2.0", MinEngine: "0.3.0"}
	n, err := writeBundle(out, src, b)
	if err != nil {
		t.Fatal(err)
	}
	// bundle.json (written from the header) + manifest.json + one PNG. The
	// stale bundle.json and the dotfile are excluded
	if n != 3 {
		t.Fatalf("entries = %d, want 3", n)
	}

	zr, err := zip.OpenReader(out)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	methods := map[string]uint16{}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		methods[f.Name] = f.Method
	}
	if names[".DS_Store"] {
		t.Error("dotfile should be excluded")
	}
	if !names["bundle.json"] || !names["manifest.json"] || !names["background/any.png"] {
		t.Errorf("missing expected entries: %v", names)
	}
	if methods["background/any.png"] != zip.Store {
		t.Error("PNG should be stored uncompressed")
	}
	if methods["manifest.json"] != zip.Deflate {
		t.Error("JSON should be deflated")
	}

	// The header written into the zip is the one pack computed, not the stale
	// bundle.json from the source directory
	rc, err := zr.Open("bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	var got Bundle
	if err := json.NewDecoder(rc).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got != b {
		t.Errorf("bundle.json = %+v, want %+v", got, b)
	}
}

func TestRepackBundle(t *testing.T) {
	// Build a base bundle with a real layer PNG the manifest references
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "manifest.json"),
		`{"template":"normal","width":10,"height":10,"layers":[{"name":"background","colorVariants":{"any":{"path":"background/any.png"}}}]}`)
	writeFile(t, filepath.Join(src, "background", "any.png"), "ORIGINAL-PNG-BYTES")
	base := filepath.Join(t.TempDir(), "base.mimic")
	if _, err := writeBundle(base, src, Bundle{Format: 1, Template: "normal", Version: "1.0.0", MinEngine: "0.3.0"}); err != nil {
		t.Fatal(err)
	}

	// Repack it with an edited manifest that still points at the same PNG
	edited := []byte(`{"template":"normal","width":20,"height":20,"layers":[{"name":"background","colorVariants":{"any":{"path":"background/any.png"}}}]}`)
	m, err := validateManifestBytes(edited)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "repacked.mimic")
	if _, err := repackBundle(out, base, edited, m, Bundle{Format: 1, Template: "normal", Version: "1.1.0", MinEngine: "0.3.0"}); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.OpenReader(out)
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
		data, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(data)
	}

	// The PNG is carried over byte-for-byte from the base bundle
	if got["background/any.png"] != "ORIGINAL-PNG-BYTES" {
		t.Errorf("carried PNG = %q, want the original bytes", got["background/any.png"])
	}
	// The manifest is the edited one
	if got["manifest.json"] != string(edited) {
		t.Error("repacked manifest should be the edited one")
	}
	// The header carries the new version
	var b Bundle
	if err := json.Unmarshal([]byte(got["bundle.json"]), &b); err != nil {
		t.Fatal(err)
	}
	if b.Version != "1.1.0" {
		t.Errorf("version = %q, want 1.1.0", b.Version)
	}
}

func TestRepackRejectsMissingLayer(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "manifest.json"),
		`{"template":"normal","width":10,"height":10,"layers":[{"name":"background","colorVariants":{"any":{"path":"background/any.png"}}}]}`)
	writeFile(t, filepath.Join(src, "background", "any.png"), "PNG")
	base := filepath.Join(t.TempDir(), "base.mimic")
	if _, err := writeBundle(base, src, Bundle{Format: 1, Template: "normal", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}

	// Edited manifest points at a PNG the base bundle does not contain
	edited := []byte(`{"template":"normal","width":10,"height":10,"layers":[{"name":"crown","colorVariants":{"any":{"path":"crown/any.png"}}}]}`)
	m, err := validateManifestBytes(edited)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "bad.mimic")
	if _, err := repackBundle(out, base, edited, m, Bundle{Format: 1, Template: "normal", Version: "1.1.0"}); err == nil {
		t.Fatal("expected repack to reject a manifest referencing a missing PNG")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
