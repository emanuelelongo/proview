# Proview website and Google OAuth branding

Updated October 14, 2026. The website is published at
[proview.0x7f.it](https://proview.0x7f.it/). Google OAuth branding has been
approved and published, and the app's Audience is **External**, **In production**.

The website is static HTML/CSS, with no build,
JavaScript, external fonts or analytics. Its files are `index.html`,
`privacy.html`, `terms.html`, `site-assets/`, `.nojekyll` and `CNAME`; the homepage also
uses the existing `screenshots/overview.png`. Relative links work at the custom domain's root.
The application icon is copied from `assets/app-icon.png`.

## 1. Review contact information and content

The public pages identify Emanuele Longo as the maintainer and include
`longo.emanuele@gmail.com` as a hardcoded `mailto:` contact for support and
privacy enquiries. GitHub issues remain available for public bug reports.
Use the same email for **User support email** and **Developer contact
information** in Google Auth Platform. Confirm the privacy statements match
the version you distribute.

The privacy page covers local processing, Google Drive data and tokens,
optional S3, retention, revocation, deletion and GitHub Pages security logging.
Terms preserve the project's AGPL-3.0-or-later grant and the rights to users'
documents. These pages do not add a privacy notice inside the desktop app.
Keep the published policies and in-product privacy disclosures aligned with
the distributed app.

## 2. Optional local preview

From the repository root:

```sh
python3 -m http.server 8080 --directory docs
```

Open `http://localhost:8080/`. Check all three pages and the mobile layout.
Stop the server with Ctrl+C. No installation or build is needed.

## 3. Publish website updates

The publishing branch is `develop`, with Pages serving `/docs`. Review the
website diff before committing and pushing changes. Keep `docs/CNAME` containing
`proview.0x7f.it` and include any required assets. Preserve unrelated working-tree
changes. Do not upload `.env`, OAuth credentials or user tokens.

## 4. Enable Pages

In `https://github.com/emanuelelongo/proview`:

1. Open **Settings → Pages**.
2. Under **Build and deployment**, choose **Deploy from a branch**.
3. Select **develop**, folder **/docs**, and click **Save**.
4. Set **Custom domain** to `proview.0x7f.it`. Enable **Enforce HTTPS** once
   the certificate is available.
5. Wait for the Pages deployment in **Actions** to finish successfully.
6. Open the URLs below and confirm that all pages and the screenshot load.

| Purpose | URL |
| --- | --- |
| Homepage | https://proview.0x7f.it/ |
| Privacy policy | https://proview.0x7f.it/privacy.html |
| Terms of service | https://proview.0x7f.it/terms.html |

`.nojekyll` makes Pages serve these files without a Jekyll build. Publishing
`/docs` also exposes the existing documentation and screenshots in that folder.
Future pushes to the chosen branch update the site automatically.

Reference: [GitHub publishing source documentation](https://docs.github.com/en/pages/getting-started-with-github-pages/configuring-a-publishing-source-for-your-github-pages-site).

## 5. Configure OVH DNS and verify domain ownership

In the `0x7f.it` DNS zone, the `proview` CNAME must point to
`emanuelelongo.github.io.`. The trailing dot is essential with OVH: without it,
OVH treats the target as relative and appends `.0x7f.it`. Do not include a scheme
or `/proview/` path. To correct the existing record and publish the zone:

```sh
ovhcloud domain-zone record update 0x7f.it 5441804432 \
  --target 'emanuelelongo.github.io.'
ovhcloud domain-zone refresh 0x7f.it
dig proview.0x7f.it CNAME +short
```

The expected answer is `emanuelelongo.github.io.`. After propagation, use
**Check again** in GitHub Pages if needed.

For Google ownership verification:

1. Sign in to [Google Search Console](https://search.google.com/search-console)
   with the Google account that owns the Google Cloud project.
2. Add a **Domain property** for `0x7f.it`.
3. Add Google's provided TXT record to the OVH zone and refresh the zone.
4. Click **Verify** in Search Console and keep the TXT record in place.

Domain ownership is verified through DNS. Keep all three pages' canonical URLs
on the custom domain.

References: [GitHub custom domain configuration](https://docs.github.com/en/pages/configuring-a-custom-domain-for-your-github-pages-site/managing-a-custom-domain-for-your-github-pages-site),
[OAuth domain verification requirements](https://support.google.com/cloud/answer/13804266?hl=en).

## 6. Configure Google Auth Platform

Select the **same Google Cloud project used by the distributed desktop app**.
Do not replace its Desktop app client with a Web application client for this
website. The website does not host an OAuth callback or need client secrets.

1. In **Branding**, use app name **Proview** and support email
   **longo.emanuele@gmail.com**.
   The support email must be eligible in Google's dropdown; a developer
   contact email is also required.
2. Upload `docs/site-assets/app-icon.png` if using a logo. The PNG is under
   Google's 1 MB limit; Google recommends a square 120 × 120 image.
3. Set **Authorized domains** to `0x7f.it`, which covers `proview.0x7f.it`.
   Enter the domain without a scheme or path.
4. Enter the three URLs from the table above in **Application home page**,
   **Application privacy policy link** and **Application terms of service link**.
5. In **Data Access**, keep only the scope the app requests:
   `https://www.googleapis.com/auth/drive.file`. Remove unused scopes after
   checking that no other client in the same project needs them. This scope is
   non-sensitive and does not require sensitive or restricted scope verification.
   Keep **Google Drive API** enabled in the project.
6. Keep **Audience** set to **External**, **In production**. Use a separate
   project for development and testing.
7. Keep the approved branding published. Publishing the app audience and
   publishing branding are separate steps. Future branding changes may require
   verification again.
8. Test the released app with a Google account:
   open the signature library → Cloud sync → Google Drive → Connect to Google
   Drive; approve access, create a sample signature and confirm it reaches Drive.
   Test a second installation, deletion, disconnect and account revocation using
   sample artwork. See the [cloud sync acceptance check](signature-cloud-sync.md#google-drive-two-installation-acceptance-check).

Google Workspace administrators can still restrict authorization for their users.

If useful for the reviewer, describe the feature this way:

> Proview is a desktop image and PDF viewer/editor. Optional Google Drive
> integration synchronizes the user's reusable handwritten signature library
> between their computers. The app requests only drive.file to manage its
> signature archive. Documents opened for editing are not uploaded by this
> feature. Authorization uses the system browser and a local desktop callback.

References: [Branding configuration](https://support.google.com/cloud/answer/15549049?hl=en),
[Brand verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/brand-verification),
[Drive scope classification](https://developers.google.com/workspace/drive/api/guides/api-specific-auth),
[Google API Services User Data Policy](https://developers.google.com/terms/api-services-user-data-policy).

## 7. Desktop release configuration

Keep the existing Desktop OAuth configuration and release credentials. Publishing
the website, changing its domain and publishing branding alone do not require an
app rebuild. If the embedded client configuration changes, rebuild the packages;
see the [cloud sync guide](signature-cloud-sync.md).
