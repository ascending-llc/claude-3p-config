# Test plan

Run this on each new platform, and again after changing the config schema or the generated policies. Run it twice: first with an administrator permission set to prove the configuration works, then with the permission set people will actually get, to prove its IAM policy is enough.

## Results

| Platform | Date | Versions | Result |
| --- | --- | --- | --- |
| macOS 26.7.1 | 2026-10-21 | Claude Desktop 2.26454.2, Claude Code 2.1.306, AWS CLI 2.37.12 | All passed, with the administrator permission set. Sonnet and Haiku only: the Opus tier and `default_tier` were added later and are untested. D4 passed with automatic client registration; re-run it with the pre-registered client and callback port 47280 |
| Windows 10 Home 22H2 (19045) | 2026-10-22 | Claude Desktop 3.1623.4 (Microsoft Store), Claude Code 2.1.308, AWS CLI 2.38.4 | All passed, with the administrator permission set and WSL2 installed. Sonnet and Haiku only. D4 failed with automatic client registration (port 53280 reserved by WSL2), then passed with a pre-registered client and callback port 47280 |

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
