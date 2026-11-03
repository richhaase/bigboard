# Publisher setup

[README](../README.md) · [Development guide](../native/README.md)

This guide is for maintaining Bigboard's GitHub App and release credentials.
App users sign in through GitHub; see the [user reference](reference.md#setup-and-settings).

## GitHub App registration

The publisher app is [Bigboard Desktop](https://github.com/apps/bigboard-desktop).
Use a public GitHub App with these settings:

| Setting | Value |
| --- | --- |
| Homepage | `https://github.com/richhaase/bigboard` |
| Production callback | `com.richhaase.bigboard://oauth/github` |
| Development callback | `com.richhaase.bigboard.dev://oauth/github` |
| Callback wildcard matching | Disabled |
| Expire user authorization tokens | Enabled |
| Request user authorization during installation | Disabled |
| Device flow | Disabled |
| Webhooks | Disabled |
| Setup URL | Empty |
| Installation availability | Any account |
| Repository permissions | Contents, Pull requests, Checks, Commit statuses: read-only |
| Metadata | Read-only, implicitly required |
| Organization/account permissions | None |

Keep both callbacks registered on the same app. Each Bigboard build accepts only
its own exact callback route. Authorization uses state, S256 PKCE, expiration,
and single-use validation before exchanging a code.

GitHub requires a client secret for the authorization-code flow. A native app
cannot keep an embedded client secret confidential; PKCE protects intercepted
codes. Bigboard uses user access tokens and does not need a GitHub App private
key or hosted authentication service. See
[GitHub's public-client guidance](https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/best-practices-for-creating-a-github-app#client-secrets).

## Build configuration

The publisher supplies three build settings:

| Setting | Value | GitHub Actions storage |
| --- | --- | --- |
| `BIGBOARD_GITHUB_CLIENT_ID` | GitHub App client ID | Repository variable |
| `BIGBOARD_GITHUB_APP_SLUG` | App slug from its public URL | Repository variable |
| `BIGBOARD_GITHUB_CLIENT_SECRET` | GitHub App client secret | Repository secret |

For local development, create the ignored `native/publisher.local.json` using
the publisher's values in place of these placeholders:

```json
{
  "BIGBOARD_GITHUB_CLIENT_ID": "your-client-id",
  "BIGBOARD_GITHUB_CLIENT_SECRET": "your-client-secret",
  "BIGBOARD_GITHUB_APP_SLUG": "bigboard-desktop"
}
```

Restrict the file to your account with `chmod 600 native/publisher.local.json`.
Keep actual values out of source control, logs, and chat. GitHub Actions secrets
cannot be read back for local use.

`BIGBOARD_PUBLISHER_CONFIG` selects another JSON file. Environment variables
override individual file values, including explicit empty strings.
`scripts/configure-publisher.swift` validates the configuration and embeds it
in the built Info.plist before signing.

A local build may omit all three values and open with sign-in unavailable.
Configured builds require all three. The release script sets
`BIGBOARD_REQUIRE_AUTH=YES` so publication fails if any value is missing or invalid.
Use `make dev` and the **Bigboard Dev** Xcode scheme for
[local development](../native/README.md#local-development).

## Authentication and access

Access is the intersection of the signed-in user's permissions and the GitHub
App's installation access. Discovery pages through user installations and their
repositories. Organization approval and SSO policies still apply.

Access and refresh tokens stay in the authentication service and macOS Keychain.
Token rotation is serialized and persisted before a new token is used. Tokens
are excluded from settings, logs, repository URLs, and command arguments.
Authenticated Git fetches use a temporary, GitHub-scoped environment header;
credentials are not written into Git config or passed to credential helpers.

Signing out removes the current build's local credential. Revoking authorization
or changing installation access on GitHub affects both production and development.
The [development guide](../native/README.md#storage-and-access) describes account
isolation and cached access.

## Release credentials

Configure these repository secrets for the
[Release workflow](../.github/workflows/release.yml):

| Secret | Purpose |
| --- | --- |
| `QUILL_SIGN_P12` | Base64-encoded Developer ID Application certificate and private key. |
| `QUILL_SIGN_PASSWORD` | Password for the certificate file. |
| `QUILL_NOTARY_KEY` | Apple notarization API key, as PEM text or base64. |
| `QUILL_NOTARY_KEY_ID` | Notarization API key ID. |
| `QUILL_NOTARY_ISSUER` | Notarization API issuer ID. |
| `HOMEBREW_TAP_GITHUB_TOKEN` | Token with write access to `richhaase/homebrew-tap`. |

The workflow also uses the GitHub App variables and secret listed above.
GitHub supplies `GITHUB_TOKEN` for publishing the release in this repository.

The certificate action imports the signing identity into a temporary keychain.
The workflow requires one Developer ID Application identity and derives its
Team ID. It passes `SIGNING_IDENTITY`, `DEVELOPMENT_TEAM`, `NOTARY_KEY_PATH`,
`NOTARY_KEY_ID`, and `NOTARY_ISSUER` to the release script. The temporary
notarization key and signing keychain are cleaned up after the job.

Follow the [release procedure](../native/README.md#releases) and
[installed-upgrade checks](../native/README.md#installed-upgrade-verification)
when publishing or changing signing behavior.
