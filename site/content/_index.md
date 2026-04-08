---
title: Trove
layout: hextra-home
---

<div class="hx-mt-6 hx-mb-4">
{{< hextra/hero-headline >}}
  Trove
{{< /hextra/hero-headline >}}
</div>

<div class="hx-mb-8">
{{< hextra/hero-subtitle >}}
  Self-hosted file storage. Single Docker container, ~18MB image, no JavaScript framework.
{{< /hextra/hero-subtitle >}}
</div>

```bash
docker run -d -p 8080:8080 -v ./data:/app/data -v /tmp \
  -e SESSION_SECRET="$(openssl rand -base64 32)" \
  -e DB_TYPE=sqlite \
  ghcr.io/agjmills/trove:latest
```

<div class="hx-mt-6 hx-mb-10">
{{< hextra/hero-button text="Documentation" link="docs/" >}}
{{< hextra/hero-button text="GitHub" link="https://github.com/agjmills/trove" style="secondary" >}}
</div>

---

Trove is a file storage server written in Go. Upload, organise, and share files from a single binary. It uses local disk or S3-compatible storage, and PostgreSQL or SQLite.

It doesn't have a mobile app, a cloud sync daemon, or a premium tier.

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="File management"
    subtitle="Upload, rename, move, trash, and restore files. Folder hierarchy. Natural sort order."
  >}}
  {{< hextra/feature-card
    title="Storage backends"
    subtitle="Local disk, AWS S3, Cloudflare R2, MinIO, Backblaze B2, or rustfs."
  >}}
  {{< hextra/feature-card
    title="Multi-user"
    subtitle="Per-user quotas. Admin panel for user management. OIDC/SSO via Authentik, Authelia, or Keycloak."
  >}}
  {{< hextra/feature-card
    title="Resumable uploads"
    subtitle="Chunked upload with pause and resume. SHA-256 integrity check. No client-side JS framework."
  >}}
  {{< hextra/feature-card
    title="Sharing"
    subtitle="Share files or folders via link. Optional password, expiry date, and download limit."
  >}}
  {{< hextra/feature-card
    title="Search"
    subtitle="Search by filename, folder path, and tags. Tags can be set at upload time."
  >}}
{{< /hextra/feature-grid >}}
