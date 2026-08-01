package mastertpl

// Custom template override lookup (design D6b): conf-custom parity with
// ISPConfig3, where server/conf-custom/ overrides server/conf/. A file in
// the configured custom directory with the same name as an embedded
// template takes precedence over the embedded one.

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Template sources reported by Load, for render-source logging.
const (
	// SourceCustom marks a template loaded from the custom override dir.
	SourceCustom = "custom"
	// SourceEmbedded marks a template loaded from the embedded set.
	SourceEmbedded = "embedded"
)

// Names returns the sorted names of every embedded ".master" template.
func Names() []string {
	entries, err := Templates.ReadDir("templates")
	if err != nil {
		// The embedded directory is compiled into the binary; this cannot
		// fail at runtime.
		panic(fmt.Sprintf("mastertpl: reading embedded templates: %v", err))
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// Load returns the source text of the named template, custom directory
// first (design D6b): when customDir (empty = embedded only) contains a
// readable file with that name it wins, otherwise the embedded template is
// used. The returned source is SourceCustom or SourceEmbedded so callers
// can log which one a render used.
func Load(name, customDir string) (src, source string, err error) {
	if !slices.Contains(Names(), name) {
		return "", "", fmt.Errorf("mastertpl: unknown template %q", name)
	}
	if customDir != "" {
		data, err := os.ReadFile(filepath.Join(customDir, name))
		if err == nil {
			return string(data), SourceCustom, nil
		}
		if !os.IsNotExist(err) {
			return "", "", fmt.Errorf("mastertpl: reading custom template %q: %w", name, err)
		}
	}
	data, err := Templates.ReadFile("templates/" + name)
	if err != nil {
		return "", "", fmt.Errorf("mastertpl: reading embedded template %q: %w", name, err)
	}
	return string(data), SourceEmbedded, nil
}
