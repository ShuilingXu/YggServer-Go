package skin

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"testing"

	"github.com/ShuilingXu/YggServer-Go/internal/model"
)

func TestTextureUploadValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    model.TextureType
		w, h    int
		allowed bool
	}{
		{"classic", 0, 64, 32, true}, {"modern", 0, 64, 64, true}, {"slim", 1, 64, 64, true}, {"cape", 2, 64, 32, true},
		{"legacy slim rejected", 1, 64, 32, false}, {"square cape rejected", 2, 64, 64, false}, {"bad scale", 0, 65, 64, false},
		{"compact cape", 2, 22, 17, true}, {"scaled compact cape", 2, 44, 34, true},
		{"large decompression rejected", 0, 8192, 8192, false}, {"invalid kind", 3, 64, 64, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if ValidDimensions(tc.kind, tc.w, tc.h) != tc.allowed {
				t.Fatalf("dimensions %dx%d kind=%d accepted=%v", tc.w, tc.h, tc.kind, !tc.allowed)
			}
			if tc.w > 128 {
				return
			}
			var source bytes.Buffer
			if err := png.Encode(&source, image.NewNRGBA(image.Rect(0, 0, tc.w, tc.h))); err != nil {
				t.Fatal(err)
			}
			data, w, h, err := Sanitize(&source, tc.kind)
			if (err == nil) != tc.allowed {
				t.Fatalf("upload accepted=%v, want %v", err == nil, tc.allowed)
			}
			if tc.allowed {
				expectedW, expectedH := tc.w, tc.h
				if tc.kind == model.TextureCape && tc.w != 2*tc.h {
					expectedW, expectedH = tc.w/22*64, tc.h/17*32
				}
				if w != expectedW || h != expectedH {
					t.Fatal("texture dimensions changed")
				}
				if _, err := png.Decode(bytes.NewReader(data)); err != nil {
					t.Fatal("cleaned texture cannot be decoded")
				}
			}
		})
	}
	for _, data := range [][]byte{[]byte("not png"), bytes.Repeat([]byte{0}, (1<<20)+1)} {
		if _, _, _, err := Sanitize(bytes.NewReader(data), model.TextureSteve); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid or oversized upload accepted")
		}
	}
}
