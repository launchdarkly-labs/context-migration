# Flag migrator for custom contexts

Use this script if you have feature flags with user targeting (individual targets or rules) and want to migrate them to use custom contexts. By default, this script does not make any changes -- the script's default behavior is to do a dry-run.

## Migration methodology

LaunchDarkly customers use our flexible platform in many different ways. The way in which customers organize their feature flags vary significantly from each other. This script should support the majority of our customers as they migrate to custom contexts. If it doesn't work for you, the script is open source--you can fork it and make any customizations you need.

In this section, you'll learn about our migration methodology and how it applies to different project structures. Before you start, review the migration requirements in our [product documentation](https://docs.launchdarkly.com/guides/flags/upgrading-contexts#migrating-to-contexts).

**Identifying which flags are safe to migrate:** First, you must upgrade your SDKs to versions that support contexts. Some customers evaluate their feature flags with multiple SDKs. If this statement applies to you, you must update every SDK you use before you can migrate the flags that rely on them. For example, different parts of your stack or different platforms might evaluate the same flags, or perhaps multiple codebases use the same LaunchDarkly project. Consider whether you have flags which are evaluated in multiple codebases; because flag prerequisites get evaluated as part of their dependent flags' evaluations, these flag prerequisites also need to be considered for migration.

The migration script runs on a per-environment basis, so it needs to correctly identify which flags in the specified environment can be migrated. By default, the script assumes all codebases associated with an environment are ready for migration, and as a result, the script considers all flags in that envirovnment to be ready for migration.

If one or more codebases in your environment aren't ready for migration, specify the `REPOSITORIES` argument in conjunction with LaunchDarkly's code references feature. This lets the script distinguish between flags that are and aren't ready for migration. Based on this argument, the script only migrates flags that are solely located in the codebases that are ready to migrate. If you provide the `REPOSITORIES` argument, the script omits all prerequisites in case any of them are shared among multiple codebases. These guardrails should protect your LaunchDarkly flags from being migrated before they're ready. To learn more about using code references, read the [product documentation](https://docs.launchdarkly.com/home/code/code-references).

Additionally, don't migrate flags that you're using in an active and running experiment. The script identifies these flags and marks them as unsafe to migrate.

**Identifying how your user schema maps to your context schema**: Every customer structures their attributes differently. The script requires you to provide a map from their existing user schema to their newer context schema. The newer context schema could describe a single non-user context or it could describe a multi-context. If you omit user attributes from your schema, they will be ommitted from the migration. The "schema file format" section below provides for more information.

**Individual targets:** Individual targets are groupings of a variation, a context kind, and a list of context keys. For each flag that's safe to migrate, the script identifies individual targets associated with the user context kind and replaces them with individual targets for the mapped context kind and attribute.

**Targeting rules:** Targeting rules contain one or more clauses. Each clause refers to a context kind and attribute. For every flag that's safe to migrate, the script identifies targeting rule clauses associated with the user context and replaces them with targeting rule clauses for the mapped context kind attribute.

**Percentage rollouts:** Each percentage rollout refers to a context kind and attribute. This applies to both rule percentage rollouts and fallthrough percentage rollouts. For each flag that's safe to migrate, the script identifies percentage rollouts associated with the user context and replaces them with percentage rollouts for the mapped context kind and attribute.

**Segments:** Segments are reusable lists of users or contexts. Big Segments are segments that can contrain tens of thousands of users or contexts. The script does not automatically migrate segments or Big Segments to contexts.

**How the script applies migration changes:** The script doesn't commit any actual flag changes. Instead, the script proposes flag changes which humans need to explicitly review, approve, and apply. To do this, the script uses LaunchDarkly's approvals feature to tell flag maintainers what changes should occur for each flag. Each flag maintainer must verify that the flag is safe to be migrated and that the changes look appropriate. To learn more about identifying flag maintainers, read the [product documentation](https://docs.launchdarkly.com/home/flags/settings#maintainer).

## How to run the flag migrator locally

### One-time setup

Here are the steps you must complete the first time you run the script:

1.  Navigate to the Authorization page in your LaunchDarkly UI at `<LD_HOST>/settings/authorization`. For example: https://app.launchdarkly.com/settings/authorization.
2. Create an API key. If you want to test the script without changing anything, the API key you use should at least have reader access to your project and environment. If you want to execute the migration, the API key must have sufficient access to submit approval requests to your project and environment. To learn more, read the [product documentation](https://docs.launchdarkly.com/home/account-security/api-access-tokens).
3. Copy `.env.template` to `.env` and specify the API key you created.
4. Load your env file: `source .env`

The script is now ready to build and run.

### Build the script

Run: `go build main.go`

### Run the script in dry-run mode to identify which flags are safe to migrate

Run: `LD_API_KEY=$LD_API_KEY ./main`

You can add the `REPOSITORIES` argument to specify which repositories are ready for the migration. Consider specifying this argument if you have multiple distinct codebases in use within a single LaunchDarkly project and some, but not all, of your codebases are ready. Continue reading to learn more about this argument.

### Run the script in dry-run mode to identify what changes will be made

Run: `LD_API_KEY=$LD_API_KEY SCHEMA_FILE=schema.yml ./main`

This command runs the script following the migration methodology described above, but no approvals are submitted.

### Run the script to migrate flags

Run: `LD_API_KEY=$LD_API_KEY SCHEMA_FILE=schema.yml MIGRATE=true ./main`

This command runs the script following the migration methodology described above, concluding by submitting approval requests to flag maintainers.

You can add the `BACKUP_MAINTAINER_MEMBER` or `BACKUP_MAINTAINER_TEAM` arguments to ensure that all approvals have at least one person or team notified. Consider using these arguments with your own member ID or your own team's key. If you do this, you will get notified of all approvals and can later distribute them among your team. Continue reading for more information about this argument.

## Optional arguments

Optionally, you may add the following arguments to customize your results:

* `LD_HOST`: A different LaunchDarkly host if you are not using the commercial production site. Defaults to `https://app.launchdarkly.com`.
* `LD_PROJECT`: The key of the LaunchDarkly project you wish to migrate. Defaults to `default`.
* `LD_ENVIRONMENT`: The key of theLaunchDarkly environment you wish to migrate. Defaults to `production`.
* `LD_FLAGS`: A comma-separated list of flag keys you wish to migrate. Defaults to migrating all flags.
* `SCHEMA_FILE`: The relative path to a YAML file that contains the mapping from your user schema to your custom contexts schemas. Defaults to no file.
* `MIGRATE`: When this is specified, the script creates approvals for all flags which are safe to migrate. When unspecified, the script performs an informative dry-run instead. You can set the value for this argument to anything, such as `true`, but it cannot be blank or omitted.
* `BACKUP_MAINTAINER_MEMBER`: The member ID of the user who should be notified about approval requests for flags where no maintainer is set. Defaults to `none`. You can get the member ID by extracting it from the URL on the "Manage member" page.
* `BACKUP_MAINTAINER_TEAM`: The key of the team that should be notified about approval requests for flags where no maintainer is set. Defaults to `none`. You can get the team key from the Teams list (which is at `<LD_HOST>/settings/teams`. For example: https://app.launchdarkly.com/settings/teams). If both this and `BACKUP_MAINTAINER_MEMBER` are provided, `BACKUP_MAINTAINER_TEAM` takes precedence.
* `REPOSITORIES`: A comma-separated list of repository names (as used by [code references](https://docs.launchdarkly.com/home/code/code-references)) to be used as a guardrail in the script. Repositories named in this argument are considered ready for the migration and omitted repositories are considered not ready; additionally, when provided, all prerequisites will be deemed "unsafe" in case they're used across both safe and unsafe repositories. If unspecified, the script defaults to behavior where all repositories are considered ready and all flags in the environment are considered ready.

## Formatting the schema file

The schema file should be in YAML. The top-level attributes are your user attributes and each of those has `kind` and `attribute` child attributes that denote the custom context kind and attribute where the user attribute will map to.

For example, given a user:

```json
{
  "key": "user-key-123",
  "accountId": "account-id-abc",
  "accountName": "Some Company",
  "device": "google-pixel-6",
  "name": "User Name",
  "userZipCode": 12345
}
```

... which is becoming the following multi-context...:

```json
{
  "kind": "multi",
  "account": {
    "key": "key-123",
    "name": "Some Company"
  },
  "device": {
    "key": "google-pixel-6"
  },
  "user": {
    "key": "user-key-abc",
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

We didn't provide a mapping for the `key` or `name` attributes because those do not change. In the multi-context format, they're mapped to attributes with the same name and context kind as before.

Also, in order to use this script to migrate individual targets, if you provide a `key` attribute, you must map it to a `key` attribute in a custom context. This is because individual targets are stored as lists of keys. If your user key attribute maps to a non-key custom context attribute, you cannot use this script to automatically migrate your individual targets.
