package account

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type avatarStoreStub struct {
	oldKey       *string
	url, key     string
	replaceError error
	clearError   error
}

func (s *avatarStoreStub) ReplaceAvatar(_ context.Context, _ int64, url, key string) (*string, error) {
	s.url, s.key = url, key
	return s.oldKey, s.replaceError
}
func (s *avatarStoreStub) ClearAvatar(context.Context, int64) (*string, error) {
	return s.oldKey, s.clearError
}

type avatarStorageStub struct {
	putKey, contentType string
	putData             []byte
	deleted             []string
	putError            error
}

func (s *avatarStorageStub) Put(_ context.Context, key string, data []byte, contentType string) (string, error) {
	s.putKey, s.putData, s.contentType = key, append([]byte(nil), data...), contentType
	if s.putError != nil {
		return "", s.putError
	}
	return "http://minio:9000/media/" + key, nil
}
func (s *avatarStorageStub) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return nil
}

func TestAvatarUploadReplacesAndDeletesOldObject(t *testing.T) {
	oldKey := "avatars/7/old.png"
	store := &avatarStoreStub{oldKey: &oldKey}
	storage := &avatarStorageStub{}
	service, err := NewAvatarService(store, storage)
	if err != nil {
		t.Fatal(err)
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)
	url, err := service.Upload(context.Background(), 7, bytes.NewReader(png))
	if err != nil {
		t.Fatal(err)
	}
	if url != store.url || storage.contentType != "image/png" || !strings.HasPrefix(store.key, "avatars/7/") || !strings.HasSuffix(store.key, ".png") {
		t.Fatalf("url=%q key=%q type=%q", url, store.key, storage.contentType)
	}
	if len(storage.deleted) != 1 || storage.deleted[0] != oldKey {
		t.Fatalf("deleted=%v", storage.deleted)
	}
}

func TestAvatarUploadValidation(t *testing.T) {
	service, _ := NewAvatarService(&avatarStoreStub{}, &avatarStorageStub{})
	if _, err := service.Upload(context.Background(), 7, strings.NewReader("not an image")); !errors.Is(err, ErrUnsupportedAvatarType) {
		t.Fatalf("type error=%v", err)
	}
	tooLarge := bytes.NewReader(make([]byte, MaxAvatarBytes+1))
	if _, err := service.Upload(context.Background(), 7, tooLarge); !errors.Is(err, ErrAvatarTooLarge) {
		t.Fatalf("size error=%v", err)
	}
}

func TestAvatarUploadCompensatesDatabaseFailure(t *testing.T) {
	store := &avatarStoreStub{replaceError: errors.New("database unavailable")}
	storage := &avatarStorageStub{}
	service, _ := NewAvatarService(store, storage)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 16)...)
	if _, err := service.Upload(context.Background(), 7, bytes.NewReader(png)); !errors.Is(err, ErrAvatarUpload) {
		t.Fatalf("error=%v", err)
	}
	if len(storage.deleted) != 1 || storage.deleted[0] != storage.putKey {
		t.Fatalf("new object was not cleaned up: %v", storage.deleted)
	}
}

func TestAvatarDeleteClearsProfileBeforeObject(t *testing.T) {
	oldKey := "avatars/7/old.webp"
	storage := &avatarStorageStub{}
	service, _ := NewAvatarService(&avatarStoreStub{oldKey: &oldKey}, storage)
	if err := service.Delete(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if len(storage.deleted) != 1 || storage.deleted[0] != oldKey {
		t.Fatalf("deleted=%v", storage.deleted)
	}
}
