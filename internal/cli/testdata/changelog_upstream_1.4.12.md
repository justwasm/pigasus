# 1.4.12

- Added: `"optional": true` for API key providers in a plugin's `auth.json`, for services that work without a key but raise rate limits with one: when you haven't set the key, the plugin's commands now see the variable as unset and make unauthenticated requests at the free rate limit, instead of failing with 401 errors or asking you to sign in.
- Fixed: Markdown tables in long streaming replies leaving misaligned borders and repeated text in your terminal scrollback: a table's rows now stay in the live view until the table is complete, so they reach scrollback with their final column widths.
