# Flag migrator for custom contexts

Use this script if you have feature flags with user targeting (individual targets or rules) and want to migrate them to use custom contexts. By default this script does not make any changes -- the script's default behavior is to do a dry-run.

## Migration methodology

LaunchDarkly is quite flexible in how it can be used. As a result, the way which our customers organize their feature flags can vary quite a bit from each other. This script intends to support the majority of our customers (and is open sourced so that it can be forked for any customers who need customizations). In this section we describe our migration methodology and how it applies to different project structures.

**Identifying flags which are safe to migrate:** Among [other requirements](https://docs.launchdarkly.com/guides/flags/upgrading-contexts#migrating-to-contexts), customers must update their SDK versions before migrating their feature flags. All SDKs which evaluate feature flags must be updated before those flags can be migrated. Some customers evaluate their feature flags with multiple SDKs. For example, different parts of their stack or different platforms might evaluate the same flags. Likewise, some customers share a LaunchDarkly project across multiple distinct codebases. Some customers might not even realize that they have flags which are evaluated in multiple codebases; since flag prerequisites get evaluated as part of their dependent flags' evaluations, these flag prerequisites similarly need to be considered.

As this script executes on a per-environment basis, the script needs to safely identify which flags in the specified environment can be migrated. By default, the script considers all codebases associated with an environment to be "ready for migration" -- and as a result, by default, the script considers all flags to be "ready for migration".

If one or more codebases in your environment aren't ready for migration, be sure to specify the `REPOSITORIES` argument in conjunction with our [code references feature](https://docs.launchdarkly.com/home/code/code-references) so that this script distinguishes between safe and unsafe flags. Based on this argument, the script only migrates flags which are solely located in the "safe" codebases. Similarly, if the `REPOSITORIES` argument is provided, the script omits all prerequisites just in case any of them are shared among multiple codebases. These "guardrails" intend to protect your LaunchDarkly environments from being migrated too early.

**Identifying how your user schema maps to your context schema**: Every customer differs in how they structure their attributes. The script requires customers to provide a mapping from their existing user schema to their newer context schema. The newer context schema could describe a single non-user context or it could describe a multi-context. Omitting user attributes from your schema results in them being omitted from the migration. See the "schema file format" section below for more information.

**Individual targets:** Individual targets are groupings of a variation, a context kind, and a list of context keys. For each flag that's safe to migrate, the script identifies individual targets associated with the user context kind and replaces them with individual targets for the mapped context kind and attribute.

**Targeting rules:** Targeting rules contain one or more clauses, and each of these clauses refers to a context kind and attribute. For each flag that's safe to migrate, the script identifies targeting rule clauses associated with the user context and replaces them with targeting rule clauses for the mapped context kind attribute.

**Percentage rollouts:** Each percentage rollouts refers to a context kind and attribute. This applies to both rule percentage rollouts and fallthrough percentage rollouts. For each flag that's safe to migrate, the script identifies percentage rollouts associated with the user context and replaces them with percentage rollouts for the mapped context kind and attribute.

**Segments:** Segments are re-usable lists of users or contexts. At this time, the script does not automatically migrate segments to contexts. This applies to both standard segments and big segments.

**How migration changes are applied:** The script doesn't commit any actual flag changes. Rather, the script proposes flag changes which humans need to explicitly review, approve, and apply. More specifically, the script uses LaunchDarkly's approvals functionality to tell flag maintainers what changes should occur for each flag. It is then the responsibility of each flag maintainer to make sure that the flag is safe to be migrated and that the changes look appropriate.

## How to run the flag migrator locally

### One-time setup

Copy `.env.template` to `.env` and specify an API key. You can get one from the LaunchDarkly web app on the Authorization page (`<LD_HOST>/settings/authorization` such as https://app.launchdarkly.com/settings/authorization). The API key should minimally have reader access to your project and environment (if running a dry-run) or sufficient access to submit approval requests to your project and environment (if running a migration).

Load your env file: `source .env`

### Build the script

Run: `go build main.go`

### Run the script in dry-run mode to identify which flags are safe to migrate

Run: `LD_API_KEY=$LD_API_KEY ./main`

You can add the `REPOSITORIES` argument to specify which repositories are ready for the migration. Consider specifying this argument if you have multiple distinct codebases in use within a single LaunchDarkly project and a subset of your codebases are ready for the migration (e.g. have updated to context-aware SDK versions) while another subset is not ready. See below for more information about this argument.

### Run the script in dry-run mode to identify what changes will be made

Run: `LD_API_KEY=$LD_API_KEY SCHEMA_FILE=schema.yml ./main`

This command runs the script following the migration methodology described above, with the exception that no approvals are submitted.

### Run the script to migrate flags

Run: `LD_API_KEY=$LD_API_KEY SCHEMA_FILE=schema.yml MIGRATE=true ./main`

This command runs the script following the migration methodology described above, concluding in submitting approvals to flag maintainers.

You can add the `BACKUP_MAINTAINER` argument to ensure that all approvals have at least one person or team notified. Consider specifying this argument with your own member ID so that you can get notified of all approvals which need to be manually distributed across your team. See below for more information about this argument.

## Optional arguments

You may add the following arguments to customize your results.

* `LD_HOST`: A different LaunchDarkly host if not using the commercial production site. Defaults to `https://app.launchdarkly.com`.
* `PROJECT_KEY`: The LaunchDarkly project key. Defaults to `default`.
* `ENVIRONMENT_KEY`: The LaunchDarkly environment key. Defaults to `production`.
* `SCHEMA_FILE`: The relative path to a YAML file containing the mapping from your user schema to your custom contexts schemas. Defaults to no file.
* `MIGRATE`: When specified, the script creates approvals for all flags which are safe to migrate. When unspecified, the script instead runs an informative dry-run. The value can be set to anything such as `true` - so long as it is not blank or omitted.
* `BACKUP_MAINTAINER`: The member id of the user who should be notified about approvals for flags where no maintainer is set. Defaults to none. You can get the member id by extracting it from the URL on the manage member page.
* `REPOSITORIES`: A comma-separated list of repository names (as used by [Code References](https://docs.launchdarkly.com/home/code/code-references)) to be used as a guardrail in the script. Repositories named in this argument are considered ready for the migration and omitted repositories are considered not ready; additionally, when provided, all prerequisites will be deemed "unsafe" in case they're used across both safe and unsafe repositories. If unspecified, the script defaults to behavior where all repositories are considered ready and as a result all flags in the environment are considered ready.

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
