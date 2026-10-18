package skin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type Inspection struct {
	Referenced int      `json:"referenced"`
	Missing    []string `json:"missing"`
	Corrupt    []string `json:"corrupt"`
	Orphans    []string `json:"orphans"`
	Removed    []string `json:"removed"`
	Ignored    int      `json:"ignored"`
}

func (s *Service) Inspect(ctx context.Context, prune bool, minAge time.Duration) (Inspection, error) {
	report := Inspection{Missing: []string{}, Corrupt: []string{}, Orphans: []string{}, Removed: []string{}}
	if s.Dir == "" || minAge < time.Hour {
		return report, ErrInvalid
	}
	hashes, err := s.Store.Hashes(ctx)
	if err != nil {
		return report, err
	}
	report.Referenced = len(hashes)
	for _, hash := range hashes {
		if !ValidHash(hash) {
			report.Corrupt = append(report.Corrupt, hash)
			continue
		}
		info, err := os.Lstat(filepath.Join(s.Dir, hash))
		if errors.Is(err, os.ErrNotExist) {
			report.Missing = append(report.Missing, hash)
			continue
		}
		if err != nil {
			return report, fmt.Errorf("inspect texture file: %w", err)
		}
		if !info.Mode().IsRegular() {
			report.Corrupt = append(report.Corrupt, hash)
		}
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return report, fmt.Errorf("inspect texture directory: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		hash := entry.Name()
		if !ValidHash(hash) || entry.Type()&os.ModeSymlink != 0 {
			report.Ignored++
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return report, err
		}
		if !info.Mode().IsRegular() {
			report.Ignored++
			continue
		}
		conn, release, err := s.Store.LockTexture(ctx, hash)
		if err != nil {
			return report, err
		}
		err = func() error {
			defer release()
			referenced, err := s.Store.Referenced(ctx, conn, hash)
			if err != nil {
				return err
			}
			path := filepath.Join(s.Dir, hash)
			current, err := os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if !current.Mode().IsRegular() || !os.SameFile(info, current) {
				return nil
			}
			if referenced {
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				sum := sha256.New()
				_, readErr := io.Copy(sum, f)
				closeErr := f.Close()
				if readErr != nil {
					return readErr
				}
				if closeErr != nil {
					return closeErr
				}
				if hex.EncodeToString(sum.Sum(nil)) != hash {
					report.Corrupt = append(report.Corrupt, hash)
				}
				return nil
			}
			if s.Now().UTC().Sub(current.ModTime()) < minAge {
				return nil
			}
			report.Orphans = append(report.Orphans, hash)
			if prune {
				// 持有与上传/删除相同的会话锁，删除前最后一次核对全部条目引用。
				referenced, err = s.Store.Referenced(ctx, conn, hash)
				if err != nil {
					return err
				}
				if referenced {
					return nil
				}
				latest, err := os.Lstat(path)
				if err != nil {
					return err
				}
				if !latest.Mode().IsRegular() || !os.SameFile(current, latest) || latest.Size() != current.Size() || !latest.ModTime().Equal(current.ModTime()) {
					return nil
				}
				if err := os.Remove(path); err != nil {
					return err
				}
				report.Removed = append(report.Removed, hash)
			}
			return nil
		}()
		if err != nil {
			return report, err
		}
	}
	return report, nil
}
