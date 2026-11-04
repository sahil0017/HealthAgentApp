package objects

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Store struct {
	client        *minio.Client
	presignClient *minio.Client
	bucket        string
}

func Connect(ctx context.Context) (*Store, error) {
	endpoint := env("S3_ENDPOINT", "objectstore:9000")
	accessKey, secretKey := env("S3_ACCESS_KEY", "cortex"), env("S3_SECRET_KEY", "local-development-only")
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		return nil, err
	}
	presignClient, err := minio.New(env("S3_PUBLIC_ENDPOINT", "localhost:9000"), &minio.Options{
		Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: env("S3_PUBLIC_SECURE", "false") == "true", Region: env("S3_REGION", "us-east-1"),
	})
	if err != nil {
		return nil, err
	}
	bucket := env("S3_BUCKET", "cortex-private")
	deadline, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for {
		exists, err := client.BucketExists(deadline, bucket)
		if err == nil {
			if !exists {
				if err = client.MakeBucket(deadline, bucket, minio.MakeBucketOptions{}); err != nil {
					if code := minio.ToErrorResponse(err).Code; code != "BucketAlreadyOwnedByYou" && code != "BucketAlreadyExists" {
						return nil, err
					}
				}
			}
			return &Store{client: client, presignClient: presignClient, bucket: bucket}, nil
		}
		select {
		case <-deadline.Done():
			return nil, err
		case <-time.After(time.Second):
		}
	}
}

func (s *Store) Put(ctx context.Context, key, contentType string, size int64, body io.Reader) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, body, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
}

func (s *Store) Remove(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *Store) PresignedPut(ctx context.Context, key string, expires time.Duration) (*url.URL, error) {
	return s.presignClient.PresignedPutObject(ctx, s.bucket, key, expires)
}

func (s *Store) PresignedGet(ctx context.Context, key string, expires time.Duration) (*url.URL, error) {
	return s.presignClient.PresignedGetObject(ctx, s.bucket, key, expires, nil)
}

func (s *Store) Stat(ctx context.Context, key string) (minio.ObjectInfo, error) {
	return s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
}

func (s *Store) Check(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("private object bucket is missing")
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
