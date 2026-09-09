// Package federation implements Phase 6 Publishing & Federation pillars:
// - Pillar 21: Multi-Target Publishing & Portal Compatibility
// - Pillar 27: Multi-Version Documentation Matrix
// - Pillar 29: Cross-Repo & Microservice Doc Federation
// - Pillar 35: i18n / L10n Inventory
// - Pillar 36: Documentation Debt & ROI Analytics
package federation

import (
	"regexp"
	"strings"
)

var (
	ghNoteRe    = regexp.MustCompile(`(?m)^>\s*\[!NOTE\]\s*\n((?:>.*\n?)*)`)
	ghTipRe     = regexp.MustCompile(`(?m)^>\s*\[!TIP\]\s*\n((?:>.*\n?)*)`)
	ghWarnRe    = regexp.MustCompile(`(?m)^>\s*\[!WARNING\]\s*\n((?:>.*\n?)*)`)
	ghCautionRe = regexp.MustCompile(`(?m)^>\s*\[!CAUTION\]\s*\n((?:>.*\n?)*)`)
)

// FormatForPlatform converts Markdown callouts and admonitions to match the target publishing portal.
// Supported platforms: github_flat (default), vitepress, docusaurus, mkdocs.
func FormatForPlatform(content, targetPlatform string) string {
	switch strings.ToLower(targetPlatform) {
	case "vitepress":
		return transformToVitePress(content)
	case "docusaurus":
		return transformToDocusaurus(content)
	case "mkdocs":
		return transformToMkDocs(content)
	default:
		// github_flat is the native format
		return content
	}
}

func transformToVitePress(content string) string {
	res := ghNoteRe.ReplaceAllStringFunc(content, func(m string) string {
		body := cleanBlockquote(m, `> [!NOTE]`)
		return "::: info\n" + body + "\n:::\n"
	})
	res = ghTipRe.ReplaceAllStringFunc(res, func(m string) string {
		body := cleanBlockquote(m, `> [!TIP]`)
		return "::: tip\n" + body + "\n:::\n"
	})
	res = ghWarnRe.ReplaceAllStringFunc(res, func(m string) string {
		body := cleanBlockquote(m, `> [!WARNING]`)
		return "::: warning\n" + body + "\n:::\n"
	})
	res = ghCautionRe.ReplaceAllStringFunc(res, func(m string) string {
		body := cleanBlockquote(m, `> [!CAUTION]`)
		return "::: danger\n" + body + "\n:::\n"
	})
	return res
}

func transformToDocusaurus(content string) string {
	res := ghNoteRe.ReplaceAllStringFunc(content, func(m string) string {
		body := cleanBlockquote(m, `> [!NOTE]`)
		return ":::note\n" + body + "\n:::\n"
	})
	res = ghTipRe.ReplaceAllStringFunc(res, func(m string) string {
		body := cleanBlockquote(m, `> [!TIP]`)
		return ":::tip\n" + body + "\n:::\n"
	})
	res = ghWarnRe.ReplaceAllStringFunc(res, func(m string) string {
		body := cleanBlockquote(m, `> [!WARNING]`)
		return ":::caution\n" + body + "\n:::\n"
	})
	res = ghCautionRe.ReplaceAllStringFunc(res, func(m string) string {
		body := cleanBlockquote(m, `> [!CAUTION]`)
		return ":::danger\n" + body + "\n:::\n"
	})
	return res
}

func transformToMkDocs(content string) string {
	res := ghNoteRe.ReplaceAllStringFunc(content, func(m string) string {
		body := indentLines(cleanBlockquote(m, `> [!NOTE]`), "    ")
		return "!!! note\n" + body + "\n"
	})
	res = ghTipRe.ReplaceAllStringFunc(res, func(m string) string {
		body := indentLines(cleanBlockquote(m, `> [!TIP]`), "    ")
		return "!!! tip\n" + body + "\n"
	})
	res = ghWarnRe.ReplaceAllStringFunc(res, func(m string) string {
		body := indentLines(cleanBlockquote(m, `> [!WARNING]`), "    ")
		return "!!! warning\n" + body + "\n"
	})
	res = ghCautionRe.ReplaceAllStringFunc(res, func(m string) string {
		body := indentLines(cleanBlockquote(m, `> [!CAUTION]`), "    ")
		return "!!! danger\n" + body + "\n"
	})
	return res
}

func cleanBlockquote(block, marker string) string {
	lines := strings.Split(block, "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == marker || l == "" {
			continue
		}
		l = strings.TrimPrefix(l, ">")
		out = append(out, strings.TrimSpace(l))
	}
	return strings.Join(out, "\n")
}

func indentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
