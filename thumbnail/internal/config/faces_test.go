package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFacesParsesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "faces.yaml")
	content := `
faces:
  - id: face-01
    file: assets/faces/face-01.png
    sex: female
    age_feel: [35, 50]
  - id: face-02
    file: assets/faces/face-02.png
    sex: male
    age_feel: [40, 60]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write faces.yaml: %v", err)
	}

	lib, err := LoadFaces(path)
	if err != nil {
		t.Fatalf("LoadFaces: %v", err)
	}
	if len(lib.Faces) != 2 {
		t.Fatalf("expected 2 faces, got %d", len(lib.Faces))
	}
	if lib.Faces[0].ID != "face-01" || lib.Faces[0].Sex != "female" || lib.Faces[0].AgeFeel != [2]int{35, 50} {
		t.Fatalf("unexpected face-01: %+v", lib.Faces[0])
	}
}

func TestLoadFacesEmptyListReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "faces.yaml")
	if err := os.WriteFile(path, []byte("faces: []"), 0o644); err != nil {
		t.Fatalf("write faces.yaml: %v", err)
	}
	if _, err := LoadFaces(path); err == nil {
		t.Fatalf("expected an error for an empty face library")
	}
}

func TestLoadFacesMissingFieldReturnsError(t *testing.T) {
	for name, content := range map[string]string{
		"missing id":   "faces:\n  - file: f.png\n    sex: female\n",
		"missing file": "faces:\n  - id: face-01\n    sex: female\n",
		"missing sex":  "faces:\n  - id: face-01\n    file: f.png\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "faces.yaml")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write faces.yaml: %v", err)
			}
			if _, err := LoadFaces(path); err == nil {
				t.Fatalf("expected an error for %s", name)
			}
		})
	}
}

func TestLoadFacesMissingFileReturnsError(t *testing.T) {
	if _, err := LoadFaces(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatalf("expected an error for a missing file")
	}
}

func TestFaceMatchesSexAndAgeRange(t *testing.T) {
	f := Face{ID: "face-01", Sex: "female", AgeFeel: [2]int{35, 50}}
	if !f.Matches("female", 40) {
		t.Fatalf("expected a match for female age 40")
	}
	if f.Matches("male", 40) {
		t.Fatalf("expected no match for the wrong sex")
	}
	if f.Matches("female", 30) || f.Matches("female", 51) {
		t.Fatalf("expected no match outside the age_feel range")
	}
	if !f.Matches("female", 35) || !f.Matches("female", 50) {
		t.Fatalf("expected the age_feel range bounds to be inclusive")
	}
}

func TestFaceMatchesAnyAgeWhenAgeFeelUnset(t *testing.T) {
	f := Face{ID: "face-01", Sex: "female"}
	if !f.Matches("female", 20) || !f.Matches("female", 90) {
		t.Fatalf("expected an unset age_feel to match any age")
	}
}

func TestFaceLibraryByID(t *testing.T) {
	lib := FaceLibrary{Faces: []Face{{ID: "face-01"}, {ID: "face-02"}}}
	f, ok := lib.ByID("face-02")
	if !ok || f.ID != "face-02" {
		t.Fatalf("expected to find face-02, got %+v ok=%v", f, ok)
	}
	if _, ok := lib.ByID("face-99"); ok {
		t.Fatalf("expected no match for an unknown id")
	}
}
