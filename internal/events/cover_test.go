package events

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

type coverStoreStub struct {
	created  CoverUpload
	typeName string
	size     int64
	expired  []CoverUpload
	deleted  int64
}

func (s *coverStoreStub) CreateCoverUpload(_ context.Context, upload CoverUpload, contentType string, size int64) (CoverUpload, error) {
	upload.ID = 10
	s.created, s.typeName, s.size = upload, contentType, size
	return upload, nil
}
func (s *coverStoreStub) ExpiredCoverUploads(context.Context, time.Time, int) ([]CoverUpload, error) {
	return s.expired, nil
}
func (s *coverStoreStub) DeleteCoverUpload(_ context.Context, id int64) error {
	s.deleted = id
	return nil
}

type coverStorageStub struct {
	putKey     string
	deletedKey string
	err        error
}

func (s *coverStorageStub) Put(_ context.Context, key string, _ []byte, _ string) (string, error) {
	s.putKey = key
	if s.err != nil {
		return "", s.err
	}
	return "http://localhost/media/" + key, nil
}
func (s *coverStorageStub) Delete(_ context.Context, key string) error {
	s.deletedKey = key
	return s.err
}

func TestCoverUploadValidatesAndPersistsOwnership(t *testing.T) {
	store := &coverStoreStub{}
	storage := &coverStorageStub{}
	service, err := NewCoverService(store, storage)
	if err != nil {
		t.Fatal(err)
	}
	data := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, bytes.Repeat([]byte{0}, 100)...)
	upload, err := service.Upload(context.Background(), 7, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if upload.ID != 10 || store.created.OwnerID != 7 || store.typeName != "image/png" || store.size != int64(len(data)) {
		t.Fatalf("unexpected upload: %+v type=%s size=%d", upload, store.typeName, store.size)
	}
	if storage.putKey == "" {
		t.Fatal("object was not stored")
	}
}

func TestCoverUploadRejectsUnsupportedType(t *testing.T) {
	service, _ := NewCoverService(&coverStoreStub{}, &coverStorageStub{})
	_, err := service.Upload(context.Background(), 7, bytes.NewBufferString("plain text"))
	if !errors.Is(err, ErrUnsupportedCoverType) {
		t.Fatalf("got %v", err)
	}
}
