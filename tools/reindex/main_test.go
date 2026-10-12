package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSplitTag(t *testing.T) {
	cases := []struct {
		tag, name, version string
		ok                 bool
	}{
		{"normal/1.2.0", "normal", "1.2.0", true},
		{"normal/v1.2.0", "normal", "1.2.0", true},
		{"engine/0.3.0", "engine", "0.3.0", true},
		{"noversion", "", "", false},
		{"/1.2.0", "", "", false},
		{"normal/", "", "", false},
	}
	for _, c := range cases {
		name, version, ok := splitTag(c.tag)
		if ok != c.ok || name != c.name || version != c.version {
			t.Errorf("splitTag(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.tag, name, version, ok, c.name, c.version, c.ok)
		}
	}
}

func TestLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.0.0", "1.0.1", true},
		{"1.2.0", "1.10.0", true},
		{"2.0.0", "1.9.9", false},
		{"1.2.0", "1.2.0", false},
		{"0.9.0", "1.0.0", true},
	}
	for _, c := range cases {
		if got := less(c.a, c.b); got != c.want {
			t.Errorf("less(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// bundleOf builds a bundle zip holding the named files
func bundleOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadBundleInfoOfTheFirstFormat(t *testing.T) {
	b := bundleOf(t, map[string]string{"bundle.json": `{"format":1,"template":"normal","version":"1.2.0","minEngine":"0.3.0"}`, "manifest.json": `{"template":"normal"}`})
	got, err := readBundleInfo(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Format != 1 || got.MinEngine != "0.3.0" || got.ID != "" || got.Description != "" || len(got.Shapes) != 0 {
		t.Fatalf("info = %+v", got)
	}
}

func TestReadBundleInfoOfAnIdentifiedTemplate(t *testing.T) {
	b := bundleOf(t, map[string]string{
		"bundle.json": `{"format":2,"id":"mimic.m15","version":"0.1.0","minEngine":"0.18.0"}`,
		"manifest.json": `{"name":"Modern frame (M15)","description":"The modern card frame","layouts":{
			"normal":{"serves":[{"role":"single","kind":"standard"}]},
			"transform":{"serves":[{"role":"transform_front","kind":"standard"},{"role":"transform_back","kind":"standard"}]},
			"again":{"serves":[{"role":"single","kind":"standard"}]}}}`,
	})
	got, err := readBundleInfo(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Format != 2 || got.ID != "mimic.m15" || got.MinEngine != "0.18.0" || got.Description != "The modern card frame" || got.Title != "Modern frame (M15)" {
		t.Errorf("info = %+v", got)
	}
	want := []Shape{{"single", "standard"}, {"transform_back", "standard"}, {"transform_front", "standard"}}
	if len(got.Shapes) != len(want) {
		t.Fatalf("shapes = %v, want %v", got.Shapes, want)
	}
	for i := range want {
		if got.Shapes[i] != want[i] {
			t.Errorf("shapes[%d] = %v, want %v", i, got.Shapes[i], want[i])
		}
	}
}

func TestReadBundleInfoMissingHeader(t *testing.T) {
	b := bundleOf(t, map[string]string{"manifest.json": "{}"})
	if _, err := readBundleInfo(bytes.NewReader(b), int64(len(b))); err == nil {
		t.Fatal("expected an error when bundle.json is absent")
	}
}

func TestLoadKnownIgnoresAnIndexOfAnOlderSchema(t *testing.T) {
	dir := t.TempDir()
	write := func(schema int) string {
		path := filepath.Join(dir, "index.json")
		body := fmt.Sprintf(`{"schema":%d,"templates":[{"name":"mimic.m15","latest":"0.1.0","versions":[{"version":"0.1.0","format":2,"minEngine":"0.18.0","url":"u","size":1,"sha256":"s"}]}]}`, schema)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if k := loadKnown(write(1)); len(k.versions) != 0 {
		t.Errorf("an older index was trusted: %+v", k.versions)
	}
	k := loadKnown(write(indexSchema))
	if k.versions["u"].Format != 2 || k.names["u"] != "mimic.m15" || k.templates["mimic.m15"].Latest != "0.1.0" {
		t.Errorf("known = %+v", k)
	}
}

func TestAnIdentifiedBundleIndexedWithoutATitleIsReadAgain(t *testing.T) {
	k := knownIndex{
		versions: map[string]Version{"new": {Format: 2}, "old": {Format: 1}, "titled": {Format: 2}},
		names:    map[string]string{"new": "mimic.m15", "old": "normal", "titled": "mimic.other"},
		templates: map[string]Template{
			"mimic.m15":   {Name: "mimic.m15"},
			"normal":      {Name: "normal"},
			"mimic.other": {Name: "mimic.other", Title: "Other"},
		},
	}
	if !k.lacksTitle("new") {
		t.Error("a format 2 bundle with no title was kept as it was")
	}
	if k.lacksTitle("old") {
		t.Error("a bundle of the first format, which has no title to read, is read again every run")
	}
	if k.lacksTitle("titled") {
		t.Error("a bundle with a title is read again")
	}
}
