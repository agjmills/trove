---
title: Registration & First-time Setup
weight: 8
---

## First account

The first account registered on a fresh Trove instance is automatically granted admin status. This applies whether the account is created via the registration form or via OIDC.

## Controlling registration

`ENABLE_REGISTRATION` (default: `true`) controls whether the `/register` page is accessible.

Set it to `false` once your account is set up to prevent anyone else from self-registering:

```bash
ENABLE_REGISTRATION=false
```

With registration disabled, new users can only be added by an admin via the [admin panel]({{< ref "admin" >}}).

## OIDC-only setup

If you want all logins to go through OIDC with no local passwords:

```bash
ENABLE_REGISTRATION=false
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://auth.example.com/application/o/trove/
OIDC_CLIENT_ID=your-client-id
OIDC_CLIENT_SECRET=your-client-secret
OIDC_REDIRECT_URL=https://trove.example.com/auth/oidc/callback
```

New users are auto-provisioned on their first OIDC login. The first person to log in becomes admin. Subsequent users are regular users unless you configure an admin claim:

```bash
OIDC_ADMIN_CLAIM=groups
OIDC_ADMIN_VALUE=trove-admins
```

When `OIDC_ADMIN_CLAIM` is set, admin status is synced from the claim on every login.

## Migrating a local account to OIDC

Go to **Admin → Users**, find the account, and switch the identity provider to `OIDC`. The user's OIDC subject is linked automatically on their next OIDC login and password authentication is disabled for that account.

> The admin panel prevents you from switching your own account to OIDC while logged in, to avoid locking yourself out. Another admin can do it, or you can update the `identity_provider` column directly in the database.

## New user defaults

New users (however created) are assigned the default quota from `DEFAULT_USER_QUOTA`. Admins can override this per user in the admin panel.

| Variable | Default | Description |
|----------|---------|-------------|
| `ENABLE_REGISTRATION` | `true` | Allow self-registration at `/register` |
| `DEFAULT_USER_QUOTA` | `10G` | Storage quota assigned to new users |
