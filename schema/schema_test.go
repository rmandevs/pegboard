package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateYAML(t *testing.T) {
	for _, dir := range []string{"valid", "invalid"} {
		files, err := filepath.Glob(filepath.Join("testdata", dir, "*.yaml"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no cases in testdata/%s", dir)
		}
		for _, f := range files {
			t.Run(dir+"/"+filepath.Base(f), func(t *testing.T) {
				src, err := os.ReadFile(f)
				if err != nil {
					t.Fatal(err)
				}
				err = ValidateYAML(src)
				if dir == "valid" && err != nil {
					t.Errorf("it should be valid: %v", err)
				}
				if dir == "invalid" && err == nil {
					t.Error("it should be invalid")
				}
			})
		}
	}
}

func TestValidateYAMLSyntaxError(t *testing.T) {
	if err := ValidateYAML([]byte("version: [1")); err == nil {
		t.Error("broken YAML should fail")
	}
}
