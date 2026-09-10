package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateConfigDictionary(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "cfg")
	_ = os.MkdirAll(pkgDir, 0755)

	code := `package cfg
import "os"

func Init() {
	_ = os.Getenv("PORT")
	_ = os.Getenv("DATABASE_URL")
}
`
	_ = os.WriteFile(filepath.Join(pkgDir, "env.go"), []byte(code), 0644)

	dict, err := GenerateConfigDictionary(tempDir)
	if err != nil {
		t.Fatalf("GenerateConfigDictionary failed: %v", err)
	}

	if !strings.Contains(dict, "DATABASE_URL") {
		t.Errorf("missing DATABASE_URL in config dictionary:\n%s", dict)
	}
	if !strings.Contains(dict, "PORT") {
		t.Errorf("missing PORT in config dictionary:\n%s", dict)
	}

	dest := filepath.Join(tempDir, "docs", "configuration.md")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Errorf("docs/configuration.md not created")
	}
}

func TestGenerateConfigDictionaryExtendedSources(t *testing.T) {
	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "cfg")
	_ = os.MkdirAll(pkgDir, 0755)

	code := `package cfg

import (
	"flag"
	"os"

	"github.com/spf13/viper"
)

var port = flag.String("port", "8080", "server port")
var _ = flag.Bool("debug", false, "debug mode")

type ServerConfig struct {
	// Host is the bind address.
	Host string ` + "`mapstructure:\"server_host\" yaml:\"host\" default:\"localhost\"`" + `
	// Token authenticates upstream.
	Token string ` + "`mapstructure:\"server_token\" required:\"true\"`" + `
	Ignored string
}

func Init() {
	_ = os.Getenv("PORT")
	_ = viper.GetString("SERVER_NAME")
	viper.SetDefault("SERVER_NAME", "glassmarble")
	_ = viper.GetInt("MAX_WORKERS")
	_ = *port
}
`
	_ = os.WriteFile(filepath.Join(pkgDir, "env.go"), []byte(code), 0644)

	vars, err := scanConfigVariables(tempDir)
	if err != nil {
		t.Fatalf("scanConfigVariables failed: %v", err)
	}
	byName := map[string]ConfigVarRecord{}
	for _, v := range vars {
		byName[v.Name] = v
	}

	// viper capture with SetDefault default applied.
	vn, ok := byName["SERVER_NAME"]
	if !ok {
		t.Fatalf("missing viper key SERVER_NAME in %v", byName)
	}
	if vn.Type != "string" {
		t.Errorf("SERVER_NAME type = %q, want string", vn.Type)
	}
	if vn.Default != "glassmarble" {
		t.Errorf("SERVER_NAME default = %q, want glassmarble", vn.Default)
	}
	if vn.Required {
		t.Errorf("SERVER_NAME with default must not be required")
	}
	// viper without default is required by heuristic.
	mw, ok := byName["MAX_WORKERS"]
	if !ok {
		t.Fatalf("missing viper key MAX_WORKERS")
	}
	if mw.Type != "int" {
		t.Errorf("MAX_WORKERS type = %q, want int", mw.Type)
	}
	if !mw.Required {
		t.Errorf("MAX_WORKERS without default should be required by heuristic")
	}

	// flag capture with real type + default.
	pf, ok := byName["port"]
	if !ok {
		t.Fatalf("missing flag port")
	}
	if pf.Type != "string" || pf.Default != "8080" {
		t.Errorf("flag port = type %q default %q, want string/8080", pf.Type, pf.Default)
	}
	db, ok := byName["debug"]
	if !ok {
		t.Fatalf("missing flag debug")
	}
	if db.Type != "bool" || db.Default != "false" {
		t.Errorf("flag debug = type %q default %q, want bool/false", db.Type, db.Default)
	}

	// struct-tag capture.
	sh, ok := byName["server_host"]
	if !ok {
		t.Fatalf("missing struct tag server_host")
	}
	if sh.Type != "string" || sh.Default != "localhost" {
		t.Errorf("server_host = type %q default %q, want string/localhost", sh.Type, sh.Default)
	}
	st, ok := byName["server_token"]
	if !ok {
		t.Fatalf("missing struct tag server_token")
	}
	if !st.Required {
		t.Errorf("server_token with required:true tag should be required")
	}

	// Table renders real Type/Default columns.
	dict, err := GenerateConfigDictionary(tempDir)
	if err != nil {
		t.Fatalf("GenerateConfigDictionary failed: %v", err)
	}
	if !strings.Contains(dict, "SERVER_NAME") || !strings.Contains(dict, "glassmarble") {
		t.Errorf("expected viper default in table:\n%s", dict)
	}
}
