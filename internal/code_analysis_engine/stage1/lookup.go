package stage1

// lookupSpec finds the LanguageSpec for a given SupportedLang within a registry.
func lookupSpec(lang SupportedLang, reg []LanguageSpec) *LanguageSpec {
	for i := range reg {
		if reg[i].Lang == lang {
			return &reg[i]
		}
	}
	return nil
}
