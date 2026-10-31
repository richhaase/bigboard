# GitHub sign-in and publisher setup

Bigboard v1.1.0 replaces the GitHub CLI dependency with a public GitHub App.
Homebrew users install Bigboard, choose **Sign in with GitHub**, and authorize
repository access in their browser. After sign-in, a new workspace prompts them
to choose an installation account and repositories on GitHub, then discovers
those repositories when they continue in Bigboard. They do not register an app, supply client
IDs, create personal access tokens, or install `gh`.

## One-time publisher registration

Register a **public GitHub App** owned by the Bigboard publisher. The following
settings are required before a release can support sign-in:

| Setting | Value |
| --- | --- |
| Homepage | `https://github.com/richhaase/bigboard` |
| Callback URL | `com.richhaase.bigboard://oauth/github` |
| Callback wildcard | Disabled |
| Expire user authorization tokens | Enabled |
| Request user authorization during installation | Disabled |
| Device flow | Disabled |
| Webhooks | Disabled |
| Setup URL | Empty |
| Installation availability | Any account |
| Repository permissions | Contents, Pull requests, Checks, Commit statuses: **read-only** |
| Metadata | Read-only, implicitly required |
| Organization/account permissions | None |

Bigboard initiates authorization itself so every attempt includes a fresh state
value and an S256 PKCE challenge. Its registered macOS URL scheme receives the
callback; the backend verifies the exact route, matching state, expiration,
and single use before exchanging the code. It requests no OAuth `repo` scope
and needs no GitHub App private key, installation-token signing key, webhook
receiver, or hosted authentication service.

GitHub requires a client secret for this native authorization-code flow. Its
[current native-app guidance](https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/best-practices-for-creating-a-github-app#client-secrets)
explicitly treats a client secret shipped in a public native client as public;
PKCE protects intercepted authorization codes. The build parameter must never
be mistaken for an app private key or used to authorize privileged app operations.

In GitHub App settings, open **Credentials → Client secrets** to create the
client secret; the control is no longer on the general settings page. The
separate **Key pairs** tab is for app private keys, which Bigboard does not use.
GitHub moved these controls in its [October 2026 app-management update](https://github.blog/changelog/2026-10-13-github-app-improvements-for-least-privilege-and-management/).
Store the generated value directly in the build environment or repository secret;
do not paste it into chat or commit it. GitHub Actions secrets cannot be read back
for a local build.

Configure the publishing repository after registration:

| Build setting | GitHub Actions storage |
| --- | --- |
| `BIGBOARD_GITHUB_CLIENT_ID` | Repository variable |
| `BIGBOARD_GITHUB_APP_SLUG` | Repository variable, from the app's public URL |
| `BIGBOARD_GITHUB_CLIENT_SECRET` | Repository secret |

The release workflow injects these publisher values into the native binary.
`scripts/build-macos.sh` rejects missing or malformed configuration. End users
have no configuration fields for these values. A local development build may
omit all three, in which case sign-in is unavailable; supplying all three as
environment variables enables the same flow through `make build` or `make dev`.
The app bundle must be launched on macOS for native callback registration.
The browser preview cannot complete native sign-in.

The publisher app is [Bigboard Desktop](https://github.com/apps/bigboard-desktop).
Registration and publishing-repository settings are configured separately from
the source code. A successful native exchange must still verify the client
secret and callback before release readiness is established.

## Access and credential boundaries

User access is the intersection of the signed-in user's GitHub permissions and
repositories selected for the app's installations. Discovery pages through
`/user/installations` and each installation's repositories. **Manage repository
access** opens GitHub's app installation page; return to Bigboard and discover
again after selecting repositories or receiving an organization's approval.
Organization policy, SSO, and administrator approval still apply. Bigboard does
not bypass them or request write permissions to work around a denial.

Access and refresh tokens stay in the native backend and macOS Keychain. They
are absent from settings JSON, frontend state, diagnostic messages, repository
URLs, and command arguments. Expiring user tokens refresh under a serialized
lock; refresh-token rotation is persisted before the new token is used. API
requests are serialized, bounded, and respect GitHub retry/reset headers and
conditional GET caching. Private Git fetches use a temporary, GitHub-scoped
process environment header, without credential helpers or persistent Git config.

Sign out removes Bigboard's local credentials. To revoke the authorization or
change installation permissions on GitHub, use GitHub's application settings.
Cached repository evidence is separated by the stable GitHub account ID. A new
sign-in verifies access before opening history; a restart with the same stored
account can use its local cache offline, visibly cached/stale. Live work checks
access again, and authoritative access denials do not become offline fallbacks.

## Security scan boundary

The Linux security job scans the root Go module and excludes `desktop`, which
is a separate Go module requiring macOS Keychain APIs. The `desktop-security`
job independently runs govulncheck and gosec from `desktop` on macOS, including
`keychain_darwin.go`. Excluding it from the Linux traversal prevents an invalid
cross-platform scan; it does not remove desktop security coverage.

G117 remains enabled except in `auth/keychain_payload.go`, which contains only
the credential persistence method. Its JSON payload intentionally includes access
and refresh tokens and is passed only to the backend Store. The native Store
writes it to a non-synchronizing macOS Keychain item; it is not a frontend DTO,
log entry, exported report, or settings file. All other security rules still
scan this file. Keep this exception limited to that persistence boundary;
new serialization elsewhere must remain checked by G117. Existing auth tests
cover opaque status, storage failures, refresh-token rotation, and sign-out;
desktop tests cover Keychain operations without real credentials.

## Required release verification

Offline tests cover the authorization protocol, callback validation, token
rotation, storage failures, HTTP boundaries, and account switching. Before
publishing v1.1.0, verify the configured public registration with a signed app:
sign in through the browser, select public and private repositories, confirm an
organization restriction is explained, restart to verify Keychain persistence,
then sign out and sign in with another account. Confirm repository evidence does
not cross accounts. Never put real authorization codes or tokens in test fixtures,
logs, screenshots, or the PR.

References: [user access tokens](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app),
[token refresh](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/refreshing-user-access-tokens),
[Git permissions](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app#choosing-permissions-for-git-access),
[REST API practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api).
