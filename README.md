# Flag migrator for custom contexts

Use this script if you have feature flags with user targeting (individual targets or rules) and want to migrate them to use custom contexts. By default this script will not make any changes -- it will only do a dry-run. See the optional arguments below to enable a migration.

## How to run the flag migrator locally

### One-time setup

Copy `.env.template` to `.env` and specify an API key. You can get one from the LaunchDarkly web app on the Authorization page (`/settings/authorization`). The API key should minimally have reader access to your project and environment (if running a dry-run) or sufficient access to submit approval requests to your project and environment (if running a migration).

Load your env file: `source .env`

### Build the script

Run: `go build main.go`

### Run the script in dry-run mode to identify which flags are safe to migrate

Run: `LD_API_KEY=$LD_API_KEY ./main`

You can add the `REPOSITORIES` argument to specify which repositories are ready for the migration. Consider specifying this argument if you have multiple distinct codebases in use within a single LaunchDarkly project and a subset of your codebases are ready for the migration (e.g. have updated to context-aware SDK versions) while another subset is not ready. See below for more information about this argument.

### Run the script in dry-run mode to identify what changes will be made

Run: `LD_API_KEY=$LD_API_KEY SCHEMA_FILE=schema.yml ./main`

### Run the script to migrate flags

Run: `LD_API_KEY=$LD_API_KEY SCHEMA_FILE=schema.yml MIGRATE=true ./main`

You can add the `BACKUP_MAINTAINER` argument to ensure that all approvals have at least one person or team notified. Consider specifying this argument with your own member ID so that you can get notified of all approvals which need to be manually distributed across your team. See below for more information about this argument.

## Optional arguments

You may add the following arguments to customize your results.

* `LD_HOST`: A different LaunchDarkly host if not using the commercial production site. Defaults to `https://app.launchdarkly.com`.
* `PROJECT_KEY`: The LaunchDarkly project key. Defaults to `default`.
* `ENVIRONMENT_KEY`: The LaunchDarkly environment key. Defaults to `production`.
* `SCHEMA_FILE`: The relative path to a YAML file containing the mapping from your user schema to your custom contexts schemas. Defaults to no file.
* `MIGRATE`: If specified, the script will create approvals for all flags which are safe to migrate. If unspecified, the script will instead run an informative dry-run. The value can be set to anything such as `true` - so long as it is not blank or omitted.
* `BACKUP_MAINTAINER`: The member id of the user who should be notified about approvals for flags where no maintainer is set. Defaults to none. You can get the member id by extracting it from the URL on the manage member page.
* `REPOSITORIES`: A comma-separated list of repository names (as used by [Code References](https://docs.launchdarkly.com/home/code/code-references)) to be used within a guardrail in the script. Repositories named in this argument will be considered ready for the migration and omitted repositories will be considered not ready. If unspecified, the script defaults to behavior where all repositories are considered ready.

## Schema file format

The schema file should be in YAML. The top-level attributes are your user attributes and each of those has `kind` and `attribute` child attributes which denote the custom context kind and attribute where the user attribute will map to.

For example, given a user...:

```json
{
  "key": "abcdef",
  "accountId": "ghijkl",
  "accountName": "Some Company",
  "device": "google-pixel-6",
  "name": "Some User",
  "userZipCode": 12345
}
```

... which is becoming the following multi-context...:

```json
{
  "kind": "multi",
  "account": {
    "key": "ghijkl",
    "name": "Some Company"
  },
  "device": {
    "key": "google-pixel-6"
  },
  "user": {
    "key": "abcdef",
    "name": "Some User",
    "zipCode": 12345
  }
}
```

... you would then want to define the following schema:

```yaml
accountId:
  kind: account
  attribute: key
accountName:
  kind: account
  attribute: name
device:
  kind: device
  attribute: key
userZipCode:
  kind: user
  attribute: zipCode
```

Note that we didn't provide a mapping for the `key` or `name` attributes since those are unchanged -- in the multi-context format they're mapped to attributes with the same name and context kind as before.

Also, in order to use this script to migrate individual targets, the `key` attribute (if provided) must be mapped to a `key` attribute within a custom context. This is because individual targets are stored as lists of keys. If your user key attribute maps to a non-key custom context attribute then you cannot use this script to automatically migrate your individual targets.