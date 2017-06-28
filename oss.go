package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"strconv"
	"strings"
	"sync"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// consts ...
const (
	CopyPartWorkerCount   = 8
	CopyPartSizeInBytes   = 50 * 1024 * 1024
	MaxWriteBufferInBytes = 100 * 1024 * 1024
	MinPartSizeInBytes    = 100 * 1024
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

// readAll keeps reading from r until it fills the buf
func readAll(r io.Reader, buf []byte) error {
	p := 0
	for p < len(buf) {
		pBuf := buf[p:]
		n, err := r.Read(pBuf)
		if n > 0 {
			p += n
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if p != len(buf) {
		return fmt.Errorf("expect %d bytes, got: %d", len(buf), p)
	}
	return nil
}

// ReadAt reads len(buf) bytes from OSS object at offset
func (r *Reader) ReadAt(buf []byte, off int64) (int, error) {
	resp, err := r.Client.GetObject(
		r.Object, oss.Range(off, off+int64(len(buf))-1))
	if err != nil {
		return 0, err
	}
	defer resp.Close()

	err = readAll(resp, buf)
	if err != nil {
		return 0, err
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

	srcClient *oss.Bucket
	buffer    []byte
	offset    int64
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
	srcBucketClient, _ := client.Bucket(srcBucket)

	return &Writer{
		Bucket:    bucket,
		Object:    object,
		SrcBucket: srcBucket,
		SrcObject: srcObject,
		Client:    bucketClient,
		srcClient: srcBucketClient,
		offset:    offset,
	}, nil
}

// Writer ...
func (w *Writer) Write(buf []byte) (int, error) {
	w.buffer = append(w.buffer, buf...)
	if len(w.buffer) > MaxWriteBufferInBytes {
		log.Printf("max writer buffer exceeded: %d", len(w.buffer))
	}
	return len(buf), nil
}

// Flush writes the target object:
// 1. initiate a multipart upload
// 2. copy the content before w.offset to the target
// 3. upload the newly written w.buffer
// 4. complete the multipart upload
func (w *Writer) Flush() error {
	// don't use multipart if the size is too small
	if w.offset < MinPartSizeInBytes {
		log.Printf("small object: %d", w.offset)

		resp, err := w.srcClient.GetObject(w.SrcObject, oss.Range(0, w.offset-1))
		if err != nil {
			return err
		}
		defer resp.Close()
		buf, err := ioutil.ReadAll(resp)
		if err != nil {
			return err
		}
		w.buffer = append(buf, w.buffer...)
		return w.Client.PutObject(w.Object, bytes.NewReader(w.buffer))
	}

	log.Printf("begin multipart copy, size: %d", w.offset)

	up, err := w.Client.InitiateMultipartUpload(w.Object)
	if err != nil {
		return err
	}

	// determine number of parts
	numParts := w.offset / CopyPartSizeInBytes
	if numParts <= 0 {
		numParts = 1
	}
	// avoid the last part < 100KB
	if w.offset%CopyPartSizeInBytes <= MinPartSizeInBytes {
		numParts--
	}

	// prepare all parts
	type partDesc struct {
		index int64
		start int64
		size  int64
	}
	partsChan := make(chan partDesc, numParts)
	for i := int64(0); i < numParts; i++ {
		start := i * CopyPartSizeInBytes
		size := int64(CopyPartSizeInBytes)
		if i == numParts-1 {
			size = w.offset - start
		}
		partsChan <- partDesc{
			index: i + 1,
			start: start,
			size:  size,
		}
	}
	close(partsChan)

	// parallelly copy part and gather all results
	type resultDesc struct {
		part oss.UploadPart
		err  error
	}
	resChan := make(chan resultDesc, numParts)

	var wg sync.WaitGroup
	wg.Add(CopyPartWorkerCount)
	for i := 0; i < CopyPartWorkerCount; i++ {
		go func() {
			defer wg.Done()
			for p := range partsChan {
				part, err := w.Client.UploadPartCopy(
					up, w.SrcBucket, w.SrcObject, p.start, p.size, int(p.index))
				resChan <- resultDesc{
					part: part,
					err:  err,
				}
			}
		}()
	}
	wg.Wait()
	close(resChan)

	// check if any parts fail
	parts := []oss.UploadPart{}
	for r := range resChan {
		if r.err != nil {
			return err
		}
		parts = append(parts, r.part)
	}

	finalPart, err := w.Client.UploadPart(
		up, strings.NewReader(string(w.buffer)),
		int64(len(w.buffer)), int(numParts+1))
	if err != nil {
		return err
	}
	parts = append(parts, finalPart)

	_, err = w.Client.CompleteMultipartUpload(up, parts)
	return err
}
