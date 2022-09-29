#!/bin/bash
set -e

# not used in netlify

"${ROOT_DIR}"/scripts/build.sh

git branch -D gh-pages || true

git checkout --orphan gh-pages

git rm -rf .

cp -rfv public/* ./
rm -rf public/
