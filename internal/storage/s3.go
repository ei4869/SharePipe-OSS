package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

type S3Options struct {
	Endpoint             string
	Region               string
	Bucket               string
	Prefix               string
	PathStyle            bool
	ServerSideEncryption string
	AccessKeyID          string
	SecretAccessKey      string
	SessionToken         string
}

type S3 struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
	prefix    string
	sse       types.ServerSideEncryption
}

func NewS3(ctx context.Context, options S3Options) (*S3, error) {
	if options.Region == "" || options.Bucket == "" {
		return nil, errors.New("S3 region and bucket are required")
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(options.Region)}
	if options.AccessKeyID != "" {
		provider := credentials.NewStaticCredentialsProvider(options.AccessKeyID, options.SecretAccessKey, options.SessionToken)
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(provider))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load AWS configuration: %w", err)
	}
	// S3-compatible providers differ in which optional checksum headers they accept.
	// Only calculate and validate checksums when the operation requires them so
	// presigned browser requests remain portable across AWS, R2, and MinIO.
	cfg.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
	cfg.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	client := s3.NewFromConfig(cfg, func(clientOptions *s3.Options) {
		clientOptions.UsePathStyle = options.PathStyle
		if options.Endpoint != "" {
			clientOptions.BaseEndpoint = aws.String(strings.TrimRight(options.Endpoint, "/"))
		}
	})
	sse := types.ServerSideEncryption("")
	switch options.ServerSideEncryption {
	case "", "none":
	case "AES256":
		sse = types.ServerSideEncryptionAes256
	case "aws:kms":
		sse = types.ServerSideEncryptionAwsKms
	default:
		return nil, fmt.Errorf("unsupported S3 server-side encryption: %s", options.ServerSideEncryption)
	}
	return &S3{
		client: client, presigner: s3.NewPresignClient(client), bucket: options.Bucket,
		prefix: strings.Trim(strings.ReplaceAll(options.Prefix, "\\", "/"), "/"), sse: sse,
	}, nil
}

func (s *S3) Name() string { return "s3" }

func (s *S3) Put(ctx context.Context, key string, body io.Reader, size int64) (ObjectInfo, error) {
	objectKey, err := s.objectKey(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	input := &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objectKey), Body: body}
	if size >= 0 {
		input.ContentLength = aws.Int64(size)
	}
	if s.sse != "" {
		input.ServerSideEncryption = s.sse
	}
	output, err := s.client.PutObject(ctx, input)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("put S3 object: %w", err)
	}
	return ObjectInfo{Size: size, ModTime: time.Now(), ETag: cleanETag(aws.ToString(output.ETag))}, nil
}

func (s *S3) Open(ctx context.Context, key string, byteRange *ByteRange) (Object, error) {
	objectKey, err := s.objectKey(key)
	if err != nil {
		return Object{}, err
	}
	input := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objectKey)}
	if byteRange != nil {
		if byteRange.Start < 0 || byteRange.End < byteRange.Start {
			return Object{}, errors.New("invalid byte range")
		}
		input.Range = aws.String(fmt.Sprintf("bytes=%d-%d", byteRange.Start, byteRange.End))
	}
	output, err := s.client.GetObject(ctx, input)
	if err != nil {
		return Object{}, fmt.Errorf("get S3 object: %w", err)
	}
	total := aws.ToInt64(output.ContentLength)
	if output.ContentRange != nil {
		if parsed, ok := totalFromContentRange(aws.ToString(output.ContentRange)); ok {
			total = parsed
		}
	}
	modified := time.Time{}
	if output.LastModified != nil {
		modified = *output.LastModified
	}
	return Object{
		Body: output.Body, Size: total, ContentLength: aws.ToInt64(output.ContentLength),
		ModTime: modified, ETag: cleanETag(aws.ToString(output.ETag)),
	}, nil
}

func (s *S3) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	objectKey, err := s.objectKey(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objectKey)})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("head S3 object: %w", err)
	}
	modified := time.Time{}
	if output.LastModified != nil {
		modified = *output.LastModified
	}
	return ObjectInfo{Size: aws.ToInt64(output.ContentLength), ModTime: modified, ETag: cleanETag(aws.ToString(output.ETag))}, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	objectKey, err := s.objectKey(key)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objectKey)})
	if err != nil {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}

func (s *S3) DeletePrefix(ctx context.Context, prefix string) error {
	objectPrefix, err := s.objectKey(prefix)
	if err != nil {
		return err
	}
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket), Prefix: aws.String(strings.TrimRight(objectPrefix, "/") + "/"),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list S3 prefix: %w", err)
		}
		if len(page.Contents) == 0 {
			continue
		}
		objects := make([]types.ObjectIdentifier, 0, len(page.Contents))
		for _, object := range page.Contents {
			objects = append(objects, types.ObjectIdentifier{Key: object.Key})
		}
		if _, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket), Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		}); err != nil {
			return fmt.Errorf("delete S3 prefix: %w", err)
		}
	}
	return nil
}

func (s *S3) CreateMultipart(ctx context.Context, key, contentType string) (string, error) {
	objectKey, err := s.objectKey(key)
	if err != nil {
		return "", err
	}
	input := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(objectKey), ContentType: aws.String(contentType),
	}
	if s.sse != "" {
		input.ServerSideEncryption = s.sse
	}
	output, err := s.client.CreateMultipartUpload(ctx, input)
	if err != nil {
		return "", fmt.Errorf("create S3 multipart upload: %w", err)
	}
	return aws.ToString(output.UploadId), nil
}

func (s *S3) PresignUploadPart(ctx context.Context, key, uploadID string, partNumber int, expires time.Duration) (string, error) {
	objectKey, err := s.objectKey(key)
	if err != nil {
		return "", err
	}
	output, err := s.presigner.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(s.bucket), Key: aws.String(objectKey), UploadId: aws.String(uploadID), PartNumber: aws.Int32(int32(partNumber)),
	}, func(options *s3.PresignOptions) { options.Expires = expires })
	if err != nil {
		return "", fmt.Errorf("presign S3 upload part: %w", err)
	}
	return output.URL, nil
}

func (s *S3) CompleteMultipart(ctx context.Context, key, uploadID string, parts []MultipartPart) (ObjectInfo, error) {
	objectKey, err := s.objectKey(key)
	if err != nil {
		return ObjectInfo{}, err
	}
	completed := make([]types.CompletedPart, 0, len(parts))
	for _, part := range parts {
		completed = append(completed, types.CompletedPart{PartNumber: aws.Int32(int32(part.PartNumber)), ETag: aws.String(part.ETag)})
	}
	_, err = s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(objectKey), UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
	})
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("complete S3 multipart upload: %w", err)
	}
	return s.Stat(ctx, key)
}

func (s *S3) AbortMultipart(ctx context.Context, key, uploadID string) error {
	objectKey, err := s.objectKey(key)
	if err != nil {
		return err
	}
	_, err = s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket: aws.String(s.bucket), Key: aws.String(objectKey), UploadId: aws.String(uploadID),
	})
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) && apiError.ErrorCode() == "NoSuchUpload" {
			return nil
		}
		return fmt.Errorf("abort S3 multipart upload: %w", err)
	}
	return nil
}

func (s *S3) PresignGet(ctx context.Context, key, filename string, expires time.Duration) (string, error) {
	objectKey, err := s.objectKey(key)
	if err != nil {
		return "", err
	}
	disposition := "attachment; filename*=UTF-8''" + url.PathEscape(filename)
	output, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(objectKey), ResponseContentDisposition: aws.String(disposition),
	}, func(options *s3.PresignOptions) { options.Expires = expires })
	if err != nil {
		return "", fmt.Errorf("presign S3 download: %w", err)
	}
	return output.URL, nil
}

func (s *S3) objectKey(key string) (string, error) {
	key = strings.TrimSpace(strings.ReplaceAll(key, "\\", "/"))
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, ":") {
		return "", errors.New("invalid object key")
	}
	cleaned := path.Clean(key)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("object key escapes storage prefix")
	}
	if s.prefix == "" {
		return cleaned, nil
	}
	return path.Join(s.prefix, cleaned), nil
}

func cleanETag(value string) string {
	return strings.Trim(value, "\"")
}

func totalFromContentRange(value string) (int64, bool) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return 0, false
	}
	total, err := strconv.ParseInt(parts[1], 10, 64)
	return total, err == nil
}
