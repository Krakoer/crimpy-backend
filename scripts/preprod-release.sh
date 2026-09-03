#!/usr/bin/env bash
# Promote dev to main. Pushing main makes the CI publish the :edge images that
# the preproduction stack runs.
set -euo pipefail

cd "$(dirname "$0")/.."

assume_yes=false
if [ "${1:-}" = "-y" ]; then
    assume_yes=true
elif [ $# -gt 0 ]; then
    echo "usage: $0 [-y]" >&2
    exit 1
fi

git fetch origin

if git merge-base --is-ancestor origin/dev origin/main; then
    echo "origin/main already contains origin/dev, nothing to promote"
    exit 0
fi

if ! git merge-base --is-ancestor origin/main origin/dev; then
    echo "origin/main holds commits origin/dev does not, promote by hand" >&2
    exit 1
fi

echo "Promoting to preproduction:"
git --no-pager log --oneline origin/main..origin/dev

if [ "$assume_yes" = false ]; then
    read -r -p "Push these to main? [y/N] " answer
    case $answer in
    y | Y) ;;
    *)
        echo "aborted" >&2
        exit 1
        ;;
    esac
fi

git push origin origin/dev:refs/heads/main

echo
echo "main updated, the CI is building krakoer/crimpy-api:edge and krakoer/crimpy-migrate:edge"
echo "once it is green, on the server: just preprod-up"
