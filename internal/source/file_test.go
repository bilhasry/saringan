package source

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bilhasry/saringan/internal/config"
)

func TestJSONFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte(`{"name":"ada","tags":["x","y"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newFileSource("f", config.Source{Type: "file", Path: path})
	doc, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !doc.IsStructured() {
		t.Fatal("expected structured document")
	}
	m, ok := doc.Data.(map[string]any)
	if !ok || m["name"] != "ada" {
		t.Fatalf("bad data: %#v", doc.Data)
	}
}

func TestCSVFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")
	csv := "name,ssn\nada,123-45-6789\nbob,987-65-4321\n"
	if err := os.WriteFile(path, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newFileSource("f", config.Source{Type: "file", Path: path})
	doc, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	rows, ok := doc.Data.([]map[string]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("want 2 rows, got %#v", doc.Data)
	}
	if rows[0]["ssn"] != "123-45-6789" {
		t.Fatalf("bad row: %#v", rows[0])
	}
}

func TestYAMLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.yaml")
	yml := "user:\n  name: ada\n  ssn: 123-45-6789\n"
	if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newFileSource("f", config.Source{Type: "file", Path: path})
	doc, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	m, ok := doc.Data.(map[string]any)
	if !ok {
		t.Fatalf("want map, got %#v", doc.Data)
	}
	user, ok := m["user"].(map[string]any)
	if !ok || user["name"] != "ada" {
		t.Fatalf("bad nested data: %#v", m)
	}
}

func TestTextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	if err := os.WriteFile(path, []byte("ssn=123-45-6789"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newFileSource("f", config.Source{Type: "file", Path: path})
	doc, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if doc.IsStructured() || doc.Raw != "ssn=123-45-6789" {
		t.Fatalf("bad doc: %+v", doc)
	}
}

func TestMissingFile(t *testing.T) {
	s := newFileSource("f", config.Source{Type: "file", Path: "/does/not/exist.json"})
	if _, err := s.Fetch(context.Background()); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestEnvSource(t *testing.T) {
	t.Setenv("SARINGAN_A", "one")
	t.Setenv("SARINGAN_B", "two")
	s := newEnvSource("e", config.Source{Type: "env", Keys: []string{"SARINGAN_A", "SARINGAN_B", "SARINGAN_MISSING"}})
	doc, err := s.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	m := doc.Data.(map[string]any)
	if m["SARINGAN_A"] != "one" || m["SARINGAN_B"] != "two" {
		t.Fatalf("bad env data: %#v", m)
	}
	if v, ok := m["SARINGAN_MISSING"]; !ok || v != nil {
		t.Fatalf("missing key should be nil: %#v", m)
	}
}
