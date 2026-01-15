package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Version information (set via ldflags during build)
var (
	version   = "dev"
	gitCommit = "unknown"
	buildTime = "unknown"
)

const (
	// Default timeout for the pihole backup command
	piholeCommandTimeout = 5 * time.Minute
	// S3 upload timeout
	s3UploadTimeout = 2 * time.Minute
	// Maximum retry attempts for S3 upload
	maxRetries = 3
	// Base delay for retry backoff
	retryBaseDelay = 2 * time.Second
)

// exitCode constants for consistent exit behavior
const (
	exitSuccess        = 0
	exitConfigError    = 1
	exitBackupError    = 2
	exitUploadError    = 3
	exitValidationError = 4
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("pb2s3 %s (commit: %s, built: %s)\n", version, gitCommit, buildTime)
		os.Exit(exitSuccess)
	}

	if err := run(); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(getExitCode(err))
	}
}

// Custom error types for proper exit codes
type configError struct{ error }
type backupError struct{ error }
type uploadError struct{ error }
type validationError struct{ error }

func getExitCode(err error) int {
	switch err.(type) {
	case configError:
		return exitConfigError
	case backupError:
		return exitBackupError
	case uploadError:
		return exitUploadError
	case validationError:
		return exitValidationError
	default:
		return 1
	}
}

func run() error {
	// Validate required configuration
	bucketName := os.Getenv("PB2S3_AWS_BUCKET_NAME")
	if bucketName == "" {
		return validationError{fmt.Errorf("PB2S3_AWS_BUCKET_NAME environment variable is required")}
	}

	// Load AWS configuration
	cfg, err := getAwsConfig()
	if err != nil {
		return configError{fmt.Errorf("failed to load AWS config: %w", err)}
	}

	// Validate region is set
	if cfg.Region == "" {
		return configError{fmt.Errorf("AWS region is not configured. Set AWS_REGION or PB2S3_AWS_REGION environment variable")}
	}

	log.Printf("Starting Pi-hole backup to s3://%s in region %s", bucketName, cfg.Region)

	// Create temporary directory for backup
	tempDir, err := os.MkdirTemp("", "piholebackup")
	if err != nil {
		return backupError{fmt.Errorf("failed to create temp directory: %w", err)}
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			log.Printf("WARNING: failed to clean up temp directory %s: %v", tempDir, err)
		}
	}()

	// Run Pi-hole backup command
	backupFile, err := runPiholeBackup(tempDir)
	if err != nil {
		return backupError{err}
	}

	log.Printf("Backup created: %s", backupFile)

	// Upload to S3 with retry logic
	s3Key := filepath.Base(backupFile) // Use only filename, not full path
	if err := uploadToS3WithRetry(cfg, bucketName, s3Key, backupFile); err != nil {
		return uploadError{err}
	}

	log.Printf("Successfully uploaded %s to s3://%s/%s", filepath.Base(backupFile), bucketName, s3Key)
	return nil
}

func getAwsConfig() (aws.Config, error) {
	ctx := context.Background()
	var opts []func(*config.LoadOptions) error

	// Check for custom credentials
	awsAccessKey := os.Getenv("PB2S3_AWS_ACCESS_KEY")
	awsSecretKey := os.Getenv("PB2S3_AWS_SECRET_KEY")

	if awsAccessKey != "" && awsSecretKey != "" {
		log.Println("Using PB2S3_AWS_* credentials")
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(awsAccessKey, awsSecretKey, ""),
		))
	}

	// Check for custom region (support both standard and custom env vars)
	region := os.Getenv("PB2S3_AWS_REGION")
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	// Check for custom S3 endpoint (for S3-compatible storage like MinIO)
	endpoint := os.Getenv("PB2S3_S3_ENDPOINT")
	if endpoint != "" {
		log.Printf("Using custom S3 endpoint: %s", endpoint)
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("unable to load SDK config: %w", err)
	}

	return cfg, nil
}

func runPiholeBackup(tempDir string) (string, error) {
	// Save current directory and restore after
	originalDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	if err := os.Chdir(tempDir); err != nil {
		return "", fmt.Errorf("failed to change to temp directory %s: %w", tempDir, err)
	}
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			log.Printf("WARNING: failed to restore original directory: %v", err)
		}
	}()

	// Create context with timeout for the pihole command
	ctx, cancel := context.WithTimeout(context.Background(), piholeCommandTimeout)
	defer cancel()

	log.Println("Running pihole teleporter backup...")
	cmd := exec.CommandContext(ctx, "pihole", "-a", "-t")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("pihole backup command timed out after %v", piholeCommandTimeout)
		}
		return "", fmt.Errorf("pihole backup command failed: %w", err)
	}

	// Find the backup file
	files, err := filepath.Glob("pi-hole*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("failed to search for backup files: %w", err)
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no backup file found matching pattern 'pi-hole*.tar.gz'")
	}

	if len(files) > 1 {
		log.Printf("WARNING: found %d backup files, using most recent: %v", len(files), files)
		// Files are returned in lexical order, but pi-hole includes timestamp so this should work
	}

	backupFile := filepath.Join(tempDir, files[0])
	return backupFile, nil
}

func uploadToS3WithRetry(cfg aws.Config, bucket, key, filePath string) error {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := uploadToS3(cfg, bucket, key, filePath)
		if err == nil {
			return nil
		}

		lastErr = err
		if attempt < maxRetries {
			delay := retryBaseDelay * time.Duration(1<<(attempt-1)) // Exponential backoff
			log.Printf("Upload attempt %d/%d failed: %v. Retrying in %v...", attempt, maxRetries, err, delay)
			time.Sleep(delay)
		}
	}

	return fmt.Errorf("upload failed after %d attempts: %w", maxRetries, lastErr)
}

func uploadToS3(cfg aws.Config, bucket, key, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer file.Close()

	// Get file info for logging
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat backup file: %w", err)
	}

	log.Printf("Uploading %s (%d bytes) to s3://%s/%s", filepath.Base(filePath), fileInfo.Size(), bucket, key)

	ctx, cancel := context.WithTimeout(context.Background(), s3UploadTimeout)
	defer cancel()

	// Create S3 client with optional custom endpoint
	var svc *s3.Client
	endpoint := os.Getenv("PB2S3_S3_ENDPOINT")
	if endpoint != "" {
		svc = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			// For S3-compatible storage, often need path-style addressing
			if strings.Contains(endpoint, "localhost") || strings.Contains(endpoint, "minio") {
				o.UsePathStyle = true
			}
		})
	} else {
		svc = s3.NewFromConfig(cfg)
	}

	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   file,
	}

	_, err = svc.PutObject(ctx, input)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("S3 upload timed out after %v", s3UploadTimeout)
		}
		return fmt.Errorf("S3 upload failed: %w", err)
	}

	return nil
}
