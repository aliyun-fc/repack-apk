package main

import (
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// Store ...
type Store interface {
	GetObject(objectKey string, options ...oss.Option) (io.ReadCloser, error)
	GetObjectDetailedMeta(objectKey string, options ...oss.Option) (http.Header, error)
	PutObject(objectKey string, reader io.Reader, options ...oss.Option) error
	InitiateMultipartUpload(objectKey string, options ...oss.Option) (oss.InitiateMultipartUploadResult, error)
	UploadPartCopy(imur oss.InitiateMultipartUploadResult, srcBucketName, srcObjectKey string,
		startPosition, partSize int64, partNumber int, options ...oss.Option) (oss.UploadPart, error)
	UploadPart(imur oss.InitiateMultipartUploadResult, reader io.Reader,
		partSize int64, partNumber int, options ...oss.Option) (oss.UploadPart, error)
	CompleteMultipartUpload(imur oss.InitiateMultipartUploadResult,
		parts []oss.UploadPart) (oss.CompleteMultipartUploadResult, error)
}

// StoreWithRetry ...
type StoreWithRetry struct {
	ossBucket *oss.Bucket
}

// NewStoreWithRetry ...
func NewStoreWithRetry(ossBucket *oss.Bucket) Store {
	return &StoreWithRetry{
		ossBucket: ossBucket,
	}
}

type backoff struct {
	delay time.Duration
	i     int
	max   int
}

func newBackoff() *backoff {
	return &backoff{
		delay: 50 * time.Millisecond,
		i:     0,
		max:   8,
	}
}

func (b *backoff) next() time.Duration {
	if b.i >= b.max {
		return 0
	}

	b.i++
	b.delay *= 2
	return b.delay
}

func (s *StoreWithRetry) retry(f func() error) error {
	b := newBackoff()
	for {
		err := f()
		if err == nil {
			return nil
		}

		log.Printf("retry error: %s", err.Error())
		if se, ok := err.(oss.ServiceError); ok && se.StatusCode == 503 {
			delay := b.next()
			if delay == time.Duration(0) {
				return err
			}
			time.Sleep(delay)
		} else if strings.Contains(err.Error(), "503") {
			delay := b.next()
			if delay == time.Duration(0) {
				return err
			}
			time.Sleep(delay)
		} else {
			return err
		}
	}
}

// GetObject ...
func (s *StoreWithRetry) GetObject(objectKey string, options ...oss.Option) (resp io.ReadCloser, err error) {
	s.retry(func() error {
		resp, err = s.ossBucket.GetObject(objectKey, options...)
		return err
	})

	return
}

// GetObjectDetailedMeta ...
func (s *StoreWithRetry) GetObjectDetailedMeta(
	objectKey string, options ...oss.Option) (resp http.Header, err error) {
	s.retry(func() error {
		resp, err = s.ossBucket.GetObjectDetailedMeta(objectKey, options...)
		return err
	})

	return
}

// PutObject ...
func (s *StoreWithRetry) PutObject(objectKey string, reader io.Reader, options ...oss.Option) (err error) {
	s.retry(func() error {
		err = s.ossBucket.PutObject(objectKey, reader, options...)
		return err
	})

	return
}

// InitiateMultipartUpload ...
func (s *StoreWithRetry) InitiateMultipartUpload(
	objectKey string, options ...oss.Option) (resp oss.InitiateMultipartUploadResult, err error) {
	s.retry(func() error {
		resp, err = s.ossBucket.InitiateMultipartUpload(objectKey, options...)
		return err
	})

	return
}

// UploadPartCopy ...
func (s *StoreWithRetry) UploadPartCopy(
	imur oss.InitiateMultipartUploadResult, srcBucketName, srcObjectKey string,
	startPosition, partSize int64, partNumber int, options ...oss.Option) (resp oss.UploadPart, err error) {
	s.retry(func() error {
		resp, err = s.ossBucket.UploadPartCopy(
			imur, srcBucketName, srcObjectKey, startPosition, partSize, partNumber, options...)
		return err
	})

	return
}

// UploadPart ...
func (s *StoreWithRetry) UploadPart(imur oss.InitiateMultipartUploadResult, reader io.Reader,
	partSize int64, partNumber int, options ...oss.Option) (resp oss.UploadPart, err error) {
	s.retry(func() error {
		resp, err = s.ossBucket.UploadPart(
			imur, reader, partSize, partNumber, options...)
		return err
	})

	return
}

// CompleteMultipartUpload ...
func (s *StoreWithRetry) CompleteMultipartUpload(imur oss.InitiateMultipartUploadResult,
	parts []oss.UploadPart) (resp oss.CompleteMultipartUploadResult, err error) {
	s.retry(func() error {
		resp, err = s.ossBucket.CompleteMultipartUpload(imur, parts)
		return err
	})

	return
}
