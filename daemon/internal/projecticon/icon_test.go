package projecticon

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestRasterAndInternalSymlinkIcons(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, "icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	bitmap := image.NewNRGBA(image.Rect(0, 0, 24, 24))
	bitmap.Set(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(file, bitmap); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if err := os.Symlink("icon.png", filepath.Join(dir, "alias.png")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"icon.png", "alias.png"} {
		icon, err := Read(dir, name)
		if err != nil || icon.MediaType != "image/png" || len(icon.ETag) != 64 || len(icon.Content) == 0 {
			t.Fatalf("%s: %+v %v", name, icon, err)
		}
	}
}
