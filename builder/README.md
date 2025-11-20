# Builder initially taken from NimbusMC, which took it from Verifye (both by Zak :D)

to run the builder, provide the following env vars:
```env
# API token that can read the repository
GH_API_TOKEN=your_github_api_token 

# SHA of the commit you are mimicking the build of
GITHUB_SHA=your_commit_sha

# The workflow ref (path to workflow file and branch).
# This is printed in the logs when it is run in prod.
# Example: nimbus-mc/go-monorepo/.github/workflows/build.yaml@refs/heads/main
GITHUB_WORKFLOW_REF=your_workflow_ref

# The repository where the workflow is located (including org/username).
GITHUB_REPOSITORY=your_org/your_repo

# The branch name of the workflow ref.
GITHUB_REF_NAME=your_branch_name

# Force the last successful build SHA to be used instead of the actual last success.
OVERRIDE_LAST_SUCCESSFUL_BUILD_SHA=your_override_sha
```