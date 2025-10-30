# Download Error Classification

This document describes the error classification system used by vod-tender to distinguish between retryable (transient) and fatal (permanent) download errors.

## Overview

The error classification system automatically determines whether a download error should be retried or treated as fatal. This prevents unnecessary retry attempts on permanent failures (like authentication errors or deleted content) while allowing the system to recover from transient issues (like network timeouts or temporary server errors).

## Implementation

The classification logic is implemented in `backend/vod/errors.go` with the following components:

- `ClassifyDownloadError(err error) ErrorClass` - Main classification function
- `IsRetryableError(err error) bool` - Helper to check if error is retryable
- `IsFatalError(err error) bool` - Helper to check if error is fatal

## Error Classes

### Fatal Errors (Non-Retriable)

These errors indicate permanent failures that will not succeed even with retries:

#### Authentication & Authorization
- **Patterns**: `subscriber-only`, `login required`, `401`, `403`, `unauthorized`
- **Example**: "This video is subscriber-only"
- **Reason**: Requires user credentials or subscription; automatic retry won't help

#### Content Not Found or Unavailable
- **Patterns**: `404`, `not found`, `video unavailable`, `deleted`, `does not exist`
- **Example**: "Video has been deleted by the creator"
- **Reason**: Content no longer exists or is not accessible

#### Invalid Input
- **Patterns**: `invalid url`, `malformed url`, `invalid video id`, `unsupported url`
- **Example**: "Invalid video ID format"
- **Reason**: Input data is malformed; same input will always fail

#### DRM & Content Protection
- **Patterns**: `drm protected`, `protected content`, `encrypted content`
- **Example**: "This content is DRM protected"
- **Reason**: Content is protected and cannot be downloaded with current tools

### Retryable Errors (Transient)

These errors indicate temporary failures that may succeed on retry:

#### Network Errors
- **Patterns**: `connection reset`, `connection refused`, `timeout`, `dns`, `eof`, `broken pipe`
- **Example**: "connection timed out after 30s"
- **Reason**: Network issues are often temporary and resolve with retry

#### Server Errors (5xx)
- **Patterns**: `500`, `502`, `503`, `504`, `internal server error`, `bad gateway`, `service unavailable`
- **Example**: "HTTP Error 503: Service Unavailable"
- **Reason**: Server is temporarily overwhelmed or undergoing maintenance
- **Note**: Checked before "unavailable" patterns to avoid misclassification with "video unavailable"

#### Rate Limiting
- **Patterns**: `429`, `too many requests`, `rate limit`, `throttled`
- **Example**: "Too many requests, please try again later"
- **Reason**: Rate limits reset over time; retry with backoff will succeed

#### Incomplete Downloads
- **Patterns**: `partial content`, `fragment`, `incomplete download`
- **Example**: "Failed to download fragment 42"
- **Reason**: Partial downloads can often be resumed

### Unknown Errors

Errors that don't match any known pattern are classified as **retryable by default**. This conservative approach prevents giving up too early on unexpected errors that might be transient.

## Integration with Download Logic

The error classification system is designed to integrate with the existing retry logic in `backend/vod/vod.go`:

### Current Behavior

```go
// In downloadVOD function:
for attempt := 0; attempt < maxAttempts; attempt++ {
    // ... attempt download ...
    if err != nil {
        lastErr = err
        // Currently retries all errors up to maxAttempts
        if attempt < maxAttempts-1 {
            // Exponential backoff + jitter
            time.Sleep(backoff)
            continue
        }
    }
}
```

### Potential Enhancement

```go
// Enhanced with error classification:
for attempt := 0; attempt < maxAttempts; attempt++ {
    // ... attempt download ...
    if err != nil {
        if IsFatalError(err) {
            // Don't retry fatal errors
            logger.Warn("fatal error detected, aborting retries", 
                slog.Any("err", err),
                slog.String("class", "fatal"))
            return "", err
        }
        lastErr = err
        // Only retry retryable errors
        if attempt < maxAttempts-1 {
            time.Sleep(backoff)
            continue
        }
    }
}
```

## Testing

The error classification system has comprehensive test coverage:

### Test Files
- `backend/vod/errors_test.go` - 50+ test cases covering all error patterns
- `backend/vod/downloader_test.go` - aria2c detection and download configuration tests

### Running Tests

```bash
cd backend
go test ./vod -run "TestClassify|TestError|TestIsRetryable|TestIsFatal" -v
```

### Test Coverage

- **Fatal Errors**: 25+ patterns tested
- **Retryable Errors**: 25+ patterns tested  
- **Edge Cases**: nil errors, empty strings, case-insensitive matching
- **Helper Functions**: `IsRetryableError()`, `IsFatalError()`

## Configuration

The error classification is automatic and doesn't require configuration. However, the retry behavior is controlled by existing environment variables:

- `DOWNLOAD_MAX_ATTEMPTS` - Maximum retry attempts (default: 5)
- `DOWNLOAD_BACKOFF_BASE` - Base backoff duration (default: 2s)

## Examples

### Fatal Error Example

```go
err := errors.New("This video is subscriber-only")
class := ClassifyDownloadError(err)
// class == ErrorClassFatal

if IsFatalError(err) {
    // Stop retrying, mark as non-retriable
    // Update DB: SET download_retries = DOWNLOAD_MAX_ATTEMPTS
}
```

### Retryable Error Example

```go
err := errors.New("connection timed out")
class := ClassifyDownloadError(err)
// class == ErrorClassRetryable

if IsRetryableError(err) {
    // Continue retry loop with exponential backoff
}
```

### Unknown Error Example

```go
err := errors.New("unexpected error: xyz")
class := ClassifyDownloadError(err)
// class == ErrorClassRetryable (default for safety)

// Unknown errors are retried to avoid giving up too early
```

## Best Practices

1. **Log Classifications**: When using error classification, log the classification result for debugging
2. **Monitor Patterns**: Track which error patterns occur most frequently to tune classification
3. **Update Patterns**: As new error types are discovered, update the classification patterns
4. **Test Integration**: When integrating classification, ensure existing tests still pass

## Future Enhancements

Possible future improvements to the error classification system:

1. **Integration**: Integrate classification into `downloadVOD()` to skip retries on fatal errors
2. **Metrics**: Add Prometheus metrics for error classifications (fatal vs retryable counts)
3. **User Feedback**: Add UI indicators for fatal vs retryable errors
4. **Pattern Learning**: Machine learning to discover new error patterns automatically
5. **Configurable Patterns**: Allow users to customize error classification patterns

## Troubleshooting

### Error Misclassified as Retryable

If an error that should be fatal is being retried:

1. Check if the error message matches any fatal patterns
2. Add the pattern to `ClassifyDownloadError()` in `backend/vod/errors.go`
3. Add a test case to `backend/vod/errors_test.go`
4. Run tests to verify: `go test ./vod -run TestClassify -v`

### Error Misclassified as Fatal

If an error that should be retryable is being marked fatal:

1. Check pattern matching order in `ClassifyDownloadError()`
2. More specific patterns should be checked before generic ones
3. Ensure server errors (503) are checked before generic "unavailable"
4. Add/update test cases to prevent regression

### Adding New Error Patterns

To add support for new error types:

1. Determine if error is fatal or retryable
2. Add pattern to appropriate section in `backend/vod/errors.go`
3. Add test case to `backend/vod/errors_test.go`
4. Run full test suite: `go test ./vod -v`
5. Update this documentation

## Related Documentation

- [ARCHITECTURE.md](./ARCHITECTURE.md) - System architecture overview
- [OPERATIONS.md](./OPERATIONS.md) - Operational procedures and troubleshooting
- [CONFIG.md](./CONFIG.md) - Configuration reference
