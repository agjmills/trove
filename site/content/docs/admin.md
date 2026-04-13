---
title: Admin Panel
weight: 6
---

The admin panel is available to any user with admin status at `/admin`.

## Dashboard

The dashboard shows system-wide totals: registered users, total files, and total storage used across all accounts.

## User management

`/admin/users` lists every user with their file count and storage usage.

### Creating users

Admins can create accounts directly — useful when `ENABLE_REGISTRATION=false`. Set the username, email, password, and optionally grant admin status on creation.

### Editing users

Click a user to edit:

- **Storage quota** — override the default quota for that user. Accepts the same human-readable sizes as `DEFAULT_USER_QUOTA` (`500M`, `10G`, etc.). Setting it below their current usage is allowed but shows a warning.
- **Admin status** — promote or demote any user. You cannot change your own admin status.
- **Identity provider** — switch a user between `internal` (password) and `oidc`. Switching to OIDC clears their password hash; their OIDC subject is linked on the next OIDC login.
- **Reset password** — set a new password for any internal-auth user.

### Deleting users

Deleting a user removes their account immediately. Their files are deleted asynchronously in the background. You cannot delete your own account.

## Trash management

The admin dashboard includes an **Empty all trash** action that permanently deletes every soft-deleted item across all users, regardless of retention settings.
