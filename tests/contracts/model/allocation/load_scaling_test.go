package allocation_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/TomazVidovic533/stackpuddle/model"
)

const (
	fewerWorkloads = 8_000
	moreWorkloads  = 32_000
	// workloadsPerFile keeps each file inside the per-file value limit.
	workloadsPerFile = 8_000
	// Each workload is named by membershipsPerWorkload applications of membersPerApp members, the
	// schema's maximum, so the file holds several references per declaration.
	membershipsPerWorkload = 4
	membersPerApp          = 256
	scalingRounds          = 3
	// linearRatioCeiling sits between the 4x a linear Load takes for 4x the workloads and the 16x
	// of a scan per reference, with room for noise from other test processes.
	linearRatioCeiling = 8.0
	// fewerOverrides options of one module instance are overridden by as many environments, so the
	// override checks walk over half their budget and the project loads. moreOverrides is four
	// times both: checks that walked every option per override would walk sixteen times as much.
	fewerOverrides = 384
	moreOverrides  = 1536
)

func TestLoadTimeGrowsLinearlyWithDeclarations(t *testing.T) {
	fewer, more := scalingProject(fewerWorkloads), scalingProject(moreWorkloads)
	var fewerBest, moreBest time.Duration
	for round := range scalingRounds {
		fewerTime, moreTime := timeLoad(t, fewer, fewerWorkloads), timeLoad(t, more, moreWorkloads)
		if round == 0 || fewerTime < fewerBest {
			fewerBest = fewerTime
		}
		if round == 0 || moreTime < moreBest {
			moreBest = moreTime
		}
	}
	ratio := float64(moreBest) / float64(fewerBest)
	t.Logf("best of %d: %d workloads in %v, %d workloads in %v, ratio %.2f", scalingRounds, fewerWorkloads, fewerBest, moreWorkloads, moreBest, ratio)
	if ratio >= linearRatioCeiling {
		t.Fatalf("4x the workloads took %.2fx the time, want below %.1fx", ratio, linearRatioCeiling)
	}
}

func timeLoad(t *testing.T, files fstest.MapFS, workloads int) time.Duration {
	t.Helper()
	start := time.Now()
	project, diagnostics := model.Load(files, rootFile, nil)
	elapsed := time.Since(start)
	if len(diagnostics) > 0 {
		t.Fatalf("the scaling project must load: %v", diagnostics[0])
	}
	if got := len(project.Workloads()); got != workloads {
		t.Fatalf("loaded %d workloads, want %d", got, workloads)
	}
	return elapsed
}

// scalingProject spreads workloads jobs over Fragment files, each with applications that name
// workloads from every file.
func scalingProject(workloads int) fstest.MapFS {
	files := fstest.MapFS{}
	fragments := workloads / workloadsPerFile
	appsPerFile := workloadsPerFile * membershipsPerWorkload / membersPerApp
	var root strings.Builder
	root.WriteString(projectHeader + "spec:\n  imports:\n")
	for fragment := range fragments {
		name := fmt.Sprintf("fragments/part-%d.yaml", fragment)
		fmt.Fprintf(&root, "    - %s\n", name)
		var file strings.Builder
		file.WriteString("apiVersion: stackpuddle/v1\nkind: Fragment\nspec:\n  workloads:\n")
		for index := fragment * workloadsPerFile; index < (fragment+1)*workloadsPerFile; index++ {
			fmt.Fprintf(&file, "    w%d: {type: job, image: busybox}\n", index)
		}
		file.WriteString("  applications:\n")
		for app := fragment * appsPerFile; app < (fragment+1)*appsPerFile; app++ {
			members := make([]string, membersPerApp)
			for member := range members {
				members[member] = fmt.Sprintf("w%d", (app*membersPerApp+member)%workloads)
			}
			fmt.Fprintf(&file, "    a%d: {workloads: [%s]}\n", app, strings.Join(members, ", "))
		}
		files[name] = &fstest.MapFile{Data: []byte(file.String())}
	}
	root.WriteString(environment)
	files[rootFile] = &fstest.MapFile{Data: []byte(root.String())}
	return files
}

func TestLoadTimeGrowsLinearlyWithEnvironmentOverrides(t *testing.T) {
	fewer, more := overridesProject(fewerOverrides), overridesProject(moreOverrides)
	var fewerBest, moreBest time.Duration
	for round := range scalingRounds {
		fewerTime, fewerDiagnostics := timeOverrides(fewer)
		moreTime, moreDiagnostics := timeOverrides(more)
		if len(fewerDiagnostics) > 0 {
			t.Fatalf("the project below the override budget must load: %v", fewerDiagnostics[0])
		}
		if len(moreDiagnostics) != 1 || moreDiagnostics[0].Code() != model.SyntaxLimitExceeded {
			t.Fatalf("the project past the override budget must fail once with %s, got %v", model.SyntaxLimitExceeded, moreDiagnostics)
		}
		if at := moreDiagnostics[0].Location().Pointer(); !strings.HasPrefix(at, "/spec/environments/") || !strings.HasSuffix(at, "/overrides/modules/store/options") {
			t.Fatalf("the override budget is reported at %q, want an environment's override of module store", at)
		}
		if round == 0 || fewerTime < fewerBest {
			fewerBest = fewerTime
		}
		if round == 0 || moreTime < moreBest {
			moreBest = moreTime
		}
	}
	ratio := float64(moreBest) / float64(fewerBest)
	t.Logf("best of %d: %d options and overrides in %v, %d in %v, ratio %.2f", scalingRounds, fewerOverrides, fewerBest, moreOverrides, moreBest, ratio)
	if ratio >= linearRatioCeiling {
		t.Fatalf("4x the options and overrides took %.2fx the time, want below %.1fx", ratio, linearRatioCeiling)
	}
}

func timeOverrides(files fstest.MapFS) (time.Duration, model.Diagnostics) {
	start := time.Now()
	_, diagnostics := model.Load(files, rootFile, nil)
	return time.Since(start), diagnostics
}

// overridesProject declares one module instance with count options and count environments that
// each override one of them, against a local option schema every option satisfies.
func overridesProject(count int) fstest.MapFS {
	var root strings.Builder
	root.WriteString(projectHeader + "spec:\n  definitions: {wide: {path: wide}}\n  modules:\n    store: {definition: wide, options: {")
	for option := range count {
		fmt.Fprintf(&root, "o%d: 1, ", option)
	}
	root.WriteString("z: 1}}\n  environments:\n")
	for name := range count {
		fmt.Fprintf(&root, "    e%d: {runtime: {type: compose, context: default}, secrets: {provider: local}, "+
			"overrides: {modules: {store: {options: {z: 2}}}}}\n", name)
	}
	definition := "apiVersion: stackpuddle/v1\nkind: Module\nname: wide\nversion: 0.1.0\n" +
		"configuration:\n  schema: content/options.schema.json\nworkloads:\n  store: {type: service, image: busybox}\n"
	return fstest.MapFS{
		rootFile:           {Data: []byte(root.String())},
		"wide/module.yaml": {Data: []byte(definition)},
		"wide/content/options.schema.json": {Data: []byte(
			`{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "additionalProperties": {"type": "integer"}}`)},
	}
}

// TestLoadRefusesTheOptionChecksWorkBudgetQuickly times the two hostile option checks that the
// schema size limits alone admit: a chained schema and a wide anyOf over many options.
func TestLoadRefusesTheOptionChecksWorkBudgetQuickly(t *testing.T) {
	const ceiling = 5 * time.Second
	for name, files := range map[string]fstest.MapFS{
		"chained 30": chainedOptionSchema(30), "chained 40": chainedOptionSchema(40), "failing branches": failingBranches(),
	} {
		elapsed, diagnostics := timeOverrides(files)
		if len(diagnostics) != 1 || diagnostics[0].Code() != model.SyntaxLimitExceeded {
			t.Errorf("%s: got %v, want one %s", name, diagnostics, model.SyntaxLimitExceeded)
		}
		if elapsed > ceiling {
			t.Errorf("%s: took %v, want under %v", name, elapsed, ceiling)
		}
	}
}

const (
	fewerOptions = 10_000
	moreOptions  = 40_000
)

// TestEffectiveOptionsTimeGrowsLinearlyWithOptionKeys guards the source record: replacing a value
// must cost the size of what it replaces, not the size of every option recorded so far.
func TestEffectiveOptionsTimeGrowsLinearlyWithOptionKeys(t *testing.T) {
	fewer, more := effectiveOptionsTime(t, fewerOptions), effectiveOptionsTime(t, moreOptions)
	ratio := float64(more) / float64(fewer)
	t.Logf("best of %d: %d keys in %v, %d keys in %v, ratio %.2f", scalingRounds, fewerOptions, fewer, moreOptions, more, ratio)
	if ratio >= linearRatioCeiling {
		t.Fatalf("4x the option keys took %.2fx the time, want below %.1fx", ratio, linearRatioCeiling)
	}
}

func effectiveOptionsTime(t *testing.T, count int) time.Duration {
	t.Helper()
	files := overridesProject(1)
	var root strings.Builder
	root.WriteString(projectHeader + "spec:\n  definitions: {wide: {path: wide}}\n  modules:\n    store: {definition: wide, options: {")
	for option := range count {
		fmt.Fprintf(&root, "o%d: 1, ", option)
	}
	root.WriteString("z: 1}}\n" + environment)
	files[rootFile] = &fstest.MapFile{Data: []byte(root.String())}
	project, diagnostics := model.Load(files, rootFile, nil)
	if len(diagnostics) > 0 {
		t.Fatalf("the options project must load: %v", diagnostics[0])
	}
	key, err := model.ParseObjectKey("store")
	if err != nil {
		t.Fatal(err)
	}
	var best time.Duration
	for round := range scalingRounds {
		start := time.Now()
		if _, ok := project.EffectiveOptions(key); !ok {
			t.Fatal("the instance must resolve")
		}
		if elapsed := time.Since(start); round == 0 || elapsed < best {
			best = elapsed
		}
	}
	return best
}
