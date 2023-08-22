#!/bin/bash

yq -i -P '.|=sort_by(.name)|.[]|[{"name": .name, "category": .category, "version": .version}]' build/versions.old.yaml
yq -i -P '.|=sort_by(.name)|.[]|[{"name": .name, "category": .category, "version": .version}]' build/versions.new.yaml
echo "Bump of Kairos repositories" > pr-message
echo "--------------------------" >> pr-message
DIFF=$(diff -u build/versions.old.yaml build/versions.new.yaml)
if [[ $? == 1 ]]; then
  {
    echo "> [\!WARNING]"
    echo "> There were changes to installed packages"
    echo "\`\`\`diff"
    echo "${DIFF}"
    echo "\`\`\`"
    echo
  } >> pr-message
fi

{
  echo "> [\!IMPORTANT]"
  echo "> Full package list from new repo"
  echo "\`\`\`yaml"
  cat build/versions.new.yaml
  echo "\`\`\`"
} >> pr-message

exit 0
