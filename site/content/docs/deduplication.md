---
title: Deduplication
weight: 9
---

Trove uses content-addressed deduplication to avoid storing the same file more than once per user account.

## How it works

When you upload a file, Trove computes a SHA-256 hash of its contents. If you already have a completed file with the same hash, the upload completes instantly and no additional storage is used — the new file entry points to the existing physical copy.

Uploads that are deduplicated are marked as such in the UI ("uploaded (deduplicated)").

## Scope

Deduplication is **per user**. If two different users upload identical files, each gets their own independent copy. Files are never shared between accounts.

## Quota

Even when a file is deduplicated, it counts toward your storage quota. The quota reflects logical storage (the sum of all your files), not physical storage on disk.

## Deletion

When you delete a file, the underlying storage is only freed once all copies pointing to the same physical file are deleted. For example, if you uploaded the same file twice and created two entries, deleting one does not free disk space until the other is also permanently deleted.

## Disabling deduplication

Set `ENABLE_FILE_DEDUPLICATION=false` to disable the dedup check. Uploaded files will always be stored as new independent copies regardless of content.

| Variable | Default | Description |
|----------|---------|-------------|
| `ENABLE_FILE_DEDUPLICATION` | `true` | Enable content-addressed deduplication within a user's account |
