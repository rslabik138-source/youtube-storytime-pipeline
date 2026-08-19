package cutout

import (
	"reflect"
	"testing"
)

func TestArgsIncludesModelAndPaths(t *testing.T) {
	got := Args(Options{Model: "birefnet-portrait"}, "in.png", "out.png")
	want := []string{"i", "-m", "birefnet-portrait", "in.png", "out.png"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args = %v, want %v", got, want)
	}
}

func TestArgsOmitsModelFlagWhenBlank(t *testing.T) {
	got := Args(Options{}, "in.png", "out.png")
	want := []string{"i", "in.png", "out.png"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Args = %v, want %v (no -m when model unset, rembg uses its default)", got, want)
	}
}

func TestRemoveBackgroundRejectsUnconfiguredCommand(t *testing.T) {
	_, err := RemoveBackground(t.Context(), Options{}, []byte("not-really-a-png"))
	if err == nil {
		t.Fatal("expected an error when rembg_cmd is empty")
	}
}
