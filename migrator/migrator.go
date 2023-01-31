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
	apiKey           string
	projectKey       string
	envKey           string
	host             string
	backupMaintainer string
	schemaFile       string
	repos            []string
	migrate          bool
	schema           map[string]attributeSchema
	client           *ldapi.APIClient
	ctx              context.Context
)

const userKind = "user"
const keyAttribute = "key"
const defaultProject = "default"
const defaultEnv = "production"
const defaultHost = "https://app.launchdarkly.com"

var attributesToIgnore = []string{
	"segmentMatch",     // Segments will be handled separate from flags, and this doesn't need to be migrated
	"not-segmentMatch", // Segments will be handled separate from flags, and this doesn't need to be migrated
	"kind",             // This attribute is new and doesn't need to be migrated
}

type targetInfo struct {
	target    ldapi.Target
	variation ldapi.Variation
}

type ruleInfo struct {
	ruleId  string
	clauses []ldapi.Clause
	rollout *ldapi.Rollout
}

type member struct {
	email string
	id    string
}

type flagDetails struct {
	targetUserRefs     []targetInfo
	ruleUserRefs       []ruleInfo
	fallthroughRollout *ldapi.Rollout
	guardrailsViolated bool
	maintainerTeamKey  string
	maintainerMember   member
	maintainerStr      string
	maintainerTypeStr  string
}

type attributeSchema struct {
	Kind      string
	Attribute string
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
	cntInstAdded := 0

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
			cntInstAdded += prepareApproval(flag, details)
			if migrate {
				safeToMigrateBonusText = " Approval(s) have been submitted to the flag maintainers for review."
			}
		}
	}

	fmt.Println()
	fmt.Printf("%v flag(s) found.\n", len(flags.Items))
	fmt.Printf(" - %v flag(s) contain user targeting and are safe to migrate.%v\n", cntMigrateReady, safeToMigrateBonusText)
	fmt.Printf(" - %v flag(s) aren't safe to migrate per the specified guardrails.\n", cntGuardrail)
	fmt.Printf(" - %v flag(s) do not need to be migrated.\n", cntNotNeeded)
	fmt.Println()
	fmt.Printf("This migration script automated %v change(s) across %v flag(s).\n", cntInstAdded, cntMigrateReady)
}

func isFlagTargetingUsers(details flagDetails) bool {
	return len(details.targetUserRefs) > 0 || len(details.ruleUserRefs) > 0 || details.fallthroughRollout != nil
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

		if len(clauses) > 0 || rule.Rollout != nil {
			details.ruleUserRefs = append(details.ruleUserRefs, ruleInfo{*rule.Id, clauses, rule.Rollout})
		}
	}

	if flagConfig.Fallthrough != nil && flagConfig.Fallthrough.Rollout != nil {
		rollout := flagConfig.Fallthrough.Rollout
		if *rollout.ContextKind == userKind {
			details.fallthroughRollout = rollout
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
			details.maintainerTypeStr = "undefined"
			details.maintainerStr = "n/a"
			if details.maintainerMember.email != "" {
				details.maintainerTypeStr = "member"
				details.maintainerStr = details.maintainerMember.email
			} else if details.maintainerTeamKey != "" {
				details.maintainerTypeStr = "team"
				details.maintainerStr = details.maintainerTeamKey
			} else if backupMaintainer != "" {
				details.maintainerTypeStr = "backup"
				details.maintainerStr = backupMaintainer
			}
			fmt.Printf("Flag '%v' is safe to be migrated by the %v maintainer (%v).\n", flag.Key, details.maintainerTypeStr, details.maintainerStr)
		}
	}

	return details
}

func prepareApproval(flag ldapi.FeatureFlag, details flagDetails) int {
	instructions := []map[string]interface{}{}

	// Add instructions to migrate individual targets
	for _, target := range details.targetUserRefs {
		mapping, isMapped := schema[keyAttribute]

		if isMapped {
			instructions = append(instructions, map[string]interface{}{
				"kind":        interface{}("addTargets"),
				"contextKind": interface{}(mapping.Kind),
				"values":      interface{}(target.target.Values),
				"variationId": interface{}(target.variation.Id),
			})
			instructions = append(instructions, map[string]interface{}{
				"kind":        interface{}("removeTargets"),
				"contextKind": interface{}(userKind),
				"values":      interface{}(target.target.Values),
				"variationId": interface{}(target.variation.Id),
			})
			fmt.Printf("  Adding instructions to replace individual user targets with individual '%v' targets.\n", schema[keyAttribute].Kind)
		} else {
			fmt.Printf("  Skipping individual user targets because no '%v' mapping was provided.\n", keyAttribute)
		}
	}

	// Add instructions to migrate rules
	for _, rule := range details.ruleUserRefs {
		// Rule clauses
		toAdd, toRemove := toInstructionClauses(rule.clauses)
		if len(toAdd) > 0 && len(toRemove) > 0 {
			instructions = append(instructions, map[string]interface{}{
				"kind":    interface{}("addClauses"),
				"ruleId":  interface{}(rule.ruleId),
				"clauses": toAdd,
			})
			instructions = append(instructions, map[string]interface{}{
				"kind":      interface{}("removeClauses"),
				"ruleId":    interface{}(rule.ruleId),
				"clauseIds": toRemove,
			})
		}

		// Rule rollouts
		instruction := handleRollout(flag, rule.rollout, &rule.ruleId)
		if instruction != nil {
			instructions = append(instructions, *instruction)
		}
	}

	// Add instructions to migrate fallthrough rollouts
	if details.fallthroughRollout != nil {
		instruction := handleRollout(flag, details.fallthroughRollout, nil)
		if instruction != nil {
			instructions = append(instructions, *instruction)
		}
	}

	if migrate {
		if len(instructions) > 0 {
			description := "Migrating " + flag.Key + " to use custom contexts."
			req := *ldapi.NewCreateFlagConfigApprovalRequestRequest(description, instructions)

			// Add the maintainer
			if details.maintainerMember.id != "" {
				req.NotifyMemberIds = []string{details.maintainerMember.id}
			} else if details.maintainerTeamKey != "" {
				req.NotifyTeamKeys = []string{details.maintainerTeamKey}
			} else if backupMaintainer != "" {
				req.NotifyMemberIds = []string{backupMaintainer}
			}

			_, r, err := client.ApprovalsApi.PostApprovalRequest(ctx, projectKey, flag.Key, envKey).CreateFlagConfigApprovalRequestRequest(req).Execute()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error when calling `FeatureFlagsBetaApi.GetDependentFlagsByEnv``: %v\n", err)
				fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
			} else {
				fmt.Printf("  An approval request has been submitted to %v maintainer '%v' for flag '%v'!\n", details.maintainerTypeStr, details.maintainerStr, flag.Key)
			}
		} else {
			fmt.Printf("  Skipping the approval for flag '%v' because no mappings were provided.\n", flag.Key)
		}
	}

	return len(instructions)
}

func handleRollout(flag ldapi.FeatureFlag, rollout *ldapi.Rollout, ruleId *string) *map[string]interface{} {
	if rollout == nil {
		return nil
	}

	rolloutType := "a rule"
	instructionKind := "updateRuleVariationOrRollout"
	if ruleId == nil {
		rolloutType = "the fallthrough"
		instructionKind = "updateFallthroughVariationOrRollout"
	}

	attribute := keyAttribute
	if rollout.BucketBy != nil {
		attribute = *rollout.BucketBy
	}
	mapping, isMapped := schema[attribute]

	if isMapped {
		fmt.Printf("  Adding an instruction to replace %v rollout for user attribute '%v' with %v rollout for '%v' attribute '%v'.\n", rolloutType, attribute, rolloutType, mapping.Kind, mapping.Attribute)
		instruction := map[string]interface{}{
			"kind":               interface{}(instructionKind),
			"rolloutContextKind": interface{}(mapping.Kind),
			"rolloutBucketBy":    interface{}(mapping.Attribute),
			"rolloutWeights":     toRolloutWeights(flag, rollout.Variations),
		}

		if ruleId != nil {
			instruction["ruleId"] = interface{}(ruleId)
		}

		return &instruction
	} else {
		fmt.Printf("  Skipping the fallthrough rollout for user attribute '%v' because no mapping was provided.\n", attribute)
		return nil
	}
}

func toInstructionClauses(clauses []ldapi.Clause) ([]map[string]interface{}, []string) {
	toAdd := []map[string]interface{}{}
	toRemove := []string{}

	for _, clause := range clauses {
		mapping, isMapped := schema[clause.Attribute]

		if !contains(attributesToIgnore, clause.Attribute) {
			if isMapped {
				toAdd = append(toAdd, map[string]interface{}{
					"attribute":   interface{}(mapping.Attribute),
					"contextKind": interface{}(mapping.Kind),
					"negate":      interface{}(clause.Negate),
					"op":          interface{}(clause.Op),
					"values":      interface{}(clause.Values),
				})
				toRemove = append(toRemove, *clause.Id)
				fmt.Printf("  Adding instructions to replace a rule clause for user attribute '%v' with a rule clause for '%v' attribute '%v'.\n", clause.Attribute, mapping.Kind, mapping.Attribute)
			} else {
				fmt.Printf("  Skipping a targeting rule clause for user attribute '%v' because no mapping was provided.\n", clause.Attribute)
			}
		}
	}

	return toAdd, toRemove
}

func toRolloutWeights(flag ldapi.FeatureFlag, weights []ldapi.WeightedVariation) map[string]int32 {
	wvs := map[string]int32{}

	for _, wv := range weights {
		wvs[*flag.Variations[wv.Variation].Id] = wv.Weight
	}
	return wvs
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

func checkGuardrails(flag ldapi.FeatureFlag) bool {
	// Each of these will be true if the corresponding guardrail has been violated.
	// They need to all be false for it to be safe to migrate a flag.
	isPrereq := guardrailPrereq(flag)
	isInUnsafeRepos := guardrailCodeRefs(flag)

	return isPrereq || isInUnsafeRepos
}

func guardrailPrereq(flag ldapi.FeatureFlag) bool {
	if len(repos) == 0 {
		// skip the guardrail check because all flags in this environment are deemed to be safe
		return false
	}

	deps, r, err := client.FeatureFlagsBetaApi.GetDependentFlagsByEnv(ctx, projectKey, envKey, flag.Key).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `FeatureFlagsBetaApi.GetDependentFlagsByEnv``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}

	return len(deps.Items) > 0
}

func guardrailCodeRefs(flag ldapi.FeatureFlag) bool {
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

	flagStats := stats.Flags[flag.Key]
	for _, stat := range flagStats {
		if !contains(repos, stat.Name) {
			return true
		}
	}

	// If we've reached this point, the script argument denotes that at least one repository is "safe".
	// Let's mark repositories with no code references as "unsafe" because we don't know whether or not they're safe.
	return len(flagStats) == 0
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}
