package skin

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/ShuilingXu/YggServer-Go/internal/model"
)

func (s *Service) Render(ctx context.Context, uuid string, accountID int64, size int, avatar bool) ([]byte, error) {
	if size < 16 || size > 512 {
		return nil, ErrInvalid
	}
	if s.Dir == "" {
		return nil, ErrUnavailable
	}
	t, err := s.Store.Get(ctx, uuid, accountID, false)
	if err != nil {
		return nil, err
	}
	if avatar && t.Type == model.TextureCape {
		return nil, ErrInvalid
	}
	f, err := os.Open(filepath.Join(s.Dir, t.Hash))
	if err != nil {
		return nil, ErrMissing
	}
	defer f.Close()
	config, err := png.DecodeConfig(f)
	if err != nil || config.Width != t.Width || config.Height != t.Height || !ValidDimensions(t.Type, config.Width, config.Height) {
		return nil, ErrInvalid
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	source, err := png.Decode(f)
	if err != nil {
		return nil, ErrInvalid
	}
	return render(source, t.Type, size, avatar)
}

func render(source image.Image, kind model.TextureType, size int, avatar bool) ([]byte, error) {
	if avatar && kind == model.TextureCape {
		return nil, ErrInvalid
	}
	scale := source.Bounds().Dx() / 64
	if scale < 1 {
		return nil, ErrInvalid
	}
	width, height := 16, 32
	if avatar {
		width, height = 8, 8
	}
	if kind == model.TextureCape {
		width, height = 10, 16
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, width*scale, height*scale))
	copyPart := func(x, y, w, h, dx, dy int, mirror bool) {
		rectangle := image.Rect(dx*scale, dy*scale, (dx+w)*scale, (dy+h)*scale)
		if !mirror {
			draw.Draw(canvas, rectangle, source, image.Pt(x*scale, y*scale), draw.Over)
			return
		}

		mirrored := image.NewNRGBA(image.Rect(0, 0, w*scale, h*scale))
		for cy := 0; cy < h*scale; cy++ {
			for cx := 0; cx < w*scale; cx++ {
				mirrored.Set(cx, cy, source.At((x+w)*scale-1-cx, y*scale+cy))
			}
		}
		draw.Draw(canvas, rectangle, mirrored, image.Point{}, draw.Over)
	}
	if avatar {
		copyPart(8, 8, 8, 8, 0, 0, false)
		copyPart(40, 8, 8, 8, 0, 0, false)
	} else if kind == model.TextureCape {
		copyPart(1, 1, 10, 16, 0, 0, false)
	} else {
		copyPart(8, 8, 8, 8, 4, 0, false)
		copyPart(40, 8, 8, 8, 4, 0, false)
		copyPart(20, 20, 8, 12, 4, 8, false)
		arm := 4
		if kind == model.TextureAlex {
			arm = 3
		}
		copyPart(44, 20, arm, 12, 4-arm, 8, false)
		copyPart(4, 20, 4, 12, 4, 20, false)
		modern := source.Bounds().Dy() == 64*scale
		if modern {
			copyPart(36, 52, arm, 12, 12, 8, false)
			copyPart(20, 52, 4, 12, 8, 20, false)
			copyPart(20, 36, 8, 12, 4, 8, false)
			copyPart(44, 36, arm, 12, 4-arm, 8, false)
			copyPart(52, 52, arm, 12, 12, 8, false)
			copyPart(4, 36, 4, 12, 4, 20, false)
			copyPart(4, 52, 4, 12, 8, 20, false)
		} else {
			copyPart(44, 20, 4, 12, 12, 8, true)
			copyPart(4, 20, 4, 12, 8, 20, true)
		}
	}
	outW := size * width / height
	output := image.NewNRGBA(image.Rect(0, 0, outW, size))
	for y := 0; y < size; y++ {
		for x := 0; x < outW; x++ {
			c := color.NRGBAModel.Convert(canvas.At(x*canvas.Bounds().Dx()/outW, y*canvas.Bounds().Dy()/size)).(color.NRGBA)
			output.SetNRGBA(x, y, c)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, output); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
