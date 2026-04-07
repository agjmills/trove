---
title: Trove
layout: hextra-home
---

{{< hextra/hero-badge >}}
  <span>Open Source</span>
  {{< icon name="arrow-circle-right" attributes="height=14" >}}
{{< /hextra/hero-badge >}}

<div class="hx-mt-6 hx-mb-6">
{{< hextra/hero-headline >}}
  Self-hosted file storage&nbsp;&nbsp;
  that stays out of your way
{{< /hextra/hero-headline >}}
</div>

<div class="hx-mb-12">
{{< hextra/hero-subtitle >}}
  Your personal Google Drive alternative.&nbsp;&nbsp;
  Single Docker container. ~18MB image.
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx-mb-6">
{{< hextra/hero-button text="Get Started" link="docs/" >}}
{{< hextra/hero-button text="View on GitHub" link="https://github.com/agjmills/trove" style="secondary" >}}
</div>

<div class="hx-mt-6"></div>

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="Simple deployment"
    subtitle="Single Docker container, ~18MB image. Up in under a minute with docker compose."
    icon="cube"
  >}}
  {{< hextra/feature-card
    title="Pluggable storage"
    subtitle="Store files on local disk, AWS S3, Cloudflare R2, MinIO, or Backblaze B2."
    icon="server"
  >}}
  {{< hextra/feature-card
    title="Multi-user"
    subtitle="Per-user storage quotas, admin panel, OIDC/SSO support for Authentik, Authelia, Keycloak."
    icon="user-group"
  >}}
  {{< hextra/feature-card
    title="Resumable uploads"
    subtitle="Chunked uploads with pause, resume, and cancel. SHA-256 integrity verification."
    icon="arrow-up"
  >}}
  {{< hextra/feature-card
    title="Sharing"
    subtitle="Share files and folders with optional password protection, expiry, and download limits."
    icon="share"
  >}}
  {{< hextra/feature-card
    title="Search"
    subtitle="Full-text search across filenames, folders, and tags. Tag files at upload time."
    icon="beaker"
  >}}
{{< /hextra/feature-grid >}}
