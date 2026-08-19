package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Face is one standing entry in the channel's portrait library
// (configs/faces.yaml) — a real photo/portrait generated once, by hand,
// and reused across every thumbnail whose narrator matches Sex and whose
// age falls in [AgeFeel[0], AgeFeel[1]]. This is deliberately NOT the same
// portrait avatar generates per-script: that one dramatizes a specific
// narrator; this one is the channel's recognizable face across the whole
// catalog.
type Face struct {
	ID      string `yaml:"id"`
	File    string `yaml:"file"`
	Sex     string `yaml:"sex"`
	AgeFeel [2]int `yaml:"age_feel"`
}

// Matches reports whether f is a plausible stand-in for a narrator of the
// given sex and age — sex must match exactly; age must fall within
// AgeFeel inclusive. AgeFeel == [0,0] (unset) matches any age, so a face
// library entry doesn't have to specify a range it doesn't care about.
func (f Face) Matches(sex string, age int) bool {
	if f.Sex != sex {
		return false
	}
	if f.AgeFeel == [2]int{0, 0} {
		return true
	}
	return age >= f.AgeFeel[0] && age <= f.AgeFeel[1]
}

// FaceLibrary is faces.yaml's shape.
type FaceLibrary struct {
	Faces []Face `yaml:"faces"`
}

// LoadFaces reads and parses faces.yaml at path.
func LoadFaces(path string) (FaceLibrary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FaceLibrary{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	var lib FaceLibrary
	if err := yaml.Unmarshal(data, &lib); err != nil {
		return FaceLibrary{}, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if len(lib.Faces) == 0 {
		return FaceLibrary{}, fmt.Errorf("config: %s: no faces defined — the channel needs at least one standing portrait", path)
	}
	for _, f := range lib.Faces {
		if f.ID == "" {
			return FaceLibrary{}, fmt.Errorf("config: %s: a face is missing id", path)
		}
		if f.File == "" {
			return FaceLibrary{}, fmt.Errorf("config: %s: face %q has no file", path, f.ID)
		}
		if f.Sex == "" {
			return FaceLibrary{}, fmt.Errorf("config: %s: face %q has no sex", path, f.ID)
		}
	}
	return lib, nil
}

// ByID returns the face with the given id, or ok=false if none matches.
func (lib FaceLibrary) ByID(id string) (Face, bool) {
	for _, f := range lib.Faces {
		if f.ID == id {
			return f, true
		}
	}
	return Face{}, false
}
