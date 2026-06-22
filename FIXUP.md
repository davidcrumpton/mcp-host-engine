# Issues requiring fixes

## ✅ Fixed Issues

### Logging

1. ✅ **Detailed Logs** - FIXED

The logging format now includes:

- Log level (e.g., [INFO], [WARN], [DEBUG])
- Identity (username or "-" if not authenticated)
- SessionID (or "-" if no session)
- Full message with context

Format: `2026-Jun-21 18:08:54 [INFO] bear WNV4OIG2LT5HY6JO4ISJHEKE33 - <message>`

This is now consistent across all logging calls in the HTTP transport, plugin execution, and HTTP client operations.

2. ✅ **Expired or revoked tokens** - FIXED

Token validation events are now logged:

- Successful token validation: `Token validation successful for user`
- Failed validation: `Token validation failed: <error details>`
- Legacy token usage: `Using legacy bearer token`

Error messages include details about expiry or revocation status.

## Upcoming Features

None discussed.  If there were some, they would be noted here.
