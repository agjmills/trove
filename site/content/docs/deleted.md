---
title: Deleted Items
weight: 7
---

When you delete a file or folder it moves to the trash rather than being removed immediately. Items in the trash can be restored or permanently deleted.

## Viewing the trash

Go to **Deleted** in the sidebar. Each item shows its original location, when it was deleted, and how long it has until it is permanently removed.

## Restoring items

Click **Restore** next to a file or folder to return it to its original location.

- If the original folder no longer exists, the file is restored to the root.
- If a file with the same name already exists at the destination, the restore is blocked — rename or remove the conflicting file first.
- Restoring a folder restores all of its contents recursively.

## Permanent deletion

You can permanently delete individual items from the trash before they expire, or use **Empty trash** to remove everything at once.

Physical storage is not freed until the last reference to a file is removed — if you uploaded the same file twice, deleting one copy does not free storage until both are deleted.

## Automatic cleanup

Items in the trash are permanently deleted after a retention period. The default is 30 days, controlled by `DELETED_RETENTION_DAYS`.

The cleanup worker runs on an interval set by `DELETED_CLEANUP_INTERVAL_MIN` (default: 60 minutes).

| Variable | Default | Description |
|----------|---------|-------------|
| `DELETED_RETENTION_DAYS` | `30` | Days to retain deleted items before auto-deletion |
| `DELETED_CLEANUP_INTERVAL_MIN` | `60` | How often the cleanup worker runs (minutes) |

Set `DELETED_RETENTION_DAYS=0` to keep deleted items indefinitely (no auto-deletion).
