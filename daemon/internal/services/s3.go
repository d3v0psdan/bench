package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// s3Region is what the .env block tells Laravel (AWS_DEFAULT_REGION).
const s3Region = "us-east-1"

// emptySHA256 is the SHA-256 of an empty body (S3 wants it spelled out).
const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// ensureBucket creates bucket on the S3 endpoint unless it already exists,
// so the AWS_BUCKET in the .env block works without a console visit.
// Shortcut: a hand-rolled SigV4 for one bodiless request; pull in an S3
// client only if Bench ever needs more of the API.
func ensureBucket(ctx context.Context, endpoint, accessKey, secretKey, bucket string) error {
	// Storage can lag readiness briefly after a start; 503 means "not yet".
	for range bucketAttempts - 1 {
		err := putBucket(ctx, endpoint, accessKey, secretKey, bucket)
		if !errors.Is(err, errNotReady) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return putBucket(ctx, endpoint, accessKey, secretKey, bucket)
}

// bucketAttempts × 500ms is how long ensureBucket waits for storage.
const bucketAttempts = 20

var errNotReady = errors.New("storage not ready")

func putBucket(ctx context.Context, endpoint, accessKey, secretKey, bucket string) error {
	u, err := url.Parse(endpoint + "/" + bucket)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), nil)
	if err != nil {
		return err
	}
	signV4(req, accessKey, secretKey, time.Now().UTC())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("creating bucket %q: %w", bucket, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	switch {
	case resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusConflict && strings.Contains(string(body), "BucketAlreadyOwnedByYou"):
		return nil
	case resp.StatusCode == http.StatusServiceUnavailable:
		return fmt.Errorf("creating bucket %q: %w: %s", bucket, errNotReady, strings.TrimSpace(string(body)))
	default:
		return fmt.Errorf("creating bucket %q: %s: %s", bucket, resp.Status, strings.TrimSpace(string(body)))
	}
}

// signV4 adds AWS Signature Version 4 headers for a bodiless S3 request.
func signV4(req *http.Request, accessKey, secretKey string, now time.Time) {
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", emptySHA256)

	const signed = "host;x-amz-content-sha256;x-amz-date"
	canonical := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		"host:" + req.URL.Host + "\n" + "x-amz-content-sha256:" + emptySHA256 + "\n" + "x-amz-date:" + amzDate + "\n",
		signed,
		emptySHA256,
	}, "\n")
	scope := day + "/" + s3Region + "/s3/aws4_request"
	hashed := sha256.Sum256([]byte(canonical))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(hashed[:])

	key := hmacSHA256([]byte("AWS4"+secretKey), day)
	key = hmacSHA256(key, s3Region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(key, toSign))

	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+
		", SignedHeaders="+signed+", Signature="+sig)
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
