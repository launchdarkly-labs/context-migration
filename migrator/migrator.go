package migrator

import (
    "context"
    "fmt"
    "log"
    "os"
    "strings"

    ldapi "github.com/launchdarkly/api-client-go/v10"
)

var (
    apiKey             string
    projectKey         string
    envKey             string
    host               string
    repos              []string
    client             *ldapi.APIClient
    ctx                context.Context
)

const userKind = "user"
const defaultProject = "default"
const defaultEnv = "production"
const defaultHost = "https://app.launchdarkly.com"

func init() {
    args()

    config := ldapi.NewConfiguration()
    config.Servers = ldapi.ServerConfigurations{
        {
            URL: host,
        },
    }
    config.AddDefaultHeader("LD-API-Version", "beta") //needed to determine prereqs
    client = ldapi.NewAPIClient(config)

    auth := make(map[string]ldapi.APIKey)
    auth["ApiKey"] = ldapi.APIKey{
        Key: apiKey,
    }

    ctx = context.Background()
    ctx = context.WithValue(ctx, ldapi.ContextAPIKeys, auth)

    fmt.Println()
}

func args() {
    apiKey = os.Getenv("LD_API_KEY")
    if apiKey == "" {
        log.Fatal("Must supply LD_API_KEY")
        os.Exit(1)
    }

    projectKey = os.Getenv("PROJECT_KEY")
    if projectKey == "" {
        projectKey = defaultProject
        fmt.Fprintf(os.Stdout, "PROJECT_KEY is unspecified: using default value of %v\n", defaultProject)
    } else {
        fmt.Fprintf(os.Stdout, "PROJECT_KEY is provided: %v\n", projectKey)
    }

    envKey = os.Getenv("ENVIRONMENT_KEY")
    if envKey == "" {
        envKey = defaultEnv
        fmt.Fprintf(os.Stdout, "ENVIRONMENT_KEY is unspecified: using default value of %v\n", defaultEnv)
    } else {
        fmt.Fprintf(os.Stdout, "ENVIRONMENT_KEY is provided: %v\n", envKey)
    }

    host = os.Getenv("LD_HOST")
    if host == "" {
        host = defaultHost
        fmt.Fprintf(os.Stdout, "LD_HOST is unspecified: using default value of %v\n", defaultHost)
    } else {
        fmt.Fprintf(os.Stdout, "LD_HOST is provided: %v\n", host)
    }

    repoArg := os.Getenv("REPOSITORIES")
    if repoArg == "" {
        fmt.Fprintf(os.Stdout, "REPOSITORIES is unspecified: using default behavior where all repositories are ready\n")
    } else {
        fmt.Fprintf(os.Stdout, "REPOSITORIES is provided: %v\n", repoArg)
        repoNames := strings.Split(repoArg, ",")
        for _, repo := range repoNames {
            repos = append(repos, repo)
        }
    }
}

func Migrate() {
    flags, r, err := client.FeatureFlagsApi.GetFeatureFlags(ctx, projectKey).Env(envKey).Summary(false).Execute()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error when calling `FeatureFlagsApi.GetFeatureFlags``: %v\n", err)
        fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
    }
    fmt.Fprintf(os.Stdout, "Inspecting flags for project %v and environment %v.\n", projectKey, envKey)

    cntMigrateReady := 0
    cntGuardrail := 0
    cntNotNeeded := 0

    for _, flag := range flags.Items {
        targetsUsers, guardrails := inspectFlag(flag)
        
        if guardrails {
            cntGuardrail++
        } else if targetsUsers {
            cntMigrateReady++
        } else {
            cntNotNeeded++
        }
    }

    fmt.Println()
    fmt.Fprintf(os.Stdout, "%v flag(s) found.\n", len(flags.Items))
    fmt.Fprintf(os.Stdout, " - %v flag(s) contain user targeting and are safe to migrate.\n", cntMigrateReady)
    fmt.Fprintf(os.Stdout, " - %v flag(s) aren't safe to migrate per the specified guardrails.\n", cntGuardrail)
    fmt.Fprintf(os.Stdout, " - %v flag(s) do not need to be migrated.\n", cntNotNeeded)
}

func inspectFlag(flag ldapi.FeatureFlag) (bool, bool) {
    targetsUsers := false
    guardrails := false

    flagConfig := flag.Environments[envKey]
    targets := flagConfig.Targets
    contextTargets := flagConfig.ContextTargets
    rules := flagConfig.Rules
    
    for _, target := range targets {
        if (*target.ContextKind == userKind) {
            targetsUsers = true
        }
    }

    for _, target := range contextTargets {
        if (*target.ContextKind == userKind) {
            targetsUsers = true
        }
    }

    for _, rule := range rules {
        for _, clause := range rule.Clauses {
            if (*clause.ContextKind == userKind) {
                targetsUsers = true
            }
        }
    }

    if targetsUsers {
        maintainerType, maintainer := getMaintainer(flag)
        guardrails = checkGuardrails(flag)

        if (guardrails) {
            fmt.Fprintf(os.Stdout, "Flag %v is not safe to be migrated because of the specified guardrails.\n", flag.Key)
        } else {
            fmt.Fprintf(os.Stdout, "Flag %v is safe to be migrated by the %v maintainer (%v).\n", flag.Key, maintainerType, maintainer)
        }
    }

    return targetsUsers, guardrails
}

func getMaintainer(flag ldapi.FeatureFlag) (string, string) {
    maintainerType := "no"
    maintainer := "n/a"
    if flag.MaintainerTeamKey != nil {
        maintainerType = "team"
        maintainer = *flag.MaintainerTeamKey
    } else if flag.MaintainerId != nil {
        maintainerType = "member"
        maintainer = flag.Maintainer.Email //*flag.MaintainerId
    }

    return maintainerType, maintainer
}

func checkGuardrails(flag ldapi.FeatureFlag) (bool) {
    // Each of these will be true if the corresponding guardrail has been violated.
    // They need to all be false for it to be safe to migrate a flag.
    isPrereq := guardrailPrereq(flag)
    isInUnsafeRepos := guardrailCodeRefs(flag)

    return isPrereq || isInUnsafeRepos
}

func guardrailPrereq(flag ldapi.FeatureFlag) (bool) {
    deps, r, err := client.FeatureFlagsBetaApi.GetDependentFlagsByEnv(ctx, projectKey, envKey, flag.Key).Execute()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error when calling `FeatureFlagsBetaApi.GetDependentFlagsByEnv``: %v\n", err)
        fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
    }

    return len(deps.Items) > 0
}

func guardrailCodeRefs(flag ldapi.FeatureFlag) (bool) {
    if (len(repos) == 0) {
        // skip the guardrail check because all repos are "ready"
        return false
    }

    stats, r, err := client.CodeReferencesApi.GetStatistics(ctx, projectKey).FlagKey(flag.Key).Execute()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Code references is an Enterprise feature. Your LaunchDarkly account must be on an Enterprise plan to use this guardrail.\n")
        fmt.Fprintf(os.Stderr, "Error when calling `CodeReferencesApi.GetStatistics``: %v\n", err)
        fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
    }

    for _, stat := range stats.Flags[flag.Key] {
        if !contains(repos, stat.Name) {
            return true
        }
    }

    return false
}

func contains(s []string, str string) bool {
    for _, v := range s {
        if v == str {
            return true
        }
    }

    return false
}