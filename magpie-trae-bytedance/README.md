# Trae ByteDance authentication for Magpie

This local Magpie plugin adds the `trae-bytedance` provider for ByteDance
employees. It signs in through Trae CN employee SSO and adapts the employee
model service to Magpie's OpenAI-compatible interface.

This package is separate from `@magpie-community/opencode-trae-auth` and does
not add, replace, or modify the public `trae-cn` provider.

## Requirements

- ByteDance employee account with the required Trae model entitlements
- Access to the ByteDance internal network
- Magpie 0.1.1290 or newer

The plugin does not bypass account entitlement, device, SSO, or network
controls.

## Install

Run this from the directory containing the package:

```bash
magpie plugin add "$PWD/magpie-trae-bytedance"
magpie plugin login trae-bytedance
```

Complete employee SSO in the browser window. Models are then available as:

```text
trae-bytedance/<model-id>
```

List and test the provider:

```bash
magpie plugin
magpie provider test trae-bytedance
```

## Security

Upstream requests are restricted in code to these HTTPS hosts:

- `solo-web.bytedance.net` for login and token refresh
- `copilot-cn.bytedance.net` for model discovery and chat
- `sso.bytedance.com` and `accounts.feishu.cn` for browser SSO navigation

The OAuth callback listens only on a random `127.0.0.1` port, accepts one
credential, and closes immediately. Credential values are not logged.

Magpie stores plugin provider credentials in
`~/.config/magpie/plugin-auth.json`. The Magpie configuration directory
should be accessible only by its owner, and credential-bearing files must
use mode `0600`.

## Live Check

Use a non-sensitive prompt after login:

```text
Reply with OK
```

Confirm that the response streams successfully, then test a harmless local
function such as `echo`. Do not paste employee tokens or callback URLs into
logs, issues, screenshots, or chat.

## Uninstall

```bash
magpie plugin logout trae-bytedance
magpie plugin rm opencode-trae-bytedance-auth
```

Removing the plugin does not affect `trae-cn` or its account.

## Development

```bash
npm test
```

Runtime code uses only Node.js/Bun built-ins. Portions are adapted from
`magpie-community/plugins` under the MIT License; see `NOTICE` and `LICENSE`.
