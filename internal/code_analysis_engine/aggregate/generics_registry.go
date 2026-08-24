package aggregate

import (
	"sort"
	"strings"
)

// IndexGenerics scans the global index for template/generic signatures
// and maps the base symbol name to its instantiated or template signature.
// This allows the linker to resolve calls like Repository<User>.Find() to Repository<T>.Find()
func IndexGenerics(output *AggregateOutput) {
	// Rebuild from scratch from current GlobalDefinitionIndex (C2-10): purges
	// orphans from deleted/modified files; sorted keys for determinism.
	output.GenericsRegistry = make(map[string]string)

	keys := make([]string, 0, len(output.GlobalDefinitionIndex))
	for k := range output.GlobalDefinitionIndex {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, fqn := range keys {
		// Detect <T> syntax (Java, C++, TS, C#)
		if idx := strings.Index(fqn, "<"); idx != -1 && strings.HasSuffix(fqn, ">") {
			baseName := fqn[:idx]
			output.GenericsRegistry[baseName] = fqn
		}

		// Detect [T] syntax (Go 1.18+)
		if idx := strings.Index(fqn, "["); idx != -1 && strings.HasSuffix(fqn, "]") {
			baseName := fqn[:idx]
			output.GenericsRegistry[baseName] = fqn
		}
	}
}
