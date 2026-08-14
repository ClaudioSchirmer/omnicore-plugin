// Command omnicore-gen generates a complete omnicore entity from a spec.
//
// It is invoked by path from the installed plugin — `go run
// <plugin>/gen/cmd/omnicore-gen` — so it carries no version of its own: the
// plugin's version IS its version. The only external requirement is a Go
// toolchain new enough for this module's `go` directive.
//
// Because `go run` collapses every exit code to 1, the JSON emitted by `check`
// is the authoritative status, never the process exit code.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/cli"
	"github.com/ClaudioSchirmer/omnicore-plugin/gen/internal/compat"
)

const usage = `omnicore-gen — generate an omnicore entity from a spec

Usage:
  omnicore-gen <command> [flags]

Commands:
  init <Entity>     write a commented spec template, pre-filled from the project
  check             validate a spec without writing anything (JSON is the status contract)
  generate          generate the entity
  adopt <path>      accept a hand fix on a generated file, so regeneration keeps it
  doctor            report drift between the spec, the lock file and what is on disk
  explain [topic]   document the spec language, offline

Common flags:
  -spec <path>      the spec file (default: the only *.omnicore.yaml under ./specs)
  -project <dir>    the service root (default: the current directory)
  -json             machine-readable output
  -lang-fallback    emit marked placeholders for missing translations instead of refusing
  -force-unsupported  generate against a framework older than this build targets
  -migrations yes|no  write DDL. The generator writes migrations for a CREATE only;
                      evolving an existing schema is not something it does, and the
                      report says so with the shape the model now needs.

This generator targets framework ` + compat.Supported + `.x
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "check":
		runCheck(args)
	case "explain":
		runExplain(args)
	case "version":
		fmt.Printf("omnicore-gen (ships with the omnicore plugin) — targets framework %s.x\n", compat.Supported)
	case "help", "-h", "--help":
		fmt.Print(usage)
	case "generate":
		runGenerate(args)
	case "adopt":
		runAdopt(args)
	case "doctor":
		runDoctor(args)
	case "init":
		runInit(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func runCheck(args []string) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	specPath := fs.String("spec", "", "path to the spec file")
	project := fs.String("project", ".", "the service root")
	asJSON := fs.Bool("json", false, "machine-readable output")
	langFallback := fs.Bool("lang-fallback", false, "allow missing translations, marked")
	forceUnsupported := fs.Bool("force-unsupported", false, "generate against an older framework")
	if err := fs.Parse(args); err != nil {
		// Even a flag error answers in JSON when JSON was requested: a caller
		// reading canGenerate must never receive empty output.
		cli.Fatal("flags", err.Error(), hasJSONFlag(args))
		os.Exit(2)
	}

	resolved, err := resolveSpecPath(*specPath, *project)
	if err != nil {
		cli.Fatal("spec", err.Error(), *asJSON)
		os.Exit(1)
	}

	res, err := cli.Check(os.Stdout, cli.CheckOptions{
		SpecPath:         resolved,
		ProjectDir:       *project,
		LangFallback:     *langFallback,
		ForceUnsupported: *forceUnsupported,
		JSON:             *asJSON,
	})
	if err != nil {
		cli.Fatal("check", err.Error(), *asJSON)
		os.Exit(1)
	}
	if !res.CanGenerate {
		os.Exit(1)
	}
}

// hasJSONFlag re-reads the raw arguments because flag parsing itself may have
// failed before the flag was bound.
func hasJSONFlag(args []string) bool {
	for _, a := range args {
		if a == "-json" || a == "--json" || strings.HasPrefix(a, "-json=") {
			return true
		}
	}
	return false
}

func resolveSpecPath(given, project string) (string, error) {
	if given != "" {
		if _, err := os.Stat(given); err != nil {
			return "", fmt.Errorf("no spec file at %s", given)
		}
		return given, nil
	}
	dir := project + "/specs"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("no -spec given and no specs/ directory at %s — "+
			"pass -spec <path>, or put the spec in specs/<entity>.omnicore.yaml", project)
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".omnicore.yaml") {
			found = append(found, dir+"/"+e.Name())
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no *.omnicore.yaml found in %s", dir)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("%s holds %d specs — name the one you mean with -spec",
			dir, len(found))
	}
}

func runGenerate(args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	specPath := fs.String("spec", "", "path to the spec file")
	project := fs.String("project", ".", "the service root")
	langFallback := fs.Bool("lang-fallback", false, "allow missing translations, marked")
	forceUnsupported := fs.Bool("force-unsupported", false, "generate against an older framework")
	dryRun := fs.Bool("dry-run", false, "show what would happen, write nothing")
	force := fs.String("force", "", "comma-separated paths to overwrite even if hand-edited")
	migrations := fs.String("migrations", "",
		"write DDL: yes | no. Default writes it only for a CREATE — an entity this "+
			"generator has not produced before")
	_ = fs.Parse(args)

	resolved, err := resolveSpecPath(*specPath, *project)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	forced := map[string]bool{}
	for _, p := range strings.Split(*force, ",") {
		if p = strings.TrimSpace(p); p != "" {
			forced[p] = true
		}
	}

	if err := cli.Generate(os.Stdout, cli.GenerateOptions{
		SpecPath:         resolved,
		ProjectDir:       *project,
		LangFallback:     *langFallback,
		ForceUnsupported: *forceUnsupported,
		DryRun:           *dryRun,
		Force:            forced,
		Migrations:       *migrations,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runInit(args []string) {
	positional, flags := splitPositional(args)
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	project := fs.String("project", ".", "the service root")
	out := fs.String("out", "", "where to write (default: specs/<entity>.omnicore.yaml)")
	force := fs.Bool("force", false, "overwrite an existing spec")
	_ = fs.Parse(flags)
	if len(positional) < 1 {
		fmt.Fprintln(os.Stderr, "init needs the entity name, e.g. omnicore-gen init Student")
		os.Exit(2)
	}
	fsArg0 := positional[0]
	if err := cli.Init(os.Stdout, cli.InitOptions{
		Entity: fsArg0, ProjectDir: *project, Out: *out, Force: *force,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runAdopt(args []string) {
	positional, flags := splitPositional(args)
	fs := flag.NewFlagSet("adopt", flag.ExitOnError)
	project := fs.String("project", ".", "the service root")
	why := fs.String("why", "", "one line on what the spec could not express — recorded, and shown by doctor")
	_ = fs.Parse(flags)
	if len(positional) < 1 {
		fmt.Fprintln(os.Stderr, "adopt needs the path of the generated file to accept")
		os.Exit(2)
	}
	if err := cli.Adopt(os.Stdout, *project, positional[0], *why); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runDoctor(args []string) {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	project := fs.String("project", ".", "the service root")
	_ = fs.Parse(args)
	if err := cli.Doctor(os.Stdout, *project); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// splitPositional separates the bare arguments from the flags.
//
// Go's flag package stops parsing at the first non-flag token, so
// `init Student -project X` silently drops -project and writes to the wrong
// place. Separating first makes both orders work, which is the only behaviour
// anyone expects.
func splitPositional(args []string) (positional, flags []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			// A flag written as two tokens takes the next one with it.
			if !strings.Contains(a, "=") && i+1 < len(args) &&
				!strings.HasPrefix(args[i+1], "-") && takesValue(a) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positional = append(positional, a)
	}
	return positional, flags
}

// takesValue reports whether a flag consumes the token after it. Booleans do
// not, and treating one as if it did would swallow the entity name.
func takesValue(flag string) bool {
	switch strings.TrimLeft(flag, "-") {
	case "project", "out", "spec", "force-paths", "migrations":
		return true
	}
	return false
}

func runExplain(args []string) {
	topic := ""
	if len(args) > 0 {
		topic = args[0]
	}
	fmt.Print(cli.Explain(topic))
}
