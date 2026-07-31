package stage3

import (
	"strings"
)

// IndexGenerics scans the global index for template/generic signatures
// and maps the base symbol name to its instantiated or template signature.
// This allows Stage 4 to resolve calls like Repository<User>.Find() to Repository<T>.Find()
func IndexGenerics(output *Stage3Output) {
	if output.GenericsRegistry == nil {
		output.GenericsRegistry = make(map[string]string)
	}

	for fqn := range output.GlobalDefinitionIndex {
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
