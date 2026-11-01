#!/bin/sh
# Usage: bump-version.sh 1.2.3
set -eu
v=${1:?version}
echo "$v" > VERSION
sed -i 's/newTag: ".*"/newTag: "'"$v"'"/' config/default/kustomization.yaml
sed -i 's|image: ghcr.io/ferrix/capturing:.*|image: ghcr.io/ferrix/capturing:'"$v"'|' config/manager/manager.yaml
