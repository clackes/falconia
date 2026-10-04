package main

import "flag"

// parseFlags reads options even when they come after the path.
//
// The standard flag package stops at the first argument that does not start
// with a dash, so `falconia deploy ./site --title Hello` would look like a
// command without options and silently publish with the defaults. That is the
// worst way to fail: no error, and an artifact with the wrong visibility.
// Agents in particular put flags wherever they like.
//
// Options are separated from positionals first, asking the FlagSet which
// options are booleans and therefore do not consume the next token.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, positional []string

	for i := 0; i < len(args); i++ {
		a := args[i]

		// Everything after "--" is positional by definition.
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			positional = append(positional, a)
			continue
		}

		flags = append(flags, a)
		// The --name=value form already carries its value.
		if containsEquals(a) {
			continue
		}
		if isBoolFlag(fs, trimDashes(a)) {
			continue
		}
		// The next token is this option's value, not a positional.
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}

	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return positional, nil
}

func isBoolFlag(fs *flag.FlagSet, name string) bool {
	f := fs.Lookup(name)
	if f == nil {
		// An unknown option is left to fs.Parse, which will give the right
		// error. When in doubt, do not consume the next token.
		return true
	}
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

func trimDashes(s string) string {
	for len(s) > 0 && s[0] == '-' {
		s = s[1:]
	}
	return s
}

func containsEquals(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return true
		}
	}
	return false
}
