# Flag migrator for custom contexts

Use this script if you have feature flags with user targeting (individual targets or rules) and want to migrate them to use custom contexts.

## How to run the flag migrator locally

### One-time setup

Copy `.env.template` to `.env` and specify an API key. You can get one from the LaunchDarkly web app on the Authorization page (`/settings/authorization`). The API key should minimally have reader access to your project and environment.

Load your env file: `source .env`

### Build and run

Build: `go build main.go`

Run the script: `LD_API_KEY=$LD_API_KEY ./main`

## Optional arguments

You may add the following arguments to customize your results.

* `LD_HOST`: A different LaunchDarkly host if not using the commercial production site. Defaults to `https://app.launchdarkly.com`.
* `PROJECT_KEY`: The project key. Defaults to `default`.
* `ENVIRONMENT_KEY`: The environment key. Defaults to `production`.
* `REPOSITORIES`: A comma-separated list of repository names (as used by [Code References](https://docs.launchdarkly.com/home/code/code-references)) to be used within a guardrail in the script. Repositories named in this argument will be considered ready for the migration and omitted repositories will be considered not ready. Defaults to behavior where all repositories are considered ready.