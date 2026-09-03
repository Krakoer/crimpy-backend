#!/usr/bin/env bash
# Cut a production release: tag the commit preproduction is already running so
# the CI publishes the :vX.Y.Z and :latest images that the prod stack runs.
set -euo pipefail

cd "$(dirname "$0")/.."

usage() {
    echo "usage: $0 <major|minor|patch|X.Y.Z> [-y]" >&2
    exit 1
}

[ $# -ge 1 ] || usage
bump=$1
shift

assume_yes=false
if [ "${1:-}" = "-y" ]; then
    assume_yes=true
    shift
fi
[ $# -eq 0 ] || usage

case $bump in
major | minor | patch) ;;
*)
    [[ $bump =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || usage
    ;;
esac

if [ -n "$(git status --porcelain)" ]; then
    echo "working tree is dirty, commit or stash first" >&2
    exit 1
fi

branch=$(git rev-parse --abbrev-ref HEAD)
if [ "$branch" != "dev" ]; then
    echo "releases are cut from dev, currently on $branch" >&2
    exit 1
fi

git fetch origin --tags

if [ "$(git rev-parse dev)" != "$(git rev-parse origin/dev)" ]; then
    echo "dev and origin/dev have diverged, pull or push first" >&2
    exit 1
fi

if [ "$(git rev-parse origin/main)" != "$(git rev-parse origin/dev)" ]; then
    echo "origin/main is not origin/dev: run scripts/preprod-release.sh and validate preproduction first" >&2
    exit 1
fi

previous=$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -n 1)
current=${previous#v}
[ -n "$current" ] || current=0.0.0

IFS=. read -r major minor patch <<<"$current"
case $bump in
major) next="$((major + 1)).0.0" ;;
minor) next="$major.$((minor + 1)).0" ;;
patch) next="$major.$minor.$((patch + 1))" ;;
*) next=$bump ;;
esac

tag="v$next"
if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
    echo "$tag already exists" >&2
    exit 1
fi

if [ -n "$previous" ]; then
    echo "Releasing $tag, changes since $previous:"
    git --no-pager log --oneline "$previous..HEAD"
else
    echo "Releasing $tag, the first release"
fi

if [ "$assume_yes" = false ]; then
    read -r -p "Tag $tag and push it? [y/N] " answer
    case $answer in
    y | Y) ;;
    *)
        echo "aborted" >&2
        exit 1
        ;;
    esac
fi

git tag -a "$tag" -m "$tag"
git push origin "$tag"

echo
echo "$tag pushed, the CI is building krakoer/crimpy-api:$tag and krakoer/crimpy-migrate:$tag"
echo "once it is green, on the server: set VERSION=$tag in .env.prod, then just prod-pull"
