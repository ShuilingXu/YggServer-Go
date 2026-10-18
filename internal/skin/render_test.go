package skin

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/ShuilingXu/YggServer-Go/internal/model"
)

func TestAvatarAndPreview(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	source.SetNRGBA(8, 8, color.NRGBA{R: 255, A: 255})
	source.SetNRGBA(40, 8, color.NRGBA{B: 255, A: 128})
	data, err := render(source, model.TextureSteve, 64, true)
	if err != nil {
		t.Fatal(err)
	}
	avatar, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	pixel := color.NRGBAModel.Convert(avatar.At(0, 0)).(color.NRGBA)
	if pixel.R < 120 || pixel.R > 130 || pixel.B < 125 || pixel.B > 135 || pixel.A != 255 {
		t.Fatalf("hat layer not composed over face: %v", pixel)
	}
	for _, tc := range []struct {
		kind   model.TextureType
		height int
		width  int
	}{{0, 32, 64}, {0, 64, 64}, {1, 64, 64}, {2, 32, 80}} {
		src := image.NewNRGBA(image.Rect(0, 0, 64, tc.height))
		data, err := render(src, tc.kind, 128, false)
		if err != nil {
			t.Fatal(err)
		}
		preview, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if preview.Bounds().Dx() != tc.width || preview.Bounds().Dy() != 128 {
			t.Fatalf("wrong preview dimensions: %v", preview.Bounds())
		}
	}
	if _, err := render(source, model.TextureCape, 64, true); err != ErrInvalid {
		t.Fatal("cape accepted as avatar")
	}
}
