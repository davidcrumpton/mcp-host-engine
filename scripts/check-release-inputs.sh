#!/bin/sh
# Guard the inputs that GoReleaser copies from the *working tree*.
#
# Why this exists: archives, .deb/.rpm/.apk packages and the container image all
# copy plugins/ and a config file straight from the checkout, not from git. So a
# file that is gitignored — a local plugin variant holding a real API key, say —
# would be published to every user without ever appearing in the repository.
# .gitignore already excludes several such plugins, which makes this a live
# risk rather than a hypothetical one.
#
# Run as a GoReleaser `before` hook, so a dirty tree fails the release instead of
# leaking. CI checks out a clean tree, so it always passes there.
#
# Usage: scripts/check-release-inputs.sh   (from anywhere)
set -eu

cd "$(dirname "$0")/.."

fail=0

# 1. Anything ignored or untracked under plugins/ would be shipped.
#
# A snapshot (MCPHE_RELEASE_DRY_RUN=1) builds locally and publishes nothing, so
# a dirty tree cannot leak there — warn instead of failing, otherwise
# `make snapshot` is unusable on a machine that keeps gitignored plugin
# variants around. A real release still fails hard.
extra=$(git status --porcelain --ignored -- plugins/ 2>/dev/null || true)
if [ -n "$extra" ]; then
    if [ "${MCPHE_RELEASE_DRY_RUN:-0}" = "1" ]; then
        echo "warning: plugins/ contains files that are not in git; they are" >&2
        echo "included in this snapshot build only, which is never published:" >&2
        printf '%s\n' "$extra" >&2
    else
        echo "refusing to package: plugins/ contains files that are not in git:" >&2
        printf '%s\n' "$extra" >&2
        echo >&2
        echo "Packages and the image copy plugins/ from the working tree, so these" >&2
        echo "would be published. Move them aside, or release from a clean clone." >&2
        fail=1
    fi
fi

# 2. The config baked into packages must never carry a credential. The
#    repo-root config.yaml is a local development file and is not shipped; the
#    packages use config.example.yaml, which must keep an empty token_secret.
if [ -f config.example.yaml ]; then
    secret=$(sed -n 's/^[[:space:]]*token_secret:[[:space:]]*//p' config.example.yaml |
        tr -d "\"'[:space:]")
    if [ -n "$secret" ]; then
        echo "refusing to package: config.example.yaml sets a non-empty token_secret" >&2
        fail=1
    fi
else
    echo "refusing to package: config.example.yaml is missing" >&2
    fail=1
fi

if [ "$fail" -ne 0 ]; then
    exit 1
fi

if [ "${MCPHE_RELEASE_DRY_RUN:-0}" = "1" ]; then
    echo "release inputs checked (dry run; see warnings above)"
else
    echo "release inputs are clean"
fi
