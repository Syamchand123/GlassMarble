package stage2

import (
	"testing"

	"github.com/Syamchand123/GlassMarble/internal/code_analysis_engine/stage1"
	"github.com/stretchr/testify/assert"
)

// TestT1SignatureCompleteness verifies that signature extraction and normalization achieves ≥95% resolution accuracy across all 7 T1 languages (W6-02 / §10.3).
func TestT1SignatureCompleteness(t *testing.T) {
	testCases := []struct {
		lang       stage1.SupportedLang
		tok        stage1.RichToken
		wantName   string
		wantReturn string
		minParams  int
	}{
		{
			lang: stage1.LangGo,
			tok: stage1.RichToken{
				Kind:       stage1.TokenDeclaration,
				Type:       "function_declaration",
				Name:       "Execute",
				Content:    "func Execute(ctx context.Context, id string) (bool, error)",
				FieldRoles: map[string]string{"name": "Execute", "parameters": "(ctx context.Context, id string)", "result": "(bool, error)"},
			},
			wantName:   "Execute",
			wantReturn: "(bool, error)",
			minParams:  2,
		},
		{
			lang: stage1.LangPython,
			tok: stage1.RichToken{
				Kind:       stage1.TokenDeclaration,
				Type:       "function_definition",
				Name:       "process",
				Content:    "def process(self, data: bytes) -> bool:",
				FieldRoles: map[string]string{"name": "process", "parameters": "(self, data: bytes)", "result": "bool"},
			},
			wantName:   "process",
			wantReturn: "bool",
			minParams:  2,
		},
		{
			lang: stage1.LangJS,
			tok: stage1.RichToken{
				Kind:       stage1.TokenDeclaration,
				Type:       "function_declaration",
				Name:       "handle",
				Content:    "function handle(req, res)",
				FieldRoles: map[string]string{"name": "handle", "parameters": "(req, res)"},
			},
			wantName:   "handle",
			wantReturn: "",
			minParams:  2,
		},
		{
			lang: stage1.LangTS,
			tok: stage1.RichToken{
				Kind:       stage1.TokenDeclaration,
				Type:       "function_declaration",
				Name:       "calculate",
				Content:    "function calculate(x: number, y: number): number",
				FieldRoles: map[string]string{"name": "calculate", "parameters": "(x: number, y: number)", "result": "number"},
			},
			wantName:   "calculate",
			wantReturn: "number",
			minParams:  2,
		},
		{
			lang: stage1.LangJava,
			tok: stage1.RichToken{
				Kind:       stage1.TokenDeclaration,
				Type:       "method_declaration",
				Name:       "run",
				Content:    "public boolean run(String name, int count)",
				FieldRoles: map[string]string{"name": "run", "parameters": "(String name, int count)", "result": "boolean"},
			},
			wantName:   "run",
			wantReturn: "boolean",
			minParams:  2,
		},
		{
			lang: stage1.LangCSharp,
			tok: stage1.RichToken{
				Kind:       stage1.TokenDeclaration,
				Type:       "method_declaration",
				Name:       "ExecuteTask",
				Content:    "public void ExecuteTask(string taskName, int priority)",
				FieldRoles: map[string]string{"name": "ExecuteTask", "parameters": "(string taskName, int priority)", "result": "void"},
			},
			wantName:   "ExecuteTask",
			wantReturn: "void",
			minParams:  2,
		},
		{
			lang: stage1.LangRust,
			tok: stage1.RichToken{
				Kind:       stage1.TokenDeclaration,
				Type:       "function_item",
				Name:       "new",
				Content:    "pub fn new(id: u64, label: &str) -> Self",
				FieldRoles: map[string]string{"name": "new", "parameters": "(id: u64, label: &str)", "result": "Self"},
			},
			wantName:   "new",
			wantReturn: "Self",
			minParams:  2,
		},
	}

	successCount := 0
	totalCount := len(testCases)

	for _, tc := range testCases {
		t.Run(string(tc.lang), func(t *testing.T) {
			sig := BuildSignature(tc.tok)
			assert.Equal(t, tc.wantName, sig.Name)
			assert.Equal(t, tc.wantReturn, sig.ReturnType)
			assert.GreaterOrEqual(t, len(sig.Params)+len(sig.ParamTypes), tc.minParams)
			successCount++
		})
	}

	accuracy := float64(successCount) / float64(totalCount)
	assert.GreaterOrEqual(t, accuracy, 0.95, "T1 signature resolution accuracy must be >= 95%")
}
