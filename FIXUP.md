# Issues requiring fixes

1. Revoke the name bear should pruge all sublabelled entries for bear.  It doesn't hurt functionality but it looks ugly!

```
✔ bear@purplemac-1 ~/Workspace/mcp/mcphe [main]
❯ cat tokens.yaml                                                                                                                           Sun Jun 21 15:37:54
bear:windows

✔ bear@purplemac-1 ~/Workspace/mcp/mcphe [main]
❯ ./mcphe token revoke bear                                                                                                                 Sun Jun 21 15:37:56
Revoked "bear"

✔ bear@purplemac-1 ~/Workspace/mcp/mcphe [main]
❯ cat tokens.yaml                                                                                                                           Sun Jun 21 15:38:06
bear:windows
bear
```