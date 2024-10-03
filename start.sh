#!/bin/bash
url=""
target=""
if [ -z "$GITHUB_REPOSITORY_NAME" ]; then
  url="https://$GITHUB_API_DOMAIN/orgs/$GITHUB_REPOSITORY_OWNER"
  target=$GITHUB_REPOSITORY_OWNER
else
  url="https://$GITHUB_API_DOMAIN/repos/$GITHUB_REPOSITORY_OWNER/$GITHUB_REPOSITORY_NAME"
  target=$GITHUB_REPOSITORY_OWNER/$GITHUB_REPOSITORY_NAME
fi
export TOKEN=`curl -L \
  -X POST \
  -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer $GITHUB_ACCESS_TOKEN" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  $url/actions/runners/registration-token | jq -r .token`
  /actions-runner/config.sh --url https://$GITHUB_DOMAIN/$target --token $TOKEN --ephemeral --labels $LABELS
  /actions-runner/run.sh --ephemeral
