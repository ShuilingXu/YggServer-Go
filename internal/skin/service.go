package skin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/redisstore"
)

var (
	ErrQuota       = errors.New("texture quota exceeded")
	ErrRateLimited = errors.New("upload rate exceeded")
	ErrConflict    = errors.New("pending report already exists")
	ErrInvalid     = errors.New("invalid texture")
	ErrDenied      = errors.New("texture access denied")
	ErrMissing     = errors.New("texture not found")
	ErrUnavailable = errors.New("texture storage unavailable")
)

type Texture struct {
	ID        int64              `json:"-"`
	UUID      string             `json:"id"`
	AccountID int64              `json:"-"`
	Name      string             `json:"name"`
	Type      model.TextureType  `json:"type"`
	Hash      string             `json:"hash"`
	Size      int64              `json:"sizeBytes"`
	Width     int                `json:"width"`
	Height    int                `json:"height"`
	Public    int16              `json:"public"`
	CreatedAt time.Time          `json:"createdAt"`
	State     model.TextureState `json:"state"`
	ItemName  string             `json:"itemName,omitempty"`
}

type Service struct {
	Store       *Store
	Dir         string
	Now         func() time.Time
	MaxBytes    int64
	Redis       *redisstore.Store
	UploadLimit int
}

func ValidDimensions(kind model.TextureType, w, h int) bool {
	if kind == model.TextureCape && w > 0 && h > 0 && w%22 == 0 && h%17 == 0 && w/22 == h/17 {
		scale := int64(w / 22)
		return scale*64 <= 8192 && scale*32 <= 8192 && scale*64*scale*32 <= 16*1024*1024
	}
	if w < 64 || h < 32 || w > 8192 || h > 8192 || int64(w)*int64(h) > 16*1024*1024 || w%64 != 0 || h%32 != 0 {
		return false
	}
	switch kind {
	case model.TextureSteve:
		return w == h || w == 2*h
	case model.TextureAlex:
		return w == h
	case model.TextureCape:
		return w == 2*h
	default:
		return false
	}
}

func Sanitize(reader io.Reader, kind model.TextureType) ([]byte, int, int, error) {
	return sanitize(reader, kind, 1<<20)
}

func sanitize(reader io.Reader, kind model.TextureType, maxBytes int64) ([]byte, int, int, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read texture: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, 0, 0, ErrInvalid
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || !ValidDimensions(kind, config.Width, config.Height) {
		return nil, 0, 0, ErrInvalid
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, ErrInvalid
	}
	if kind == model.TextureCape && config.Width != 2*config.Height {
		scale := config.Width / 22
		canvas := image.NewNRGBA(image.Rect(0, 0, scale*64, scale*32))
		draw.Draw(canvas, img.Bounds(), img, image.Point{}, draw.Src)
		img = canvas
		config.Width, config.Height = scale*64, scale*32
	}
	var clean bytes.Buffer
	if err := png.Encode(&clean, img); err != nil {
		return nil, 0, 0, fmt.Errorf("encode texture: %w", err)
	}
	return clean.Bytes(), config.Width, config.Height, nil
}

func validName(name string) bool {
	return strings.TrimSpace(name) != "" && utf8.RuneCountInString(name) <= 50
}

func (s *Service) Upload(ctx context.Context, accountID int64, name string, kind model.TextureType, public int16, reader io.Reader, playerUUID string) (Texture, error) {
	if !validName(name) || public < 0 || public > 1 {
		return Texture{}, ErrInvalid
	}
	if s.UploadLimit > 0 {
		if s.Redis == nil {
			return Texture{}, ErrUnavailable
		}
		allowed, err := s.Redis.Allow(ctx, "texture-upload:"+fmt.Sprint(accountID), s.UploadLimit, time.Minute)
		if err != nil {
			return Texture{}, ErrUnavailable
		}
		if !allowed {
			return Texture{}, ErrRateLimited
		}
	}
	maxBytes := s.MaxBytes
	if maxBytes == 0 {
		maxBytes = 1 << 20
	}
	data, w, h, err := sanitize(reader, kind, maxBytes)
	if err != nil {
		return Texture{}, err
	}
	sum := sha256.Sum256(data)
	t := Texture{UUID: model.UUID(), AccountID: accountID, Name: name, Type: kind, Hash: hex.EncodeToString(sum[:]), Size: int64(len(data)), Width: w, Height: h, Public: public, CreatedAt: s.Now().UTC()}
	conn, release, err := s.Store.LockTexture(ctx, t.Hash)
	if err != nil {
		return Texture{}, err
	}
	defer release()
	// 文件按内容寻址且不可变；先原子发布完整文件，再提交元数据，失败只可能留下不可达文件。
	f, err := os.CreateTemp(s.Dir, "upload-*")
	if err != nil {
		return Texture{}, fmt.Errorf("create texture staging file: %w", err)
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return Texture{}, fmt.Errorf("write texture: %w", writeErr)
	}
	if closeErr != nil {
		return Texture{}, fmt.Errorf("close texture: %w", closeErr)
	}
	if err := os.Rename(f.Name(), filepath.Join(s.Dir, t.Hash)); err != nil {
		return Texture{}, fmt.Errorf("publish texture: %w", err)
	}
	if err := s.Store.Upload(ctx, conn, t, playerUUID); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if referenced, checkErr := s.Store.Referenced(cleanupCtx, conn, t.Hash); checkErr == nil && !referenced {
			_ = os.Remove(filepath.Join(s.Dir, t.Hash))
		}
		return Texture{}, err
	}
	return t, nil
}

func (s *Service) Delete(ctx context.Context, accountID int64, uuid string) error {
	if s.Dir == "" {
		return ErrUnavailable
	}
	t, err := s.Store.Get(ctx, uuid, accountID, true)
	if err != nil {
		return err
	}
	if t.AccountID != accountID {
		return ErrDenied
	}
	conn, release, err := s.Store.LockTexture(ctx, t.Hash)
	if err != nil {
		return err
	}
	defer release()
	unused, err := s.Store.Delete(ctx, conn, accountID, uuid, t.Hash)
	if err != nil {
		return err
	}
	if unused {
		if err := os.Remove(filepath.Join(s.Dir, t.Hash)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove unreferenced texture: %w", err)
		}
	}
	return nil
}

func (s *Service) Edit(ctx context.Context, accountID int64, uuid, name string, kind model.TextureType, public int16) error {
	if !validName(name) || public < 0 || public > 1 {
		return ErrInvalid
	}
	t, err := s.Store.Get(ctx, uuid, accountID, false)
	if err != nil {
		return err
	}
	if t.AccountID != accountID {
		return ErrDenied
	}
	if !ValidDimensions(kind, t.Width, t.Height) {
		return ErrInvalid
	}
	// 被角色使用时不允许改变 skin/cape 分类，避免已有绑定的语义被破坏。
	if (t.Type == model.TextureCape) != (kind == model.TextureCape) {
		return ErrInvalid
	}
	return s.Store.Edit(ctx, accountID, uuid, name, kind, public)
}

func ValidHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil && hash == strings.ToLower(hash)
}

func (s *Service) Report(ctx context.Context, accountID int64, uuid, reason string) error {
	if strings.TrimSpace(reason) == "" || utf8.RuneCountInString(reason) > 1000 {
		return ErrInvalid
	}
	if s.Redis == nil {
		return ErrUnavailable
	}
	allowed, err := s.Redis.Allow(ctx, "texture-report:"+fmt.Sprint(accountID), 10, time.Minute)
	if err != nil {
		return ErrUnavailable
	}
	if !allowed {
		return ErrRateLimited
	}
	return s.Store.Report(ctx, accountID, uuid, reason, s.Now().UTC())
}
