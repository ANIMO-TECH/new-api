# Test gateway log archive and retention

This standalone operations module is scoped to the explicitly configured test hostname and Coolify application. It does not connect to or modify the application's database, OSS user-content buckets, SigNoz configuration, or production gateway. Its JSON handling is self-contained so the tool can be built and recovered independently of the application.

The application change is opt-in with `LOG_ROTATION_ENABLED=true`. It retains a stable writer for Gin, mirrors unchanged bytes to stdout/stderr and the file, and rotates at 100 MiB or after 24 hours before the next write. One larger-than-limit write remains intact. It never deletes files. Defaults leave other deployments unchanged. `LOG_ROTATION_MAX_BYTES` and `LOG_ROTATION_MAX_AGE` can narrow the validated bounds; these are deployment settings, not hot-reload controls.

The worker scans only the approved application's containers. It reads application logs and Docker `json-file` logs, compresses 8 MiB source segments, uploads each under a content-addressed key, then downloads, decompresses and verifies SHA-256. A closed file receives a complete manifest only after a second full-file restore matches the source. Recovery also verifies the manifest bytes against the SHA-256 and file size in the original manifest key; internally consistent replacement contents cannot substitute for that receipt. The checkpoint and remote manifest are synced before retention is considered. Active files are archived incrementally; open descriptors preserve access to the final bytes when a container is later removed. This does not make restarting the worker while unarchived, unlinked source files exist safe.

Deletion is **disabled by default**. After a real restore rehearsal, `delete_enabled` may be changed without restarting the worker. Each deletion still requires a complete recovered manifest, an unchanged file generation (device/inode/birth time), size, mtime and checksum, age at least 72 hours since the last write, and a fresh check that no other host process—including collectors or another container—has the file open. Missing creation-time support or any API, read, upload, download, integrity or checkpoint error retains the file. There is no size-based override that discards recent logs. The worker never deletes Docker-managed log files. Normal user-initiated root-admin cleanup in the gateway remains a separate operation.

## Infrastructure prerequisites

- A new private Hong Kong archive bucket, server-side AES256 encryption, HTTPS, public access blocked. Initial verification must not enable an archive-expiry lifecycle. Archive retention/expiry is a separate gate; keep completed archives at least 14 days and do not expire segments still referenced by an active/incomplete file.
- A narrowly scoped ECS instance RAM role: only archive prefix PutObject/GetObject, plus prefix-limited listing if needed for recovery. No DeleteObject, bucket configuration or business-data permissions. No long-lived key is copied to the host. The official OSS Go SDK refreshes temporary role credentials. SDK credential error bodies are suppressed to avoid logging tokens.
- The configured bucket must not enable versioning for these immutable keys if relying on `x-oss-forbid-overwrite`; OSS ignores that header when versioning is enabled. Keep the bucket separate from business object storage and validate its actual properties before deployment.
- Install the static Linux binary and systemd unit after review. The service uses low CPU/I/O priority, bounded memory and a 64 Mbit/s per-request transfer limit. Keep the initial configuration archive-only.

## Build and tests

```sh
go test -race ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o newapi-logarchive .
```

Root CI separately runs `go test -race ./logger`. Tests cover retained Gin writers, concurrent rotation, time/size triggers, failed sinks, archive upload/download/corruption failures, incremental recovery, inode replacement, open-file protection, and the 72-hour deletion gate. Live OSS permission, restore and deployment tests are still required; in-memory tests do not substitute for those checks.

## Operating sequence

1. Start archive-only, leaving the running gateway untouched. Read `/var/lib/newapi-logarchive/status.json`; all expected sources must be present and caught up. A successful loop by itself does not prove the byte counters match the current source.
2. Recover a complete manifest into a separate protected destination using `--restore <manifest-key> --output <new-path>`. The output path must not exist. The tool downloads actual objects, verifies segments and the complete SHA-256, and removes an incomplete output on failure. For a still-active source, explicit `--snapshot` recovers exactly the archived prefix and reports `source_closed=false`; this never grants deletion eligibility. A recovery host may run this read-only command with suitable OSS read credentials via an ECS role; mutating worker mode still enforces the configured test hostname.
3. Independently compare the restored file with the source's bytes/hash. Record the manifest key, sizes and checksums. Do not use the nearly full root filesystem to hold a second multi-GB copy; use a separate recovery machine/local restricted workspace when appropriate.
4. Only then enable application-file deletion. Check that the old eligible file alone disappears and the recovered archive remains usable. Active/young/unverified files remain present.
5. Configure the gateway's future Docker instance with short-lived logs only after archive coverage is verified. Rebuilding the current container is a distinct controlled deployment: route new requests to the healthy replacement, let old HTTP requests and batch bookkeeping drain, stop but retain the old container, verify its final archived bytes, then remove it. Preserve/restore retained application history into the persistent log mount. Do not use an automatic deployment that removes the old writable layer before this final check.
6. Preserve stdout/stderr formatting and the existing logging driver on any environment not explicitly approved. A deployment may already use fluentd→Fluent Bit→SigNoz, or may have log drain disabled. This worker must not silently enable, replace or disrupt either setup.

Never run two workers against one checkpoint directory; the process uses an exclusive lock. Configuration reload permits only the deletion switch, and disables deletion on any invalid/mismatched configuration. Stop/upgrade the worker only after pending sources—including unlinked open descriptors—are verified archived. Monitor archive lag and available disk; finite Docker retention is not a guarantee against an arbitrarily long archive outage.

## Rollback

Set `delete_enabled=false` first; it reloads on the next loop without closing source handles. Keep all archive objects and the manifest/checkpoint directory. Restore files from verified manifests when needed. Revert the application image/config via the reviewed deployment procedure while preserving its mounted log directory; a code rollback alone cannot recover deleted logs. Do not delete the archive bucket or alter SigNoz to roll back this feature.
