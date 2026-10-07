package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestInnerLayerDependencies защищает границы импортов domain и usecase.
func TestInnerLayerDependencies(t *testing.T) {
	root := filepath.Join("..", "..")
	services := []string{"api-gateway", "payment-core", "chain-worker", "webhook-sender", "crypto-vault"}
	const module = "github.com/renegadik/crypto-payment-gateway/"
	for _, service := range services {
		for _, layer := range []string{"domain", "usecase"} {
			dir := filepath.Join(root, service, "internal", layer)
			err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				for _, imp := range file.Imports {
					importPath, err := strconv.Unquote(imp.Path.Value)
					if err != nil {
						return err
					}
					first, _, _ := strings.Cut(importPath, "/")
					if !strings.Contains(first, ".") {
						continue
					} // standard library
					domain := module + service + "/internal/domain"
					if layer == "usecase" && (importPath == domain || strings.HasPrefix(importPath, domain+"/")) {
						continue
					}
					t.Errorf("%s imports forbidden dependency %s", path, importPath)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
