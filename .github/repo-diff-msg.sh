#!/bin/bash

yq -i -P '.|=sort_by(.name)|.[]|[{"name": .name, "category": .category, "version": .version}]' build/versions.old.yaml
yq -i -P '.|=sort_by(.name)|.[]|[{"name": .name, "category": .category, "version": .version}]' build/versions.new.yaml
echo "Bump of Kairos repositories" > pr-message
echo "--------------------------" >> pr-message
DIFF=$(diff -u build/versions.old.yaml build/versions.new.yaml)
if [[ $? == 1 ]]; then
  echo "> [\!WARNING]" >> pr-message
  echo "> There were changes to installed packages" >> pr-message
  echo "\`\`\`diff" >> pr-message
  echo "${DIFF}" >> pr-message
  echo "\`\`\`" >> pr-message
  echo "\n" >> pr-message
fi
echo "> [\!IMPORTANT]" >> pr-message
echo "> Full package list from new repo" >> pr-message
echo "\`\`\`yaml" >> pr-message
echo "$(cat build/versions.new.yaml)" >> pr-message
echo "\`\`\`" >> pr-message
exit 0