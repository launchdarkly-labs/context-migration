package migrator

import (
    "context"
    "fmt"
    "io/ioutil"
    "log"
    "os"
    "strings"

    "gopkg.in/yaml.v3"

    ldapi "github.com/launchdarkly/api-client-go/v10"
)

var (
    apiKey             string
    projectKey         string
    envKey             string
    host               string
    backupMaintainer   string
    schemaFile         string
    repos              []string
    migrate            bool
    schema             map[string]attributeSchema
    client             *ldapi.APIClient
    ctx                context.Context
)

const userKind = "user"
const keyAttribute = "key"
const defaultProject = "default"
const defaultEnv = "production"
const defaultHost = "https://app.launchdarkly.com"

type targetInfo struct {
    target    ldapi.Target
    variation ldapi.Variation
}

type ruleInfo struct {
    ruleId    string
    clauses   []ldapi.Clause
}

type member struct {
    email string
    id    string
}

type flagDetails struct {
    targetUserRefs     []targetInfo
    ruleUserRefs       []ruleInfo
    guardrailsViolated bool
    maintainerTeamKey  string
    maintainerMember   member
}

type attributeSchema struct {
     Kind       string
     Attribute  string
}

func init() {
    args()
    prepareSchema()

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
        fmt.Printf("PROJECT_KEY is unspecified: using default value of %v\n", defaultProject)
    } else {
        fmt.Printf("PROJECT_KEY is provided: %v\n", projectKey)
    }

    envKey = os.Getenv("ENVIRONMENT_KEY")
    if envKey == "" {
        envKey = defaultEnv
        fmt.Printf("ENVIRONMENT_KEY is unspecified: using default value of %v\n", defaultEnv)
    } else {
        fmt.Printf("ENVIRONMENT_KEY is provided: %v\n", envKey)
    }

    host = os.Getenv("LD_HOST")
    if host == "" {
        host = defaultHost
        fmt.Printf("LD_HOST is unspecified: using default value of %v\n", defaultHost)
    } else {
        fmt.Printf("LD_HOST is provided: %v\n", host)
    }

    repoArg := os.Getenv("REPOSITORIES")
    if repoArg == "" {
        fmt.Printf("REPOSITORIES is unspecified: using default behavior where all repositories are ready\n")
    } else {
        fmt.Printf("REPOSITORIES is provided: %v\n", repoArg)
        repoNames := strings.Split(repoArg, ",")
        for _, repo := range repoNames {
            repos = append(repos, repo)
        }
    }

    schemaFile = os.Getenv("SCHEMA_FILE")
    if schemaFile == "" {
        fmt.Printf("SCHEMA_FILE is unspecified: using default behavior of having no schema\n")
    } else {
        fmt.Printf("SCHEMA_FILE is provided: %v\n", schemaFile)
    }

    migrateArg := os.Getenv("MIGRATE")
    if migrateArg == "" {
        migrate = false
        fmt.Printf("MIGRATE is unspecified: using default behavior of running a dry-run\n")
    } else if schemaFile != "" {
        migrate = true
        fmt.Printf("MIGRATE is provided: the script will run the migration!\n")
    } else {
        log.Fatal("MIGRATE is provided but SCHEMA_FILE is not. SCHEMA_FILE must also be provided to run the migration.")
        os.Exit(4)
    }

    backupMaintainer = os.Getenv("BACKUP_MAINTAINER")
    if backupMaintainer == "" {
        fmt.Printf("BACKUP_MAINTAINER is unspecified: using default behavior of having no backup maintainer\n")
    } else {
        fmt.Printf("BACKUP_MAINTAINER is provided: %v\n", backupMaintainer)
    }

    fmt.Println()
}

func prepareSchema() {
    if schemaFile == "" {
        return
    }

    file, err := ioutil.ReadFile(schemaFile)

    if err != nil {
         log.Fatal(err)
         os.Exit(2)
    }

    schema = make(map[string]attributeSchema)
    err = yaml.Unmarshal(file, &schema)

    if err != nil {
         log.Fatal(err)
         os.Exit(3)
    }

    fmt.Println("Using the following schema mappings:")
    for userAttribute, newAttribute := range schema {
         fmt.Printf("  %s: %s\n", userAttribute, newAttribute)
    }

    fmt.Println()
}

func Migrate() {
    flags, r, err := client.FeatureFlagsApi.GetFeatureFlags(ctx, projectKey).Env(envKey).Summary(false).Execute()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error when calling `FeatureFlagsApi.GetFeatureFlags``: %v\n", err)
        fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
    }
    fmt.Printf("Inspecting flags for project '%v' and environment '%v'.\n", projectKey, envKey)

    cntMigrateReady := 0
    cntGuardrail := 0
    cntNotNeeded := 0

    safeToMigrateBonusText := ""

    for _, flag := range flags.Items {
        details := inspectFlag(flag)
        
        if details.guardrailsViolated {
            cntGuardrail++
        } else if isFlagTargetingUsers(details) {
            cntMigrateReady++
        } else {
            cntNotNeeded++
        }

        if isFlagTargetingUsers(details) && !details.guardrailsViolated && len(schema) > 0 {
			prepareApproval(flag, details)
            if migrate {
                safeToMigrateBonusText = " Approval(s) have been submitted to flag maintainers for review."
            }
		}
    }

    fmt.Println()
    fmt.Printf("%v flag(s) found.\n", len(flags.Items))
    fmt.Printf(" - %v flag(s) contain user targeting and are safe to migrate.%v\n", cntMigrateReady, safeToMigrateBonusText)
    fmt.Printf(" - %v flag(s) aren't safe to migrate per the specified guardrails.\n", cntGuardrail)
    fmt.Printf(" - %v flag(s) do not need to be migrated.\n", cntNotNeeded)
}

func isFlagTargetingUsers(details flagDetails) bool {
    return len(details.targetUserRefs) > 0 || len(details.ruleUserRefs) > 0
}

func inspectFlag(flag ldapi.FeatureFlag) flagDetails {
    flagConfig := flag.Environments[envKey]
    details := flagDetails{}
    
    for _, target := range flagConfig.Targets {
        if *target.ContextKind == userKind {
            details.targetUserRefs = append(details.targetUserRefs, targetInfo{target, flag.Variations[target.Variation]})
        }
    }

    for _, rule := range flagConfig.Rules {
        clauses := make([]ldapi.Clause, 0)
        for _, clause := range rule.Clauses {
            if *clause.ContextKind == userKind {
                clauses = append(clauses, clause)
            }
        }

        if len(clauses) > 0 {
            details.ruleUserRefs = append(details.ruleUserRefs, ruleInfo{*rule.Id, clauses})
        }
    }

    if isFlagTargetingUsers(details) {
        maintainerTeamKey, maintainerMemberId, maintainerMemberEmail := getMaintainer(flag)
        details.maintainerTeamKey = maintainerTeamKey
        details.maintainerMember = member{maintainerMemberEmail, maintainerMemberId}
        details.guardrailsViolated = checkGuardrails(flag)

        if details.guardrailsViolated {
            fmt.Printf("Flag '%v' is not safe to be migrated because of the specified guardrails.\n", flag.Key)
        } else {
            maintainerType := "undefined"
            maintainer := "n/a"
            if details.maintainerMember.email != "" {
                maintainerType = "member"
                maintainer = details.maintainerMember.email
            } else if details.maintainerTeamKey != "" {
                maintainerType = "team"
                maintainer = details.maintainerTeamKey
            } else if backupMaintainer != ""{
                maintainerType = "backup"
                maintainer = backupMaintainer
            }
            fmt.Printf("Flag '%v' is safe to be migrated by the %v maintainer (%v).\n", flag.Key, maintainerType, maintainer)
        }
    }

    return details
}

func prepareApproval(flag ldapi.FeatureFlag, details flagDetails) {
    instructions := []map[string]interface{}{}
	
    // Add instructions to migrate individual targets
    for _, target := range details.targetUserRefs {
        mapping, isMapped := schema[keyAttribute]

        if isMapped {
            instructions = append(instructions, map[string]interface{}{
                "kind": interface{}("addTargets"),
                "contextKind": interface{}(mapping.Kind),
                "values": interface{}(target.target.Values),
                "variationId": interface{}(target.variation.Id),
            })
            instructions = append(instructions, map[string]interface{}{
                "kind": interface{}("removeTargets"),
                "contextKind": interface{}(userKind),
                "values": interface{}(target.target.Values),
                "variationId": interface{}(target.variation.Id),
            })
            fmt.Printf("  Adding instructions to migrate individual user targets to context kind '%v'.\n", schema[keyAttribute].Kind)
        } else {
            fmt.Printf("  Skipping individual user targets because no '%v' mapping was provided.\n", keyAttribute)
        }
    }

    // Add instructions to migrate rules
    for _, rule := range details.ruleUserRefs {
        toAdd, toRemove := toInstructionClauses(rule.clauses)
        if len(toAdd) > 0 && len(toRemove) > 0 {
            instructions = append(instructions, map[string]interface{}{
                "kind": interface{}("addClauses"),
                "ruleId": interface{}(rule.ruleId),
                "clauses": toAdd,
            })
            instructions = append(instructions, map[string]interface{}{
                "kind": interface{}("removeClauses"),
                "ruleId": interface{}(rule.ruleId),
                "clauses": toRemove,
            })
        }
    }

    // Add instructions to migrate percent rollouts
    // TODO

    if migrate {
        if len(instructions) > 0 {
            description := "Migrating " + flag.Key + " to use custom contexts."
            req := *ldapi.NewCreateFlagConfigApprovalRequestRequest(description, instructions)
        
            // Add the maintainer
            if details.maintainerMember.id != "" {
                req.NotifyMemberIds = []string{details.maintainerMember.id}
            } else if details.maintainerTeamKey != "" {
                req.NotifyTeamKeys = []string{details.maintainerTeamKey}
            } else if backupMaintainer != ""{
                req.NotifyMemberIds = []string{backupMaintainer}
            }

            _, r, err := client.ApprovalsApi.PostApprovalRequest(ctx, projectKey, flag.Key, envKey).CreateFlagConfigApprovalRequestRequest(req).Execute()
            if err != nil {
                fmt.Fprintf(os.Stderr, "Error when calling `FeatureFlagsBetaApi.GetDependentFlagsByEnv``: %v\n", err)
                fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
            } else {
                fmt.Printf("  Approval request submitted for flag '%v'!\n", flag.Key)
            }
        } else {
            fmt.Printf("  Skipping approval for flag '%v' because no mappings were provided.\n", flag.Key)
        }
    }
}

func toInstructionClauses(clauses []ldapi.Clause) ([]map[string]interface{}, []string) {
    toAdd := []map[string]interface{}{}
    toRemove := []string{}

    for _, clause := range clauses {
        mapping, isMapped := schema[clause.Attribute]

        if isMapped {
            toAdd = append(toAdd, map[string]interface{}{
                "attribute": interface{}(mapping.Attribute),
                "contextKind": interface{}(mapping.Kind),
                "negate": interface{}(clause.Negate),
                "op": interface{}(clause.Op),
                "values": interface{}(clause.Values),
            })
            toRemove = append(toRemove, *clause.Id)
            fmt.Printf("  Adding instructions to migrate a rule clause for user attribute '%v' to context kind '%v' and attribute '%v'.\n", clause.Attribute, mapping.Kind, mapping.Attribute)
        } else {
            fmt.Printf("  Skipping targeting rule clause for user attribute '%v' because no mapping was provided.\n", clause.Attribute)
        }
    }

    return toAdd, toRemove
}

func getMaintainer(flag ldapi.FeatureFlag) (string, string, string) {
    maintainerTeamKey := ""
    maintainerMemberId := ""
    maintainerMemberEmail := ""

    if flag.MaintainerTeamKey != nil {
        maintainerTeamKey = *flag.MaintainerTeamKey
    } else if flag.MaintainerId != nil {
        maintainerMemberId = *flag.MaintainerId
        maintainerMemberEmail = flag.Maintainer.Email
    }

    return maintainerTeamKey, maintainerMemberId, maintainerMemberEmail
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
    if len(repos) == 0 {
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