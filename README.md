# local-runner-controller

Local Runner Controller is an application that builds images, executes containers, and registers runners to GitHub in order to run GitHub Actions runners on local machines.
CI/CD can be executed on a local machine without consuming cloud computing resources (e.g. [AWS](https://aws.amazon.com)). A GitHub Actions job is executed as a single container, and when the job finishes, the container is deleted and a new container is launched.
CI/CD execution based on user actions such as [push](https://docs.github.com/en/actions/writing-workflows/choosing-when-your-workflow-runs/events-that-trigger-workflows#push) and [pull_request_target](https://docs.github.com/en/actions/writing-workflows/choosing-when-your-workflow-runs/events-that-trigger-workflows#pull_request_target) can be executed using local machine resources. On the other hand, it is not recommended to use it for jobs that are not based on user operations (more precisely, the user's PC or laptop may be shut down when cron is started), such as cron executions.
like: [ARC](https://github.com/actions/actions-runner-controller)

## Prerecuirements

- [Go](https://go.dev/)
- [Docker](https://www.docker.com/)

## Setup

1. Determine to use GitHub Apps or GitHub personal access tokens
    - [GitHub Apps](https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/about-creating-github-apps)
    - [GitHub personal access tokens](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)

2. Make config.json

  ```json
  {
    "github": {
      "repository": {
        "owner": "OWNER_NAME",
        "name": "REPO_NAME"
      },
      "auth": {
        "is_app": true,
        "access_token": "github_pat_xxxx",
        "app": {
          "id": 0,
          "installation_id": 0,
          "key_path": "/path/to/file_name.private-key.pem"
        }
      }
    },
    "runner_limit": 1
  }
  ```

## Configuration's meanings

| name | meanings | required | required condition | default |
| --- | ---  | --- | --- | --- |
| github.repository.owner | Name of the owner of the repository where the runner is registered | true | always | - |
| github.repository.name | Name of the repository where the runner is registered | true | always | - |
| github.auth.is_app | Whether authentication is done by app or not | false | - | false |
| github.auth.access_token | GitHub personal access tokens | true | github.auth.is_app is false | ""(empty) |
| github.auth.app.id | GitHub Apps ID | true | github.auth.is_app is true | 0 |
| github.auth.app.installation_id | Installation ID of GitHub Apps | true | github.auth.is_app is true | 0 |
| github.auth.app.key_path | GitHub Apps private key path | true | github.auth.is_app is true | ""(empty) |

## How to start

```bash
go run main.go
```

## Author

tkmsaaaam
