package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
)

type ossStore struct {
	client *oss.Client
	bucket string
	prefix string
}

func newOSSStore(region, bucket, prefix string) (*ossStore, error) {
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(region) || bucket == "" || prefix == "" || strings.Contains(prefix, "..") || strings.HasPrefix(prefix, "/") {
		return nil, errors.New("invalid archive region, bucket or prefix")
	}
	cfg := oss.LoadDefaultConfig().WithRegion(region).WithEndpoint("https://oss-" + region + "-internal.aliyuncs.com").WithCredentialsProvider(credentials.NewEcsRoleCredentialsProvider()).WithConnectTimeout(5 * time.Second).WithReadWriteTimeout(time.Minute).WithRetryMaxAttempts(3)
	return &ossStore{client: oss.NewClient(cfg), bucket: bucket, prefix: strings.TrimSuffix(prefix, "/") + "/"}, nil
}

func (s *ossStore) Put(ctx context.Context, key string, data []byte) error {
	if !strings.HasPrefix(key, s.prefix) {
		return errors.New("object key outside configured archive prefix")
	}
	_, err := s.client.PutObject(ctx, &oss.PutObjectRequest{
		Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key), Body: bytes.NewReader(data),
		ContentLength: oss.Ptr(int64(len(data))), ServerSideEncryption: oss.Ptr("AES256"),
		ContentType: oss.Ptr("application/octet-stream"), ForbidOverwrite: oss.Ptr("true"),
		TrafficLimit: 64 << 20,
	})
	if err != nil {
		// Credential-provider errors can include raw metadata responses. Never log
		// or return SDK error strings, which could expose temporary credentials.
		var serviceError *oss.ServiceError
		if errors.As(err, &serviceError) && serviceError.Code == "FileAlreadyExists" {
			return nil
		}
		return errors.New("OSS upload failed; SDK details suppressed")
	}
	return nil
}

func (s *ossStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if !strings.HasPrefix(key, s.prefix) {
		return nil, errors.New("object key outside configured archive prefix")
	}
	r, err := s.client.GetObject(ctx, &oss.GetObjectRequest{Bucket: oss.Ptr(s.bucket), Key: oss.Ptr(key), TrafficLimit: 64 << 20})
	if err != nil {
		return nil, errors.New("OSS download failed; SDK details suppressed")
	}
	return r.Body, nil
}
