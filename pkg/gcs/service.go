package gcs

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"cloud.google.com/go/storage"
)

type GCSClient struct {
	client     *storage.Client
	bucketName string
}

func NewGCSClient(ctx context.Context, bucketName string) (*GCSClient, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create gcs client: %v", err)
	}

	return &GCSClient{
		client:     client,
		bucketName: bucketName,
	}, nil
}

func (g *GCSClient) Upload(ctx context.Context, objectPath string, content io.Reader) (string, error) {
	bucket := g.client.Bucket(g.bucketName)
	obj := bucket.Object(objectPath)

	writer := obj.NewWriter(ctx)
	if _, err := io.Copy(writer, content); err != nil {
		return "", fmt.Errorf("failed to copy content: %v", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close writer: %v", err)
	}

	return fmt.Sprintf("https://storage.googleapis.com/%s/%s", g.bucketName, objectPath), nil
}

func (g *GCSClient) Delete(ctx context.Context, gcsURL string) error {

	// Extract object path from GCS URL (format: gs://bucket-name/object-path)
	if len(gcsURL) < 5 || gcsURL[:5] != "gs://" {
		return fmt.Errorf("invalid GCS URL format: %s", gcsURL)
	}

	// Remove "gs://" prefix
	urlWithoutPrefix := gcsURL[5:]

	// Find the first "/" after bucket name
	slashIndex := strings.Index(urlWithoutPrefix, "/")
	if slashIndex == -1 {
		return fmt.Errorf("invalid GCS URL format, no object path: %s", gcsURL)
	}

	bucketName := urlWithoutPrefix[:slashIndex]
	objectPath := urlWithoutPrefix[slashIndex+1:] // Get everything after the first slash

	// delete obj from bucket
	bucket := g.client.Bucket(bucketName)
	obj := bucket.Object(objectPath)

	if err := obj.Delete(ctx); err != nil {
		if err == storage.ErrObjectNotExist {
			return nil
		}
		return fmt.Errorf("failed to delete object: %v", err)
	}

	return nil
}

func (g *GCSClient) GetPresignedURL(ctx context.Context, gcsURI string, expiresAt time.Time) (string, error) {
	opts := &storage.SignedURLOptions{
		Scheme:  storage.SigningSchemeV4,
		Method:  "GET",
		Expires: expiresAt,
	}

	bucketName := strings.TrimPrefix(gcsURI, "gs://")
	bucketName = strings.Split(bucketName, "/")[0]
	objectPath := strings.TrimPrefix(gcsURI, "gs://"+bucketName+"/")

	url, err := g.client.Bucket(bucketName).SignedURL(objectPath, opts)
	if err != nil {
		return "", fmt.Errorf("failed to get presigned url: %v", err)
	}
	return url, nil
}

func (g *GCSClient) Close() error {
	return g.client.Close()
}
