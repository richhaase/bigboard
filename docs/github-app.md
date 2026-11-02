# GitHub sign-in and publisher setup

Bigboard signs in through a public GitHub App. Users choose **Sign in with
GitHub**, authorize in their browser, and use **Manage access…** to
choose installation repositories on GitHub. Back in Bigboard, they refresh the
repository list and select what to collect in Settings. They do not register an
app, supply client IDs, create personal access tokens, or install `gh`.

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

The release workflow injects these publisher values into the native bundle metadata.
`scripts/build-macos.sh` rejects missing or malformed configuration. End users
have no configuration fields for these values. A local development build may
omit all three, in which case sign-in is unavailable; supplying all three as
environment variables enables the same flow through `make build` or `make dev`.
The app bundle must be launched on macOS for native callback registration.

The publisher app is [Bigboard Desktop](https://github.com/apps/bigboard-desktop).
Registration and publishing-repository settings are configured separately from
the source code. A successful native exchange must still verify the client
secret and callback before release readiness is established.

## Access and credential boundaries

User access is the intersection of the signed-in user's GitHub permissions and
repositories selected for the app's installations. Discovery pages through
`/user/installations` and each installation's repositories. **Manage access…**
opens GitHub's app installation page; return to Bigboard and refresh the
repository list after selecting repositories or receiving an organization's approval.
Organization policy, SSO, and administrator approval still apply. Bigboard does
not bypass them or request write permissions to work around a denial.

Access and refresh tokens stay in the authentication service and macOS Keychain. They
are absent from settings JSON, interface state, diagnostic messages, repository
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
The last verified grant remains available while a live access check is pending,
so quitting during a refresh does not discard offline access. A completed check
replaces that grant; a confirmed denial removes the affected access. The old
grant is invalidated immediately before saving an authoritative replacement.
If saving fails after invalidation, restart requires successful live verification
before cached evidence is available. Cancellation never restores a grant after
sign-out or an account change, and confirmed denials remain authoritative.

## Native verification boundary

Swift authentication and GitHub tests use an isolated credential store and mock
HTTP transports. Native storage tests use temporary directories, and Git tests
use owned local fixture repositories. No real credentials are read by tests.
Native dependencies are Apple frameworks and system Git, with no third-party
Swift package dependencies. Automated tests and local signature verification do
not establish release signing, notarization, or the live OAuth exchange.

## Signed-release validation

Normal installed upgrades must not repeat the Keychain permission prompt.
The first native release will be validated by Rich upgrading from the installed
Developer ID-signed prior release to the published native version. Follow up with
a patch if a regression appears; this check does not block that initial release.
Compare the installed prior release with the signed native version
from the same publisher Team ID and bundle identifier, with compatible designated
requirements and the existing Keychain entry intact. Approve persistent access
once on the initial installation if needed, perform the normal replacement
upgrade, and verify that both launch and restart reuse the signed-in account
without another prompt while the login Keychain is unlocked.

Ad-hoc local builds have code-hash-based identity that can change on rebuild;
they are not proof of continuity for published releases. Developer ID upgrades
must retain a stable designated requirement. See [Apple's code identity guidance](https://developer.apple.com/documentation/technotes/tn3127-inside-code-signing-requirements)
and the [installed-upgrade acceptance steps](../native/README.md#installed-upgrade-keychain-validation).
This validation remains pending for the native replacement: the publisher signing
identity is not available in the local test environment.

Offline tests cover the authorization protocol, callback validation, token
rotation, storage failures, HTTP boundaries, and account switching. During
signed-release validation, verify the configured public registration with the signed app:
sign in through the browser, select public and private repositories, confirm an
organization restriction is explained, restart to verify Keychain persistence,
then sign out and sign in with another account. Confirm repository evidence does
not cross accounts. Never put real authorization codes or tokens in test fixtures,
logs, screenshots, or the PR.

References: [user access tokens](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app),
[token refresh](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/refreshing-user-access-tokens),
[Git permissions](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app#choosing-permissions-for-git-access),
[REST API practices](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api).
