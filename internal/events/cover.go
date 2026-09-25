package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

const MaxCoverBytes int64 = 10 * 1024 * 1024

var (
	ErrCoverTooLarge        = errors.New("event cover too large")
	ErrUnsupportedCoverType = errors.New("unsupported event cover type")
	ErrCoverUpload          = errors.New("event cover upload failed")
	ErrCoverUnavailable     = errors.New("event cover storage unavailable")
)

type CoverUpload struct {
	ID        int64
	OwnerID   int64
	EventID   *int64
	ObjectKey string
	PublicURL string
	CreatedAt time.Time
}

type CoverUploadStore interface {
	CreateCoverUpload(context.Context, CoverUpload, string, int64) (CoverUpload, error)
	ExpiredCoverUploads(context.Context, time.Time, int) ([]CoverUpload, error)
	DeleteCoverUpload(context.Context, int64) error
}

type CoverStorage interface {
	Put(context.Context, string, []byte, string) (string, error)
	Delete(context.Context, string) error
}

type CoverService struct {
	store   CoverUploadStore
	storage CoverStorage
	now     func() time.Time
}

func NewCoverService(store CoverUploadStore, storage CoverStorage) (*CoverService, error) {
	if store == nil || storage == nil {
		return nil, errors.New("cover dependencies are required")
	}
	return &CoverService{store: store, storage: storage, now: time.Now}, nil
}

func (s *CoverService) Upload(ctx context.Context, ownerID int64, source io.Reader) (CoverUpload, error) {
	if ownerID <= 0 {
		return CoverUpload{}, ErrCoverUpload
	}
	data, err := io.ReadAll(io.LimitReader(source, MaxCoverBytes+1))
	if err != nil {
		return CoverUpload{}, fmt.Errorf("%w: read cover: %v", ErrCoverUpload, err)
	}
	if len(data) == 0 {
		return CoverUpload{}, ErrUnsupportedCoverType
	}
	if int64(len(data)) > MaxCoverBytes {
		return CoverUpload{}, ErrCoverTooLarge
	}
	contentType := http.DetectContentType(data)
	extension, ok := coverExtension(contentType)
	if !ok {
		return CoverUpload{}, ErrUnsupportedCoverType
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return CoverUpload{}, fmt.Errorf("%w: generate key: %v", ErrCoverUpload, err)
	}
	key := "events/temp/" + strconv.FormatInt(ownerID, 10) + "/" + hex.EncodeToString(random) + extension
	publicURL, err := s.storage.Put(ctx, key, data, contentType)
	if err != nil {
		return CoverUpload{}, fmt.Errorf("%w: %v", ErrCoverUnavailable, err)
	}
	upload, err := s.store.CreateCoverUpload(ctx, CoverUpload{
		OwnerID: ownerID, ObjectKey: key, PublicURL: publicURL, CreatedAt: s.now(),
	}, contentType, int64(len(data)))
	if err != nil {
		_ = s.storage.Delete(ctx, key)
		return CoverUpload{}, fmt.Errorf("%w: persist upload: %v", ErrCoverUpload, err)
	}
	return upload, nil
}

func coverExtension(contentType string) (string, bool) {
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

type CoverCleanupWorker struct {
	store    CoverUploadStore
	storage  CoverStorage
	log      *slog.Logger
	interval time.Duration
	maxAge   time.Duration
	batch    int
	now      func() time.Time
}

func NewCoverCleanupWorker(store CoverUploadStore, storage CoverStorage, log *slog.Logger) *CoverCleanupWorker {
	return &CoverCleanupWorker{
		store: store, storage: storage, log: log,
		interval: time.Hour, maxAge: 24 * time.Hour, batch: 100, now: time.Now,
	}
}

func (w *CoverCleanupWorker) Run(ctx context.Context) {
	if w == nil || w.store == nil || w.storage == nil {
		return
	}
	w.cleanup(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.cleanup(ctx)
		}
	}
}

func (w *CoverCleanupWorker) cleanup(ctx context.Context) {
	uploads, err := w.store.ExpiredCoverUploads(ctx, w.now().Add(-w.maxAge), w.batch)
	if err != nil {
		w.log.Error("list expired event covers", "error", err)
		return
	}
	for _, upload := range uploads {
		if err := w.storage.Delete(ctx, upload.ObjectKey); err != nil {
			w.log.Error("delete expired event cover", "upload_id", upload.ID, "error", err)
			continue
		}
		if err := w.store.DeleteCoverUpload(ctx, upload.ID); err != nil {
			w.log.Error("delete expired event cover record", "upload_id", upload.ID, "error", err)
		}
	}
}
