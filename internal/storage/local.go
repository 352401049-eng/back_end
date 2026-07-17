package storage

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yujixinjiang/backend/internal/config"
)

var (
	ErrFileTooLarge   = errors.New("file too large")
	ErrInvalidFileType = errors.New("invalid file type")
)

type UploadResult struct {
	Path     string `json:"path"`      // 相对路径 2026/06/30/abc.jpg
	URL      string `json:"url"`       // /uploads/2026/06/30/abc.jpg
	FullURL  string `json:"full_url"`  // http://host/uploads/...
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type LocalStorage struct {
	cfg config.UploadConfig
}

func NewLocal(cfg config.UploadConfig) *LocalStorage {
	return &LocalStorage{cfg: cfg}
}

func (s *LocalStorage) EnsureDir() error {
	return os.MkdirAll(s.cfg.Dir, 0o755)
}

func (s *LocalStorage) Save(file *multipart.FileHeader) (*UploadResult, error) {
	maxBytes := int64(s.cfg.MaxSizeMB) * 1024 * 1024
	if file.Size > maxBytes {
		return nil, ErrFileTooLarge
	}

	src, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	ext, err := detectImageExt(src, file.Filename)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	relDir := filepath.Join(
		fmt.Sprintf("%04d", now.Year()),
		fmt.Sprintf("%02d", int(now.Month())),
		fmt.Sprintf("%02d", now.Day()),
	)
	if err := os.MkdirAll(filepath.Join(s.cfg.Dir, relDir), 0o755); err != nil {
		return nil, err
	}

	name := randomName() + ext
	relPath := filepath.Join(relDir, name)
	absPath := filepath.Join(s.cfg.Dir, relPath)

	dst, err := os.Create(absPath)
	if err != nil {
		return nil, err
	}
	defer dst.Close()

	if _, err := src.Seek(0, io.SeekStart); err != nil {
		os.Remove(absPath)
		return nil, err
	}
	written, err := io.Copy(dst, io.LimitReader(src, maxBytes+1))
	if err != nil {
		os.Remove(absPath)
		return nil, err
	}
	if written > maxBytes {
		os.Remove(absPath)
		return nil, ErrFileTooLarge
	}

	urlPath := s.cfg.URLPrefix + "/" + filepath.ToSlash(relPath)
	return &UploadResult{
		Path:     filepath.ToSlash(relPath),
		URL:      urlPath,
		FullURL:  s.cfg.PublicURL(relPath),
		Filename: name,
		Size:     written,
	}, nil
}

func detectImageExt(r io.ReadSeeker, filename string) (string, error) {
	head := make([]byte, 512)
	n, _ := r.Read(head)
	contentType := http.DetectContentType(head[:n])
	_, _ = r.Seek(0, io.SeekStart)

	switch contentType {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "image/gif":
		return ".gif", nil
	case "image/webp":
		return ".webp", nil
	}

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return ext, nil
	default:
		return "", ErrInvalidFileType
	}
}

func randomName() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
