# Signature cloud sync

Google Drive and S3 storage are supported, with one active provider
at a time. Switching providers merges the local archive into the new destination;
it does not erase the previous remote archive. The local signature archive remains usable
without a network connection. No documents or general application preferences
are uploaded.

Proview's Google OAuth app is **External** and **In production**, with published
branding. The app requests only the non-sensitive
`drive.file` scope. The public website and policies are at
[proview.0x7f.it](https://proview.0x7f.it/).

## Configure Proview's Google project

This is a maintainer task, once for Proview. Users do **not** create Google Cloud
projects or supply credentials.

1. Create a Google Cloud project for Proview. Enable **Google Drive API**.
2. Configure the Google Auth platform consent screen with Proview's name,
   support contact, and the scope `https://www.googleapis.com/auth/drive.file`.
3. Create OAuth credentials of type **Desktop app**. Keep the same project and
   application identity across distributed builds so authorized files remain
   accessible on other installations.
4. Keep the public app's Audience **External** and **In production**, and its
   approved branding published. Use `0x7f.it` as the authorized domain and the
   website URLs documented in the [Pages and branding guide](github-pages-setup.md).
   The current `drive.file` scope does not require sensitive or restricted scope
   verification. Use a separate project for development or testing rather than
   changing the public app's production configuration. Google Workspace administrators can
   still restrict access for accounts in their organization.
5. Set GitHub repository secrets `PROVIEW_GOOGLE_CLIENT_ID` and
   `PROVIEW_GOOGLE_CLIENT_SECRET` from the downloaded desktop client
   configuration. The client ID may alternatively be a repository variable;
   the secret takes precedence when both are set. The native package jobs embed
   these through linker flags. Rebuild and download the packages after changing
   this configuration; existing binaries do not pick up GitHub settings.
   Neither value contains an end user's access or refresh token. Desktop
   client configuration cannot be confidential once shipped in a binary.

For local development, put `PROVIEW_GOOGLE_CLIENT_ID` and
`PROVIEW_GOOGLE_CLIENT_SECRET` in a `.env` file at the repository root. All Task
commands load it automatically using [Task dotenv support](https://taskfile.dev/docs/guide/environment);
the file is ignored by Git. Variables already exported in the shell take precedence.

For a local native build, provide those environment variables and run
`task build`, or `python3 packaging/build.py -o build/proview` after creating
`build/`. Native platform build/package tasks use the same helper. Without the complete
client configuration the app builds normally and Google Drive remains selectable with
its connection button enabled. Attempting to connect reports the missing OAuth
configuration. For development, `task run` and `go run ./cmd/gui` also read these environment
variables when no client configuration is embedded. `task run` loads `.env`;
invoking `go run ./cmd/gui` directly requires exporting the variables in your shell. Docker cross-builds need
the equivalent `-X` linker flags to embed configuration in their binaries.

Google Drive is available in every build. Published packages require the OAuth
client settings above; local builds can omit them and still use S3 and the local
archive. The manual **Build** workflow publishes one package per supported target,
with no provider-specific variants. It uses `v1.<run-number>` for `develop` and
`v1-dev.<run-number>` for other branches. Pushes do not publish releases.

Login uses the system browser, PKCE, state validation, and a temporary loopback
callback. No hosted callback service or application backend is required.

References: [desktop OAuth](https://developers.google.com/identity/protocols/oauth2/native-app),
[Drive scopes](https://developers.google.com/workspace/drive/api/guides/api-specific-auth),
[desktop Picker](https://developers.google.com/workspace/drive/picker/guides/desktop-mobile-picker),
[OAuth production readiness](https://developers.google.com/identity/protocols/oauth2/production-readiness/overview).

## User flow

Open the signature library and choose **Cloud sync**. Select **Google Drive**
and set **Directory**, which defaults to `proview`. Enter a single folder name
in the root of My Drive, not a nested path. Then choose **Connect to Google Drive**
and authorize Proview in the browser. Proview reuses an accessible folder with
that name or creates one; the initial local/remote merge is automatic.

The directory name is remembered along with the folder ID. Existing connections
without a saved name continue to use `proview`. The Directory field is visible
but disabled while connected; disconnect before changing it. Operations live in
the `signatures` subfolder of the chosen directory. Changing directories merges
the local archive into the new destination and leaves the old remote folder intact.

There is a single connection action; folder lookup and creation are automatic.
The limited `drive.file` permission can discover folders created by Proview or
previously authorized for it. An arbitrary folder created manually outside
Proview can remain invisible to the app, so it cannot guarantee reusing that
folder without broader permissions or explicit authorization. Shared Drives
are not supported. Simultaneously created accessible root folders with the configured name
are read together so their signatures converge; no user files are moved or
removed to consolidate the physical folders.

Opening the library displays local entries immediately and starts an asynchronous
sync, at most once every 30 seconds across all Proview windows. Failed attempts
also count. Connecting is an initial sync; there is no startup polling, periodic
sync, or automatic network retry timer. Opening cloud settings alone does not
bypass the throttle. New signatures, edits, and deletions upload their
queued operations immediately, independently of the download throttle.

The library shows sync status, pending operations, and errors. Offline uploads
stay queued on disk and are retried on the next eligible library opening. A
revoked or expired authorization requires disconnecting and choosing
**Connect to Google Drive** again.
**Disconnect** cancels synchronization and removes the local credentials; it
keeps both local signatures and the remote archive. A later connection merges
that archive again.

## S3 storage

No Google project or embedded application credentials are required for S3.
Create a bucket with your storage provider first. In **Cloud sync**, choose
**S3** and enter:

- **Endpoint**: the service URL, without the bucket, credentials or query string
  (for example `https://s3.eu-west-1.amazonaws.com` for that AWS region, or the
  endpoint supplied by your storage provider).
- **Region**: the signing region required by the provider. Blank defaults to
  `us-east-1`; providers requiring `auto` must be configured with that value.
- **Bucket**: an existing bucket name. Proview never creates or deletes buckets.
- **Prefix**: the archive directory, default `proview/signatures`. All devices
  sharing an archive must use the same endpoint, bucket and prefix.
- **Access key** and **Secret key**: use a long-term access key pair, such as
  credentials for an IAM user. Temporary credentials requiring a session token
  are not supported.
- **Use path-style addressing**: enabled by default for compatible services.
  Disable it when the service requires the bucket in the hostname.

Choose **Connect to S3**. Proview checks access before replacing an existing
connection, then merges signatures and uploads pending local operations. The
library uses the same 30-second download throttle and immediate upload behavior
as Google Drive. S3 remains available in builds without Google OAuth configuration. While connected,
destination settings and the Connect button are disabled. Disconnect before
changing the configuration or switching providers; credential fields remain enabled.

The account needs `s3:ListBucket` scoped to the configured prefix, plus
`s3:GetObject` and `s3:PutObject` for objects beneath it. No delete permission is
required: signature deletions are small current-state tombstones. Additional
permissions may be required by your bucket's encryption policy.

Current state is stored in `<prefix>/current-index.json`, with separate payloads
under `<prefix>/current/<revision-id>.json`. Payload creation uses `If-None-Match: *`;
index creation uses the same condition and updates use `If-Match` with the ETag
returned by S3. The service must support both conditional PutObject modes,
conditional GetObject and ListObjectsV2. Proview never falls back to unconditional
writes. Malformed indexes or legacy operations stop the sync rather than dropping
signatures. See [AWS conditional writes](https://docs.aws.amazon.com/AmazonS3/latest/userguide/conditional-writes.html).

Access keys and secret keys are stored together in the OS
keyring, separately from Google tokens. Preferences store only destination
settings and the remembered provider. If the keyring is unavailable, the
connection works for the current session and is not restored on restart.
**Disconnect** removes saved credentials and connection settings, preserving
local signatures and all remote objects. Use HTTPS for hosted endpoints; HTTP
endpoints are accepted for local/self-hosted services. OS TLS certificate
validation stays enabled.

The S3 client uses the [AWS SDK for Go v2](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/configure-endpoints.html)
with a custom endpoint and configurable addressing. Automated tests exercise
signed requests against a local HTTP server, pagination, invalid operations,
conditional-write races, retries, credential persistence and provider switching.
Real-service compatibility must still be checked with the chosen endpoint.

## Merge and persistence

SHA-256 of a versioned canonical asset representation recognizes exact content,
including original strokes for drawn signatures. Names, dates, and local IDs do
not affect this hash. Visually equivalent files with different encodings are not
necessarily duplicates. Drawn signatures remain editable after downloading.

Identity survives editing; signatures whose snapshots contain identical assets
are persistently treated as aliases. The latest UTC timestamp wins, including
cancellations; equal timestamps are resolved by operation ID. This assumes
reasonably synchronized device clocks. An offline edit newer than a deletion,
or explicitly importing the same content again, can restore a signature.

### Google Drive current state

Google Drive keeps a lightweight `current-index.json` under
`<directory>/signatures/` and separate `current-<revision-id>.json` payloads.
The index contains only the latest state of each logical signature, including
small deletion records and deduplication aliases. Deletions contain no artwork.
Old payloads are retired after publishing their replacements.

A metadata listing checks the index checksum. An unchanged index is reused in
memory; after restarting, the index is fetched again but known signatures are
reused from the local store. Only missing or changed payloads are downloaded,
with at most four concurrent transfers. Folder discovery is cached for five
minutes; the usual 30-second library-opening throttle still applies.

Writes use a conditional index update. Index downloads are bound to the update
token by matching their content checksum against freshly read metadata. A stale writer rereads the index, merges
remote changes, and retries up to four times. The index update uses Drive v2's
ETag and `If-Match` support, with `newRevision=false`, while folder discovery,
listing and payload creation use Drive v3. There is no unconditional overwrite
fallback. Simultaneously created index files are read together and their state
converges into a deterministic destination index.

The first synchronization migrates the legacy operation archive automatically:
validate and merge its records, publish the current payloads and index, then move
the superseded records to Drive's trash. Failed publication leaves the legacy
archive intact. A small cleanup list in the index allows interrupted retirement
to resume on the next synchronization; completed cleanup entries are removed.
Checksums let migration reuse exact operation bytes already stored locally.
Malformed indexes or legacy records stop migration rather than silently dropping
signatures. Update every installation sharing this archive: older builds cannot
read the new current-state format.

Drive payloads are limited to 32 MiB, individual signature assets to 16 MiB, and
the index to 4 MiB. The current local archive retains its existing format. The
hidden `.sync/` directory contains the crash-recovery journal, persisted aliases,
activation marker and destination-specific acknowledgements. The local journal
is retained for crash recovery; neither provider uploads it as a version history.

### S3 current state

S3 uses the same compact signature records, deletion tombstones and persistent
aliases as Google Drive. A conditional GET of `current-index.json` checks its
ETag; an unchanged index returns HTTP 304 with no payload body. After restarting,
the index is fetched again and matching signatures are reused from the local
store. Changed or missing signatures transfer with at most four concurrent
requests. Access checks are cached for five minutes.

Publish immutable payloads before conditionally replacing the index. A conflict
causes a fresh read, merge and retry, up to four attempts. Failed publication
leaves local operations queued; a retry verifies any already-created payload
before reusing it. S3-compatible services must actually honor the conditional
headers: unsupported requests fail explicitly.

The first sync without an index reads and validates all legacy operations under
`<prefix>/<operation-id>.json`, merging them into the current state. Matching
single-part MD5 ETags can reuse known local bytes. After the index is published,
legacy objects are never listed or downloaded by routine synchronization.
Unreferenced legacy and superseded payloads remain in the bucket for recovery;
Proview does not require `s3:DeleteObject` or automatically delete them. Do not
apply a blanket expiration rule to `current/`: an old payload can still be the
current version of a signature. Bucket versioning, if enabled by its owner, may
also retain previous index versions independently of Proview.

Update every installation sharing the archive before editing signatures:
older builds write the legacy journal and cannot read the current-state index.
Payloads are limited to 32 MiB, assets to 16 MiB and indexes to 4 MiB.

For Google Drive, the refresh token is stored in macOS Keychain, Windows Credential Manager, or
Linux Secret Service. Preferences contain only the remembered-connection flag and
folder ID and configured directory name. If the OS credential store is unavailable, login works for the current
session and the dialog explains that it will not be remembered. Access tokens
are never written to ordinary preferences or build artifacts.

## Google Drive two-installation acceptance check

Use two released installations built with the same OAuth project and a Google
account. Use sample signatures for this check.

1. Create different local signatures on both installations. Create one identical
   signature independently on both. Connect both to the same Drive account.
   Reopen each library after 30 seconds: all unique assets appear once.
2. Edit a drawn signature offline on both installations, with different
   modification times. Reconnect the network and open the libraries: the later
   edit wins, and the original drawing remains editable.
3. Delete a signature offline, restart that installation, and synchronize both.
   The deleted signature stays absent after subsequent merges and restarts.
4. Create a signature while connected. Verify its current payload and index
   appear in Drive immediately without waiting 30 seconds or reopening the library.
5. Open libraries rapidly in multiple windows. Verify only one full download
   starts within a 30-second interval; the editor remains responsive.
6. Modify signatures on both installations simultaneously. Verify both changes
   survive conditional index conflicts and converge on the next sync.
7. Deny browser authorization, cancel the dialog, revoke Google authorization,
   and test without a system keyring. Local signatures remain usable and the
   dialog reports the applicable error or session-only connection.
8. Disconnect and restart. The app performs no cloud requests until connected
   again, and both local and remote signatures remain available.

This real-account check requires the configured Google project; local automated
tests use simulated providers, a Drive HTTP server, and an OAuth token server.

## S3 two-installation acceptance check

Use two installations with credentials for the same bucket and prefix.

1. Connect both through **S3**. Verify unique signatures converge,
   identical assets deduplicate, and drawn signatures remain editable.
2. Repeat the offline edit and deletion checks above. Verify new local
   changes upload immediately and the 30-second library download throttle applies.
3. Restart with a working keyring and verify S3 reconnects when opening the
   library. Repeat without a keyring and verify the connection is session-only.
4. Test a wrong endpoint, wrong region, denied credentials and an unavailable
   bucket. A failed replacement must preserve the previous working connection.
5. Change the bucket or prefix and verify the local archive uploads to the new
   destination. Disconnect and verify no further requests and no remote deletion.
6. Test the service's supported addressing mode and conditional PutObject behavior.
   These checks require a real bucket; simulated-server tests do not establish
   compatibility with every S3 service.

## Current-state implementation references

- [Drive performance and conditional updates](https://developers.google.com/workspace/drive/api/guides/performance)
- [Drive v2 file updates and revision retention](https://developers.google.com/workspace/drive/api/reference/rest/v2/files/update)

Automated current-state checks cover legacy migration, unchanged sync request
counts, cached payloads after restart, offline deletions, alias persistence,
conditional-write conflicts, failed publication and interrupted cleanup. Native
account measurements complement these simulated-server checks.
