package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3ContractOptions struct {
	endpoint         string
	region           string
	bucket           string
	prefix           string
	pathStyle        bool
	serverEncryption string
	accessKey        string
	secretKey        string
	sessionToken     string
	corsOrigin       string
}

func TestS3MinIOContract(t *testing.T) {
	endpoint := os.Getenv("SHAREPIPE_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("set SHAREPIPE_MINIO_ENDPOINT to run the MinIO contract test")
	}
	accessKey := os.Getenv("SHAREPIPE_MINIO_ACCESS_KEY")
	secretKey := os.Getenv("SHAREPIPE_MINIO_SECRET_KEY")
	bucket := os.Getenv("SHAREPIPE_MINIO_BUCKET")
	if accessKey == "" || secretKey == "" || bucket == "" {
		t.Fatal("MinIO contract test credentials and bucket are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	awsConfiguration, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		t.Fatal(err)
	}
	awsConfiguration.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	awsConfiguration.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	client := s3.NewFromConfig(awsConfiguration, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})
	if _, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}); err != nil {
		if _, createErr := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); createErr != nil {
			t.Fatalf("create MinIO bucket: %v (head error: %v)", createErr, err)
		}
	}
	runS3Contract(t, s3ContractOptions{
		endpoint: endpoint, region: "us-east-1", bucket: bucket,
		prefix: "contract", pathStyle: true, accessKey: accessKey, secretKey: secretKey,
	})
}

func TestS3ProviderContract(t *testing.T) {
	endpoint := os.Getenv("SHAREPIPE_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set SHAREPIPE_S3_TEST_ENDPOINT to run the provider contract test")
	}
	accessKey := os.Getenv("SHAREPIPE_S3_TEST_ACCESS_KEY_ID")
	secretKey := os.Getenv("SHAREPIPE_S3_TEST_SECRET_ACCESS_KEY")
	bucket := os.Getenv("SHAREPIPE_S3_TEST_BUCKET")
	if accessKey == "" || secretKey == "" || bucket == "" {
		t.Fatal("S3 provider contract credentials and bucket are required")
	}
	region := os.Getenv("SHAREPIPE_S3_TEST_REGION")
	if region == "" {
		region = "auto"
	}
	pathStyle := false
	if raw := os.Getenv("SHAREPIPE_S3_TEST_PATH_STYLE"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			t.Fatalf("invalid SHAREPIPE_S3_TEST_PATH_STYLE: %v", err)
		}
		pathStyle = value
	}
	runS3Contract(t, s3ContractOptions{
		endpoint: endpoint, region: region, bucket: bucket,
		prefix: os.Getenv("SHAREPIPE_S3_TEST_PREFIX"), pathStyle: pathStyle,
		serverEncryption: os.Getenv("SHAREPIPE_S3_TEST_SERVER_SIDE_ENCRYPTION"),
		accessKey:        accessKey, secretKey: secretKey,
		sessionToken: os.Getenv("SHAREPIPE_S3_TEST_SESSION_TOKEN"),
		corsOrigin:   os.Getenv("SHAREPIPE_S3_TEST_ORIGIN"),
	})
}

func runS3Contract(t *testing.T, options s3ContractOptions) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	prefix := path.Join(strings.Trim(options.prefix, "/"), "provider-contract-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	store, err := NewS3(ctx, S3Options{
		Endpoint: options.endpoint, Region: options.region, Bucket: options.bucket, Prefix: prefix,
		PathStyle: options.pathStyle, ServerSideEncryption: options.serverEncryption,
		AccessKeyID: options.accessKey, SecretAccessKey: options.secretKey, SessionToken: options.sessionToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	cleaned := false
	multipartKey := ""
	multipartUploadID := ""
	multipartCompleted := false
	t.Cleanup(func() {
		if multipartUploadID != "" && !multipartCompleted {
			abortContext, abortCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer abortCancel()
			if err := store.AbortMultipart(abortContext, multipartKey, multipartUploadID); err != nil {
				t.Errorf("abort S3 provider contract multipart upload: %v", err)
			}
		}
		if cleaned {
			return
		}
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := store.DeletePrefix(cleanupContext, "objects"); err != nil {
			t.Errorf("clean S3 provider contract prefix: %v", err)
		}
	})

	serverContent := []byte("SharePipe S3 provider server-side put")
	if _, err := store.Put(ctx, "objects/server-put.txt", bytes.NewReader(serverContent), int64(len(serverContent))); err != nil {
		t.Fatalf("put provider object: %v", err)
	}
	serverInfo, err := store.Stat(ctx, "objects/server-put.txt")
	if err != nil || serverInfo.Size != int64(len(serverContent)) {
		t.Fatalf("stat provider object: size=%d err=%v", serverInfo.Size, err)
	}

	content := []byte("SharePipe S3 provider multipart contract")
	key := "objects/sample.txt"
	uploadID, err := store.CreateMultipart(ctx, key, "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	multipartKey = key
	multipartUploadID = uploadID
	presignedPart, err := store.PresignUploadPart(ctx, key, uploadID, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if options.corsOrigin != "" {
		preflight, requestErr := http.NewRequestWithContext(ctx, http.MethodOptions, presignedPart, nil)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		preflight.Header.Set("Origin", options.corsOrigin)
		preflight.Header.Set("Access-Control-Request-Method", http.MethodPut)
		preflight.Header.Set("Access-Control-Request-Headers", "content-type")
		preflightResponse, requestErr := http.DefaultClient.Do(preflight)
		if requestErr != nil {
			t.Fatalf("S3 CORS preflight: %v", requestErr)
		}
		_, _ = io.Copy(io.Discard, preflightResponse.Body)
		preflightResponse.Body.Close()
		if preflightResponse.StatusCode != http.StatusOK && preflightResponse.StatusCode != http.StatusNoContent {
			t.Fatalf("unexpected S3 CORS preflight status: %s", preflightResponse.Status)
		}
		if allowed := preflightResponse.Header.Get("Access-Control-Allow-Origin"); allowed != options.corsOrigin {
			t.Fatalf("unexpected S3 CORS allow origin: %q", allowed)
		}
		if methods := preflightResponse.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(methods, http.MethodPut) {
			t.Fatalf("S3 CORS does not allow PUT: %q", methods)
		}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, presignedPart, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if options.corsOrigin != "" {
		request.Header.Set("Origin", options.corsOrigin)
		request.Header.Set("Content-Type", "text/plain")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("ETag") == "" {
		t.Fatalf("unexpected multipart upload response: %s, ETag %q", response.Status, response.Header.Get("ETag"))
	}
	if options.corsOrigin != "" {
		if allowed := response.Header.Get("Access-Control-Allow-Origin"); allowed != options.corsOrigin {
			t.Fatalf("unexpected upload CORS allow origin: %q", allowed)
		}
		if exposed := strings.ToLower(response.Header.Get("Access-Control-Expose-Headers")); !strings.Contains(exposed, "etag") {
			t.Fatalf("S3 CORS does not expose ETag: %q", exposed)
		}
	}
	info, err := store.CompleteMultipart(ctx, key, uploadID, []MultipartPart{{PartNumber: 1, ETag: response.Header.Get("ETag")}})
	if err != nil {
		t.Fatal(err)
	}
	multipartCompleted = true
	if info.Size != int64(len(content)) {
		t.Fatalf("unexpected completed size: %d", info.Size)
	}

	object, err := store.Open(ctx, key, &ByteRange{Start: 10, End: 11})
	if err != nil {
		t.Fatal(err)
	}
	partial, readErr := io.ReadAll(object.Body)
	object.Body.Close()
	if readErr != nil || string(partial) != "S3" || object.Size != int64(len(content)) {
		t.Fatalf("unexpected ranged object: %q size=%d err=%v", partial, object.Size, readErr)
	}

	downloadURL, err := store.PresignGet(ctx, key, "sample.txt", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	downloadRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if options.corsOrigin != "" {
		downloadRequest.Header.Set("Origin", options.corsOrigin)
	}
	downloadResponse, err := http.DefaultClient.Do(downloadRequest)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, readErr := io.ReadAll(downloadResponse.Body)
	downloadResponse.Body.Close()
	if readErr != nil || downloadResponse.StatusCode != http.StatusOK || !bytes.Equal(downloaded, content) {
		t.Fatalf("unexpected presigned download: %s %q %v", downloadResponse.Status, downloaded, readErr)
	}
	if options.corsOrigin != "" && downloadResponse.Header.Get("Access-Control-Allow-Origin") != options.corsOrigin {
		t.Fatalf("unexpected download CORS allow origin: %q", downloadResponse.Header.Get("Access-Control-Allow-Origin"))
	}

	if err := store.DeletePrefix(ctx, "objects"); err != nil {
		t.Fatal(err)
	}
	cleaned = true
	if _, err := store.Stat(ctx, key); err == nil {
		t.Fatal("object should be deleted after prefix cleanup")
	}
}
