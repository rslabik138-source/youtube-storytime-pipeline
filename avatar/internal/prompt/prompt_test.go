package prompt

import (
	"strings"
	"testing"
	"text/template"

	"github.com/placeholder/avatar/internal/manifest"
)

func testTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := template.New("avatar").Parse(
		"Photorealistic waist-up portrait of an attractive, photogenic {{.Build}} {{.Sex}}, approximately {{.Age}} years old, {{.Hair}}, {{.FaceNote}}, wearing {{.Attire}}, standing upright and facing the camera straight-on, a warm genuine friendly smile with visible teeth, looking directly into the camera with engaging eye contact, camera at eye level in a straight head-on angle (absolutely NOT a high angle, NOT shot from above, NOT looking down at the subject), framed from roughly the waist up with the hands out of frame and no legs, hips or floor visible, bright flattering soft key light, vivid lifelike skin tones, crisp sharp focus on the eyes, shallow depth of field, simple uncluttered evenly-lit softly blurred neutral studio background for clean subject cutout, polished high-click YouTube thumbnail quality, no text, no watermark, no logos")
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	return tmpl
}

func fullManifest() manifest.Manifest {
	return manifest.Manifest{
		ID: "s1", Title: "T", Profession: "accountant", WordCount: 100,
		Narrator: manifest.Narrator{
			Name: "Clara Vance", Age: 43, Sex: "female",
			Build: "average", Hair: "gray, cropped short",
			FaceNote: "reading glasses pushed up into the hair",
			Attire:   "a plain button-down with the sleeves rolled",
		},
	}
}

func TestRenderIncludesEveryField(t *testing.T) {
	got, err := Render(testTemplate(t), fullManifest())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		"average", "female", "43 years old", "gray, cropped short",
		"reading glasses pushed up into the hair",
		"a plain button-down with the sleeves rolled",
		// The fixed clickable-thumbnail directives must always be present.
		"warm genuine friendly smile", "looking directly into the camera",
		"no text, no watermark, no logos",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected rendered prompt to contain %q, got: %s", want, got)
		}
	}
}

func TestRenderFallsBackToNeutralTextForBlankAppearanceFields(t *testing.T) {
	m := manifest.Manifest{
		ID: "s1", Title: "T", Profession: "nurse", WordCount: 100,
		Narrator: manifest.Narrator{Name: "Dana", Age: 42, Sex: "female"}, // no build/hair/face_note/attire
	}
	got, err := Render(testTemplate(t), m)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(got, ",  ,") || strings.Contains(got, ", ,") {
		t.Fatalf("expected no empty gaps between commas for blank fields, got: %s", got)
	}
	if !strings.Contains(got, "medium build") {
		t.Fatalf("expected a neutral build fallback, got: %s", got)
	}
	if !strings.Contains(got, "female") || !strings.Contains(got, "42 years old") {
		t.Fatalf("expected the real sex/age to still come through, got: %s", got)
	}
}

func TestFromManifestPassesThroughRealFieldsUnchanged(t *testing.T) {
	d := FromManifest(fullManifest())
	if d.Sex != "female" || d.Age != 43 || d.Profession != "accountant" {
		t.Fatalf("unexpected core fields: %+v", d)
	}
	if d.Build != "average" || d.Hair != "gray, cropped short" {
		t.Fatalf("expected real appearance fields to pass through unchanged, got: %+v", d)
	}
}
