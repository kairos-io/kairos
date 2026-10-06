// Command test-artifacts generates the keys and system extension images
// that CI and the end-to-end tests need, so none of them has to be
// committed. It is a thin wrapper over pkg/testartifacts.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/kairos-io/kairos/v4/pkg/testartifacts"
)

const usage = `usage:
  test-artifacts keys         --out DIR
  test-artifacts sysext       --out DIR --name NAME [--arch amd64|arm64] [--key FILE --cert FILE]
  test-artifacts sysext-plain --out DIR --name NAME`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// absPaths converts non-empty relative paths to absolute paths.
func absPaths(paths ...*string) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	for _, p := range paths {
		if p == nil || *p == "" {
			continue
		}

		if !filepath.IsAbs(*p) {
			*p = filepath.Join(wd, *p)
		}
	}

	return nil
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "output directory")
	name := fs.String("name", "", "extension name")
	arch := fs.String("arch", "amd64", "extension architecture")
	key := fs.String("key", "", "signing key for the verity root hash")
	cert := fs.String("cert", "", "certificate for the signing key")

	switch args[0] {
	case "keys", "sysext", "sysext-plain":
	default:
		return fmt.Errorf("unknown subcommand %q\n%s", args[0], usage)
	}
	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w\n%s", err, usage)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q\n%s", fs.Arg(0), usage)
	}
	if *out == "" {
		return fmt.Errorf("--out is required\n%s", usage)
	}

	// Convert paths to absolute; they are bind-mounted into containers,
	// which require absolute paths.
	if err := absPaths(out, key, cert); err != nil {
		return err
	}

	switch args[0] {
	case "keys":
		return testartifacts.GenerateKeySet(ctx, *out)
	case "sysext":
		if *name == "" {
			return fmt.Errorf("--name is required\n%s", usage)
		}
		if *arch != "amd64" && *arch != "arm64" {
			return fmt.Errorf("--arch must be amd64 or arm64, got %q", *arch)
		}
		_, err := testartifacts.BuildSysext(ctx, testartifacts.SysextOptions{
			Dir: *out, Name: *name, Arch: *arch, KeyFile: *key, CertFile: *cert,
		})
		return err
	default:
		if *name == "" {
			return fmt.Errorf("--name is required\n%s", usage)
		}
		_, err := testartifacts.BuildPlainSquashfsSysext(ctx, *out, *name)
		return err
	}
}
