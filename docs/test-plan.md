# Test plan

Run this on each new platform, and again after changing the config schema or the generated policies. Run it twice: first with an administrator permission set to prove the configuration works, then with the permission set people will actually get, to prove its IAM policy is enough.

## Results

| Platform | Date | Versions | Result |
| --- | --- | --- | --- |
| macOS 26.7.1 | 2026-10-21 | Claude Desktop 2.26454.2, Claude Code 2.1.306, AWS CLI 2.37.12 | All passed, with the administrator permission set. Sonnet and Haiku only: the Opus tier and `default_tier` were added later and are untested. D4 passed with automatic client registration; re-run it with the pre-registered client and callback port 47280 |
| Windows 10 Home 22H2 (19045) | 2026-10-22 | Claude Desktop 3.1623.4 (Microsoft Store), Claude Code 2.1.308, AWS CLI 2.38.4 | All passed, with the administrator permission set and WSL2 installed. Sonnet and Haiku only. D4 failed with automatic client registration (port 53280 reserved by WSL2), then passed with a pre-registered client and callback port 47280 |
| macOS (Codex direct Runtime check) | 2026-10-30 | Codex 0.165.0-alpha.1.2 | Global GPT-6 Luna, GPT-6.1 Sol and GPT-6.1 Terra each returned `bedrock-ok`; 6.1 Sol completed a local `pwd` tool call. `auth_refresh` mechanism verified the same day: on SSO expiry Codex invokes the refresh command and retries. Managed profile installation, the full browser re-login flow and a non-admin employee permission set still require rollout validation |

## Before you start

- **Use a clean shell.** Exported `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` or `AWS_SESSION_TOKEN` override `AWS_PROFILE`.
- **Expect your own Claude Code to switch.** Once the policy is installed, every `claude` session for your user uses Bedrock, including ones already running (Claude Code re-reads managed settings periodically).
- **Use a browser that completes device sign-in.** On the tested Mac, one Chrome installation never sent the approval after "Confirm and continue", in every profile and in incognito, while Safari and a fresh Chrome profile worked.

## Claude Desktop

| # | Steps | Pass when |
| --- | --- | --- |
| D1 | Install the policy, quit and reopen Desktop, sign in with AWS | The device page shows the same code as Desktop; after **Allow**, Desktop is signed in |
| D2 | Open the model picker | Only the configured models' labels, with the `default_tier` model selected |
| D3 | Chat with each model | Replies arrive; Bedrock usage of the matching AIP goes up |
| D4 | **Customize → Connectors**, connect each managed connector. On Windows, test on a machine with WSL2 or Docker Desktop installed | It's listed, OAuth sign-in completes, and its tools work in a chat. `main.log` has no `dropped` or `listen EACCES` lines for it |

## Claude Code

| # | Steps | Pass when |
| --- | --- | --- |
| T1 | `aws sso logout`, then `claude -p "say hi"` | `awsAuthRefresh` opens the browser sign-in, then the reply arrives |
| T2 | `claude -p "say hi"` again | No browser, quick reply |
| T3 | `claude`, then `/status` | Provider Amazon Bedrock, the configured region, the `default_tier` AIP as model |
| T4 | `/model` | Only the configured tiers, by name rather than ARN, with the `default_tier` model as Default; switching works |
| T5 | `claude --model us.anthropic.claude-sonnet-5 -p hi`, and `claude --model opus -p hi` if no Opus model is configured | Both refused. The client allowlist doesn't catch these; the refusal must come from IAM or an SCP as a `403 … not authorized` |
| T6 | Have a few turns | Usage goes up on the default model's AIP for the conversation and on the Haiku AIP for background tasks such as the session title |
| T7 | Look up the calls in CloudTrail or Bedrock invocation logs | The caller is the user's role session, `…/<email>` |
| T8 | Use tools, run a subagent, check `/context` | 1M-token window on Sonnet, cache reads above zero after the second turn, streaming works |
| T9 | With the policy installed, set a different model and `AWS_PROFILE` in the user settings file (`~/.claude/settings.json`) | `/status` lists the managed source (`Enterprise managed settings (plist)` on macOS), and the managed values win |
| T10 | In an open session, run `aws sso logout` in another terminal, then send a prompt | `awsAuthRefresh` runs once and the session continues, without a sign-in loop |
| T11 | Keep Desktop and Claude Code open together; sign out of one | The other keeps working |
| T12 | Leave both idle past the session durations | Note how often people must sign in again |

## Codex

These are manual configuration-effect checks, not repository automated tests. Run them after distributing the generated managed configuration.

| # | Steps | Pass when |
| --- | --- | --- |
| C1 | Install the Codex managed config and requirements, restart the client, open `/status` | Provider is `amazon-bedrock-runtime`; default model and Region match the generated TOML |
| C2 | In a disposable folder with `--sandbox read-only`, send `Reply with exactly: bedrock-ok` | Exact reply arrives without `OPENAI_API_KEY` or provider errors |
| C3 | Ask Codex to run read-only `pwd`, then ask a follow-up about the result | Local tool execution and the follow-up turn both work |
| C4 | Invoke the approved Luna, 6.1 Sol and 6.1 Terra Global or US IDs one at a time | All three complete and AWS logs show the employee's role session |
| C5 | Start a new local desktop task and a new IDE session after full restart | Both read the same managed provider and can complete the short prompt |
| C6 | With an ordinary employee role, invoke a fourth model that is already subscribed but not approved | IAM/SCP returns an authorization denial. A Marketplace/subscription error does not prove the model allowlist |
| C7 | In user `config.toml` or with `-c`, set another `model_provider`, another `model_providers.amazon-bedrock-runtime.aws.profile`, and `web_search = "live"` | `/status` still shows `amazon-bedrock-runtime` with the `CCWB` profile and the generated Region; web search stays disabled |
| C8 | Remove the Codex profile/files and restart | The managed Bedrock provider no longer applies; user `config.toml`, auth, plugins and projects remain intact |
| C9 | Let the SSO session expire (or run `aws sso logout --profile CCWB`), then send a new prompt | Codex runs the managed `auth_refresh` command (`aws sso login --profile CCWB`), you complete the browser sign-in, and the request continues without a manual restart |
| C10 | Repeat C9 in the ChatGPT desktop app started from the Dock or Start menu, not from a terminal | The browser sign-in opens. If Codex reports that it can't find `aws`, the app's `PATH` lacks the AWS CLI's folder; note the install location and how it was fixed |

The platform user guides list the exact copy commands, target paths and troubleshooting rows.
