#!/bin/bash
export TOKEN=`curl -L \
  -X POST \
  -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer $GITHUB_ACCESS_TOKEN" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  https://api.github.com/repos/$GITHUB_REPOSITORY_OWNER/$GITHUB_REPOSITORY_NAME/actions/runners/registration-token | jq -r .token`
  /actions-runner/config.sh --url https://github.com/$GITHUB_REPOSITORY_OWNER/$GITHUB_REPOSITORY_NAME --token $TOKEN --ephemeral
  /actions-runner/run.sh --ephemeral
