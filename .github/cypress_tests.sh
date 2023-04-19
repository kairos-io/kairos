#!/bin/bash

# Run agent in the background to bring the webui up
/usr/bin/kairos-agent webui &

pushd internal/webui/public || exit 1
# deps
npm ci
# cypress tests
npx cypress run --e2e -q
popd || exit