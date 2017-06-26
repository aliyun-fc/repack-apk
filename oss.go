package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"strconv"
	"strings"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// Reader implements io.ReaderAt and reads from OSS object
type Reader struct {
	Bucket string
	Object string
	Client *oss.Bucket
}

// OSSConfig ...
type OSSConfig struct {
	Endpoint        string
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
}

// NewReader ...
func NewReader(config OSSConfig, location string) (*Reader, error) {
	client, err := oss.New(
		config.Endpoint, config.AccessKeyID, config.AccessKeySecret,
		oss.SecurityToken(config.SecurityToken))

	if err != nil {
		return nil, err
	}

	bucketAndObject := strings.SplitN(location, "/", 2)
	if len(bucketAndObject) != 2 {
		return nil, fmt.Errorf("Invalid location: %s", location)
	}

	bucket, object := bucketAndObject[0], bucketAndObject[1]
	bucketClient, _ := client.Bucket(bucket)

	return &Reader{
		Bucket: bucket,
		Object: object,
		Client: bucketClient,
	}, nil
}

// ReadAt reads len(buf) bytes from OSS object at offset
func (r *Reader) ReadAt(buf []byte, off int64) (int, error) {
	resp, err := r.Client.GetObject(
		r.Object, oss.Range(off, off+int64(len(buf))-1))
	if err != nil {
		return 0, err
	}
	defer resp.Close()

	// TODO: ReadAll may cause OOM here
	all, err := ioutil.ReadAll(resp)
	if err != nil && err != io.EOF {
		return 0, err
	}
	// TODO: avoid copy here
	if copy(buf, all) != len(buf) {
		return 0, fmt.Errorf("expect %d bytes, got %d", len(buf), len(all))
	}
	return len(buf), nil
}

// Size returns the object size
func (r *Reader) Size() (int64, error) {
	resp, err := r.Client.GetObjectDetailedMeta(r.Object)
	if err != nil {
		return 0, err
	}

	contentLength := resp.Get("Content-Length")
	if len(contentLength) == 0 {
		return 0, fmt.Errorf("empty content length")
	}

	return strconv.ParseInt(contentLength, 10, 64)
}

// Writer implements io.Writer and writes to OSS object
type Writer struct {
	Bucket    string
	Object    string
	SrcBucket string
	SrcObject string
	Client    *oss.Bucket

	// TODO: limit the memory usage of buffer
	buffer []byte
	offset int64
}

// NewWriter ...
func NewWriter(config OSSConfig, location, srcLocation string, offset int64) (*Writer, error) {
	client, err := oss.New(
		config.Endpoint, config.AccessKeyID, config.AccessKeySecret,
		oss.SecurityToken(config.SecurityToken))

	if err != nil {
		return nil, err
	}

	bucketAndObject := strings.SplitN(location, "/", 2)
	if len(bucketAndObject) != 2 {
		return nil, fmt.Errorf("Invalid location: %s", location)
	}

	bucket, object := bucketAndObject[0], bucketAndObject[1]
	bucketClient, _ := client.Bucket(bucket)

	bucketAndObject = strings.SplitN(srcLocation, "/", 2)
	if len(bucketAndObject) != 2 {
		return nil, fmt.Errorf("Invalid location: %s", srcLocation)
	}
	srcBucket, srcObject := bucketAndObject[0], bucketAndObject[1]

	return &Writer{
		Bucket:    bucket,
		Object:    object,
		SrcBucket: srcBucket,
		SrcObject: srcObject,
		Client:    bucketClient,
		offset:    offset,
	}, nil
}

// Writer ...
func (w *Writer) Write(buf []byte) (int, error) {
	w.buffer = append(w.buffer, buf...)
	return len(buf), nil
}

// Flush writes the target object:
// 1. initiate a multipart upload
// 2. copy the content before w.offset to the target
// 3. upload the newly written w.buffer
// 4. complete the multipart upload
func (w *Writer) Flush() error {
	up, err := w.Client.InitiateMultipartUpload(w.Object)
	if err != nil {
		return err
	}

	// TODO: part1 may be too large, split it into multiple parts
	part1, err := w.Client.UploadPartCopy(
		up, w.SrcBucket, w.SrcObject, 0, w.offset, 1)
	if err != nil {
		return err
	}

	part2, err := w.Client.UploadPart(
		up, strings.NewReader(string(w.buffer)),
		int64(len(w.buffer)), 2)
	if err != nil {
		return err
	}

	_, err = w.Client.CompleteMultipartUpload(
		up, []oss.UploadPart{part1, part2})
	return err
}
