# Pi-hole Config Backup to S3

A lightweight Go binary that backs up your Pi-hole configuration to AWS S3 (or S3-compatible storage). Designed to run on Raspberry Pi and other ARM devices.

## Features

- Automated Pi-hole configuration backup using the built-in teleporter
- Uploads backups to AWS S3 with server-side encryption
- Supports S3-compatible storage (MinIO, Backblaze B2, etc.)
- Retry logic with exponential backoff for unreliable networks
- Systemd service and timer for scheduled backups
- Pre-built binaries for x86, ARM64, and ARM32 (Raspberry Pi)

## Quick Start

### 1. Deploy AWS Infrastructure

Deploy the CloudFormation template to create the S3 bucket and IAM credentials:

```bash
aws cloudformation deploy \
  --template-file templates/PiholeBackupToS3.cloudformation.json \
  --stack-name pihole-backup \
  --parameter-overrides piholeBackupBucketName=my-pihole-backups \
  --capabilities CAPABILITY_IAM
```

Retrieve your credentials from AWS Secrets Manager:

```bash
# Get the secret ARN
SECRET_ARN=$(aws cloudformation describe-stacks \
  --stack-name pihole-backup \
  --query 'Stacks[0].Outputs[?OutputKey==`SecretArn`].OutputValue' \
  --output text)

# Retrieve the credentials
aws secretsmanager get-secret-value \
  --secret-id "$SECRET_ARN" \
  --query 'SecretString' \
  --output text | jq .
```

### 2. Install the Binary

Download the appropriate binary for your platform:

```bash
# For Raspberry Pi 3/4 (64-bit OS)
wget https://github.com/YOUR_USERNAME/pihole-config-backup-to-S3/releases/latest/download/pb2s3-arm64
sudo mv pb2s3-arm64 /usr/local/bin/pb2s3
sudo chmod +x /usr/local/bin/pb2s3

# For Raspberry Pi 2/3/4 (32-bit OS)
wget https://github.com/YOUR_USERNAME/pihole-config-backup-to-S3/releases/latest/download/pb2s3-arm32

# For Raspberry Pi 1/Zero/Zero W
wget https://github.com/YOUR_USERNAME/pihole-config-backup-to-S3/releases/latest/download/pb2s3-arm32v6
```

Or build from source:

```bash
git clone https://github.com/YOUR_USERNAME/pihole-config-backup-to-S3.git
cd pihole-config-backup-to-S3
make build-arm64  # or build-arm32, build-arm32v6
sudo mv pb2s3-arm64 /usr/local/bin/pb2s3
```

### 3. Configure

Create the configuration directory and file:

```bash
sudo mkdir -p /etc/pihole-backup
sudo cp templates/systemd/config.example /etc/pihole-backup/config
sudo chmod 600 /etc/pihole-backup/config
sudo nano /etc/pihole-backup/config
```

Fill in your AWS credentials from step 1.

### 4. Set Up Scheduled Backups

Install the systemd service and timer:

```bash
sudo cp templates/systemd/pihole-backup.service /etc/systemd/system/
sudo cp templates/systemd/pihole-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable pihole-backup.timer
sudo systemctl start pihole-backup.timer
```

To run a backup manually:

```bash
sudo systemctl start pihole-backup.service
```

Check backup status:

```bash
sudo journalctl -u pihole-backup.service -f
```

## Configuration

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `PB2S3_AWS_BUCKET_NAME` | Yes | S3 bucket name for backups |
| `PB2S3_AWS_REGION` | Yes* | AWS region (e.g., `us-east-1`) |
| `PB2S3_AWS_ACCESS_KEY` | No | AWS access key (if not using IAM roles) |
| `PB2S3_AWS_SECRET_KEY` | No | AWS secret key (if not using IAM roles) |
| `PB2S3_S3_ENDPOINT` | No | Custom S3 endpoint URL |
| `AWS_REGION` | Yes* | Standard AWS region variable |
| `AWS_ACCESS_KEY_ID` | No | Standard AWS access key |
| `AWS_SECRET_ACCESS_KEY` | No | Standard AWS secret key |

*Either `PB2S3_AWS_REGION` or `AWS_REGION` must be set.

### Using S3-Compatible Storage

To use MinIO or other S3-compatible storage:

```bash
export PB2S3_AWS_BUCKET_NAME=pihole-backups
export PB2S3_AWS_REGION=us-east-1
export PB2S3_AWS_ACCESS_KEY=minioadmin
export PB2S3_AWS_SECRET_KEY=minioadmin
export PB2S3_S3_ENDPOINT=http://minio.local:9000
```

## CloudFormation Template

The included CloudFormation template creates:

- **S3 Bucket** with:
  - Server-side encryption (AES-256)
  - Public access blocked
  - Versioning enabled
  - Lifecycle policy for automatic cleanup (configurable, default 30 days)

- **IAM User** with minimal permissions (PutObject only)

- **Secrets Manager Secret** containing credentials (no plaintext in outputs)

### Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `piholeBackupBucketName` | (required) | Name for the S3 bucket |
| `BackupRetentionDays` | 30 | Days to retain backups (0 to disable) |

## Building from Source

Requirements:
- Go 1.21 or later

```bash
# Build for current platform
make build

# Build all platforms
make build-all

# Build specific platform
make build-arm64
make build-arm32
make build-arm32v6
make build-x86

# Clean build artifacts
make clean
```

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | Configuration error (AWS config failed) |
| 2 | Backup error (Pi-hole command failed) |
| 3 | Upload error (S3 upload failed) |
| 4 | Validation error (missing required config) |

## Troubleshooting

### "PB2S3_AWS_BUCKET_NAME environment variable is required"

Set the bucket name:
```bash
export PB2S3_AWS_BUCKET_NAME=your-bucket-name
```

### "AWS region is not configured"

Set the region:
```bash
export PB2S3_AWS_REGION=us-east-1
```

### "pihole backup command failed"

Ensure Pi-hole is installed and the command works manually:
```bash
pihole -a -t
```

### "S3 upload failed: Access Denied"

Check that your IAM user has `s3:PutObject` permission on the bucket.

### Viewing logs

```bash
# View recent backup logs
sudo journalctl -u pihole-backup.service --since "1 hour ago"

# Follow logs in real-time
sudo journalctl -u pihole-backup.service -f

# Check timer status
sudo systemctl status pihole-backup.timer
```

## License

MIT License
