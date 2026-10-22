# Jarvis Registry: a static OAuth client for Claude Desktop

This is a brief for the Jarvis Registry codebase. It explains why Claude Desktop deployments made with this repository need a **static, pre-registered public OAuth client** on the Jarvis Registry's authorization server, what Desktop does with it, and what the server must provide. `<jarvis-host>` stands for a Jarvis Registry deployment's hostname.

## Background

### What this repository does

`claude-3p-config` generates managed policies that point Claude Desktop and the Claude Code CLI at Amazon Bedrock instead of a Claude.ai subscription ("third-party" or "3P" mode). One config renders a macOS configuration profile and a Windows `.reg` file, and both are deployed to every employee device of a client company. The Desktop policy can add **managed MCP connectors**, such as the Jarvis Registry at `https://<jarvis-host>/gateway/proxy/mcpgw/mcp`, through Desktop's `managedMcpServers` setting.

### How each client signs in to the Jarvis Registry

| Client | Where the OAuth flow runs | Redirect URI | Client registration |
| --- | --- | --- | --- |
| Claude Desktop with a Claude.ai subscription (Pro, Team) | Anthropic's servers broker it | A claude.ai address | Handled by Anthropic |
| Claude Code CLI | On the device | `http://localhost:<random free port>/callback` | Dynamic client registration (RFC 7591), per device |
| Claude Desktop in 3P mode, automatic registration | On the device | `http://127.0.0.1:53280/callback`, always this port | Dynamic client registration, per device |
| Claude Desktop in 3P mode, pre-registered client | On the device | `http://127.0.0.1:<callbackPort>/callback` | **A client ID in the policy, shared by every device** |

In 3P mode Desktop starts a temporary HTTP listener on the loopback address, opens the system browser at the authorization endpoint, and receives the authorization code on that listener. It uses PKCE (S256) and needs no client secret.

### The problem: port 53280 on Windows

Automatic registration always uses port 53280. On Windows that port is inside the TCP dynamic port range (49152–65535 by default). WSL2, Docker Desktop and Hyper-V reserve blocks of ports from that range through the Host Network Service and WinNAT, and the blocks change after each reboot. When a block covers 53280, no program can listen on it.

On the Windows 10 test machine (WSL2 installed), `netsh interface ipv4 show excludedportrange protocol=tcp` listed a reserved range `53181–53280`. Every **Connect** in Desktop failed with only a brief "connection failed" message, and Desktop's `main.log` recorded:

```
LocalAgentModeSessions.authorizeDirectMcpServer: jarvis-registry — failed (transport): listen EACCES: permission denied 127.0.0.1:53280
```

The Claude Code CLI and Claude.ai-subscription Desktop weren't affected, because they don't listen on 53280.

Asking client companies to turn off WSL2 or to change Windows networking isn't workable: their IT departments manage the devices, developers need WSL2, and the reservation moves on every reboot.

### The fix, and why it needs the Jarvis Registry

Desktop's `managedMcpServers[].oauth` has a `callbackPort` setting, but Desktop accepts it **only together with a `clientId`**. With `callbackPort` and no `clientId`, Desktop drops the connector entirely:

```
managedMcpServers entry "jarvis-registry" dropped — oauth.clientId: clientId must be a non-empty string (a pre-registered client requires it; for automatic registration use `"oauth": true` or an object with only scope/appendOfflineAccess/additionalRedirectReferrerHosts)
```

So this repository now always renders a pre-registered client ID and callback port **47280** for OAuth connectors. Port 47280 is below the dynamic range, so Windows never reserves it, and it's in a block IANA hasn't assigned (47101–47556). Each device gets this entry:

```json
{
  "name": "jarvis-registry",
  "transport": "http",
  "url": "https://<jarvis-host>/gateway/proxy/mcpgw/mcp",
  "oauth": {"clientId": "<static client ID>", "scope": "mcp-proxy-ops", "callbackPort": 47280}
}
```

The same client ID goes to every device of every client company that uses the policy, so the client must exist on the server permanently and independently of any device.

## What was tested

On 2026-10-22, on the Windows machine above, with WSL2 still on and 53280 still reserved:

1. A client was registered through the existing registration endpoint, `POST https://<jarvis-host>/auth/oauth2/register`, with `redirect_uris` `["http://127.0.0.1:47280/callback"]`, `grant_types` `["authorization_code", "refresh_token"]`, `response_types` `["code"]`, `token_endpoint_auth_method` `none` and `scope` `mcp-proxy-ops`. The server returned a `client_id` and `client_secret_expires_at: 0`, with no secret.
2. The Desktop policy was set to that `clientId`, `callbackPort` 47280, `scope` `mcp-proxy-ops` and `authorizationServer` `["https://<jarvis-host>"]`.
3. **Connect** completed the Entra sign-in in the browser, and the connector's tools worked.

That test client lives in Redis like any dynamically registered client, and it expires on its own after about a month, around 2026-11-22. It proves the flow but isn't a deployment target: when it expires, every device whose policy uses it loses the connector until the policy is redeployed with a new client ID.

### The server's metadata at the time of the test

From `https://<jarvis-host>/.well-known/oauth-protected-resource/gateway/proxy/mcpgw/mcp` and `https://<jarvis-host>/.well-known/oauth-authorization-server`:

- The MCP endpoint answers `401` with `WWW-Authenticate: Bearer realm="jarvis-resources", resource_metadata="…", scope="mcp-proxy-ops"`.
- `authorization_servers`: `["https://<jarvis-host>"]`. The protected-resource metadata advertises 23 scopes in `scopes_supported`, which is why the policy sets `scope` explicitly instead of letting Desktop request everything advertised.
- `authorization_endpoint` `/auth/oauth2/login/entra`, `token_endpoint` `/auth/oauth2/token`, `registration_endpoint` `/auth/oauth2/register`.
- `token_endpoint_auth_methods_supported` `["client_secret_post", "none"]`, `code_challenge_methods_supported` `["S256"]`, `grant_types_supported` including `authorization_code` and `refresh_token`.

## What the Jarvis Registry needs

### Requirements

1. **A static client defined in the server's configuration**, not stored as a dynamic registration in Redis. Dynamically registered clients expire after about a month, which suits a per-device registration that can register again but not a client ID baked into device policies. The static client must survive restarts, redeploys and Redis flushes, must not expire, and must not be deletable or editable through the registration endpoint or RFC 7592 client management.
2. **A stable, non-secret client ID**, the same in every Jarvis Registry deployment, so one policy works for all client companies. A readable value such as `claude-desktop` is fine; it isn't a credential. Changing it later means redeploying the policy to every device.
3. **A public client.** `token_endpoint_auth_method` `none`: the token endpoint must accept requests from this client without a secret. A secret in a device policy would be readable by anyone with the device, so it would add nothing.
4. **PKCE required** for this client, with S256. With no secret, PKCE is what stops an intercepted authorization code from being used.
5. **Redirect URI** `http://127.0.0.1:47280/callback`. Plain `http` is acceptable only because it's the loopback address (RFC 8252 section 7.3).
6. **Grant types** `authorization_code` and `refresh_token`, and response type `code`. Desktop refreshes tokens in the background during a session; issue a refresh token on the code exchange, and if refresh tokens rotate, make sure a refreshed token keeps working across Desktop restarts.
7. **Scopes**: allow `mcp-proxy-ops`, the scope the MCP endpoint requires and the one the policy requests. Limit the client to the scopes Claude Desktop needs rather than everything in `scopes_supported`.
8. **Keep dynamic client registration** working as it does today. The Claude Code CLI registers itself on each device with a random port, and other MCP clients may rely on it too.

### Recommended

- **Accept any port on `127.0.0.1` for this client's redirect URI**, with the path `/callback`, as RFC 8252 section 7.3 recommends for native apps. Then a deployment that has to change `callback_port` (for example because another program on a device holds 47280) doesn't need a Jarvis Registry release. Keep the host fixed to `127.0.0.1`; Desktop can also use `localhost`, but this repository doesn't.
- **Make the static client configurable**, with the defaults above: client ID, allowed redirect URIs or a loopback-any-port flag, allowed scopes, and a display name such as "Claude Desktop" for any consent or audit screens.
- **Log the client ID and the rejected parameter** when an authorize or token request fails (unknown client, redirect URI mismatch, missing PKCE, scope not allowed). Desktop shows users only a brief error, so the server's log is often the only clear evidence.

### Acceptance test

On a Windows machine with WSL2 or Docker Desktop installed, using a policy generated from a config with `"oauth": {"client_id": "<static client ID>", "scope": "mcp-proxy-ops"}`:

1. Quit Claude Desktop completely, import the policy, start Desktop and sign in with AWS.
2. **Customize → Connectors → Connect** on `jarvis-registry`, then finish the Entra sign-in in the browser.
3. Check that the connector's tools work in a chat, and that `main.log` has no `dropped` or `listen EACCES` lines for it.
4. Restart the Jarvis Registry (and flush Redis on a test deployment), quit and reopen Desktop, and check that the connector still works or signs in again without any policy change.
5. Repeat steps 1–3 on macOS.

## Rollout

1. Release the Jarvis Registry with the static client and pass the acceptance test, before the test client expires around 2026-11-22.
2. In each deployment's `config/config.json`, set `desktop.mcp_servers[].oauth.client_id` to the static client ID, then regenerate and redeploy the policies (see the [admin guide](admin-guide.md#mcp-connectors-with-oauth)).
3. Let the test client registered on 2026-10-22 expire; no cleanup is needed.
