#!/bin/sh
# postinstall for the .deb / .rpm / .apk packages.
#
# Deliberately small: it creates the state directory the plugins may write to
# and tells the user the one thing they have to do next. It must stay
# idempotent, because it runs on every upgrade, and it must never fail the
# install over something cosmetic.
set -eu

# deb passes "configure <old-version>"; rpm and apk pass nothing useful.
case "${1:-}" in
configure | "") ;;
*) exit 0 ;;
esac

# Created 0755 and root-owned: the package cannot know which user will run
# mcphe, so it must not guess an owner. Point the memory plugin's data_dir at a
# directory your own user can write (see /etc/mcphe/config.yaml).
if [ ! -d /var/lib/mcphe ]; then
    install -d -m 0755 /var/lib/mcphe
fi

# Only mention the config when it still has the shipped, empty secret — after
# the user sets one, upgrades should stay quiet.
if [ -f /etc/mcphe/config.yaml ] &&
    grep -qE '^[[:space:]]*token_secret:[[:space:]]*(""|'"''"')[[:space:]]*$' /etc/mcphe/config.yaml; then
    cat <<'EOF'

mcphe installed. Before starting the HTTP daemon, set a token_secret in
/etc/mcphe/config.yaml, then mint a token:

    mcphe token create --username <you>

Without a token_secret the engine refuses to authenticate anyone, and it will
only listen on loopback. The plugins live in /usr/share/mcphe/plugins.

EOF
fi

exit 0
