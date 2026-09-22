package main

import (
	"archive/zip"
	"bytes"
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

func TestReadMinEngine(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`{"format":1,"template":"normal","version":"1.2.0","minEngine":"0.3.0"}`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := readMinEngine(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.3.0" {
		t.Fatalf("minEngine = %q, want 0.3.0", got)
	}
}

func TestReadMinEngineMissing(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("manifest.json"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readMinEngine(bytes.NewReader(buf.Bytes()), int64(buf.Len())); err == nil {
		t.Fatal("expected an error when bundle.json is absent")
	}
}
