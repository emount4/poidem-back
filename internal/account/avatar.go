package account

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

const MaxAvatarBytes int64 = 5_242_880

var (
	ErrAvatarTooLarge           = errors.New("avatar too large")
	ErrUnsupportedAvatarType    = errors.New("unsupported avatar type")
	ErrAvatarUpload             = errors.New("avatar upload failed")
	ErrAvatarStorageUnavailable = errors.New("avatar storage unavailable")
)

type AvatarStore interface {
	ReplaceAvatar(context.Context, int64, string, string) (*string, error)
	ClearAvatar(context.Context, int64) (*string, error)
}

type AvatarStorage interface {
	Put(context.Context, string, []byte, string) (string, error)
	Delete(context.Context, string) error
}

type AvatarService struct {
	store   AvatarStore
	storage AvatarStorage
}

func NewAvatarService(store AvatarStore, storage AvatarStorage) (*AvatarService, error) {
	if store == nil || storage == nil {
		return nil, errors.New("avatar dependencies are required")
	}
	return &AvatarService{store: store, storage: storage}, nil
}

func (s *AvatarService) Upload(ctx context.Context, userID int64, source io.Reader) (string, error) {
	if userID <= 0 {
		return "", ErrUserNotFound
	}
	data, err := io.ReadAll(io.LimitReader(source, MaxAvatarBytes+1))
	if err != nil {
		return "", fmt.Errorf("%w: read avatar: %v", ErrAvatarUpload, err)
	}
	if int64(len(data)) > MaxAvatarBytes {
		return "", ErrAvatarTooLarge
	}
	contentType := http.DetectContentType(data)
	extension, supported := avatarExtension(contentType)
	if !supported {
		return "", ErrUnsupportedAvatarType
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("%w: generate object key: %v", ErrAvatarUpload, err)
	}
	key := "avatars/" + strconv.FormatInt(userID, 10) + "/" + hex.EncodeToString(random) + extension
	publicURL, err := s.storage.Put(ctx, key, data, contentType)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrAvatarStorageUnavailable, err)
	}
	oldKey, err := s.store.ReplaceAvatar(ctx, userID, publicURL, key)
	if err != nil {
		_ = s.storage.Delete(ctx, key)
		return "", fmt.Errorf("%w: persist avatar: %v", ErrAvatarUpload, err)
	}
	if oldKey != nil && *oldKey != "" && *oldKey != key {
		_ = s.storage.Delete(ctx, *oldKey)
	}
	return publicURL, nil
}

func (s *AvatarService) Delete(ctx context.Context, userID int64) error {
	if userID <= 0 {
		return ErrUserNotFound
	}
	oldKey, err := s.store.ClearAvatar(ctx, userID)
	if err != nil {
		return fmt.Errorf("%w: clear avatar: %v", ErrAvatarUpload, err)
	}
	if oldKey != nil && *oldKey != "" {
		_ = s.storage.Delete(ctx, *oldKey)
	}
	return nil
}

func avatarExtension(contentType string) (string, bool) {
	switch contentType {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/webp":
		return ".webp", true
	default:
		return "", false
	}
}
