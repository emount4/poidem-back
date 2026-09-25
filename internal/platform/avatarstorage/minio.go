// Package avatarstorage provides S3-compatible storage for public avatar objects.
package avatarstorage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/emount4/poidem-back/internal/platform/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIO struct {
	client    *minio.Client
	bucket    string
	publicURL string
}

func NewMinIO(ctx context.Context, cfg config.Storage) (*MinIO, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create MinIO client: %w", err)
	}
	storage := &MinIO{client: client, bucket: cfg.Bucket, publicURL: strings.TrimRight(cfg.PublicURL, "/")}
	if err := storage.ensurePublicBucket(ctx, cfg.Region); err != nil {
		return nil, err
	}
	return storage, nil
}

func (s *MinIO) Put(ctx context.Context, key string, data []byte, contentType string) (string, error) {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("put avatar object: %w", err)
	}
	return s.publicURL + "/" + s.bucket + "/" + key, nil
}

func (s *MinIO) Delete(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete avatar object: %w", err)
	}
	return nil
}

func (s *MinIO) ensurePublicBucket(ctx context.Context, region string) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check avatar bucket: %w", err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
			return fmt.Errorf("create avatar bucket: %w", err)
		}
	}
	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{{
			"Effect": "Allow", "Principal": map[string][]string{"AWS": {"*"}},
			"Action":   []string{"s3:GetObject"},
			"Resource": []string{"arn:aws:s3:::" + s.bucket + "/*"},
		}},
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("encode avatar bucket policy: %w", err)
	}
	if err := s.client.SetBucketPolicy(ctx, s.bucket, string(encoded)); err != nil {
		return fmt.Errorf("make avatar bucket public: %w", err)
	}
	return nil
}
