// Package cli holds helpers for building the hub/agent flagsets — notably
// registering a long flag together with a short alias that shares one target
// variable, so e.g. --config and -c both write the same value.
package cli

import "flag"

// String registers --name on fs and, when short != "", also -short; both write
// the same string variable. The shorthand's usage line points at the long name.
func String(fs *flag.FlagSet, name, short, value, usage string) *string {
	p := fs.String(name, value, usage)
	if short != "" {
		fs.StringVar(p, short, value, "shorthand for --"+name)
	}
	return p
}

// Bool mirrors String for boolean flags.
func Bool(fs *flag.FlagSet, name, short string, value bool, usage string) *bool {
	p := fs.Bool(name, value, usage)
	if short != "" {
		fs.BoolVar(p, short, value, "shorthand for --"+name)
	}
	return p
}

// ConfigFlag registers the standard --config / -c flag used by nearly every command.
func ConfigFlag(fs *flag.FlagSet) *string {
	return String(fs, "config", "c", "", "path to config file")
}
