package handler_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"sort"
	"strconv"
	"testing"
)

// The three clients each merge program week overrides themselves, and a key one
// of them does not read is dropped from the prescription the athlete plays
// rather than merely ignored there. contract/override-keys.json is the list they
// are all held to; this file is the backend end of it, and asserts the two
// things that can silently drop a key here: a json tag the contract does not
// name, and a field mergeItemOverride never reads.
const (
	overrideContractPath = "../../contract/override-keys.json"
	trainingItemsPath    = "../../internal/handler/training_items.go"
	overrideStructName   = "itemOverride"
	mergeFuncName        = "mergeItemOverride"
)

type overrideContract struct {
	Doc  string `json:"doc"`
	Keys []struct {
		Key       string `json:"key"`
		ItemField string `json:"item_field"`
	} `json:"keys"`
}

func readOverrideContract(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(overrideContractPath)
	if err != nil {
		t.Fatalf("Failed to read the override contract: %v", err)
	}
	var contract overrideContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatalf("Failed to parse the override contract: %v", err)
	}
	keys := make([]string, 0, len(contract.Keys))
	for _, key := range contract.Keys {
		keys = append(keys, key.Key)
	}
	return keys
}

// parseTrainingItems reads the handler source rather than the package, because
// itemOverride is unexported and the tests are an external package by
// convention. The struct tags are what the override is unmarshalled through, so
// reading them off the declaration is reading the real key set.
func parseTrainingItems(t *testing.T) (*ast.File, *token.FileSet) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, trainingItemsPath, nil, 0)
	if err != nil {
		t.Fatalf("Failed to parse %s: %v", trainingItemsPath, err)
	}
	return file, fset
}

// overrideStructFields returns the json key of every field of itemOverride,
// keyed by the Go field name so the merge check can name what it misses.
func overrideStructFields(t *testing.T, file *ast.File) map[string]string {
	t.Helper()
	fields := map[string]string{}
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.TypeSpec)
		if !ok || spec.Name.Name != overrideStructName {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			t.Fatalf("%s is not a struct", overrideStructName)
		}
		for _, field := range structType.Fields.List {
			if field.Tag == nil {
				t.Fatalf("%s has a field with no json tag", overrideStructName)
			}
			tag, err := strconv.Unquote(field.Tag.Value)
			if err != nil {
				t.Fatalf("Failed to read a struct tag: %v", err)
			}
			key := reflect.StructTag(tag).Get("json")
			if key == "" {
				t.Fatalf("%s has a field with an empty json tag", overrideStructName)
			}
			for _, name := range field.Names {
				fields[name.Name] = key
			}
		}
		return false
	})
	if len(fields) == 0 {
		t.Fatalf("Failed to find %s in %s", overrideStructName, trainingItemsPath)
	}
	return fields
}

// mergedFieldNames returns the itemOverride fields mergeItemOverride reads, by
// collecting every selector on the local it unmarshals the override into.
func mergedFieldNames(t *testing.T, file *ast.File) map[string]bool {
	t.Helper()
	var merge *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == mergeFuncName {
			merge = fn
			break
		}
	}
	if merge == nil {
		t.Fatalf("Failed to find %s in %s", mergeFuncName, trainingItemsPath)
	}
	read := map[string]bool{}
	ast.Inspect(merge.Body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if ident, ok := selector.X.(*ast.Ident); ok && ident.Name == "over" {
			read[selector.Sel.Name] = true
		}
		return true
	})
	return read
}

func sorted(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func TestOverrideCoversEveryContractKey(t *testing.T) {
	file, _ := parseTrainingItems(t)
	fields := overrideStructFields(t, file)

	structKeys := make([]string, 0, len(fields))
	for _, key := range fields {
		structKeys = append(structKeys, key)
	}

	contractKeys := readOverrideContract(t)
	if !reflect.DeepEqual(sorted(structKeys), sorted(contractKeys)) {
		t.Errorf("%s and %s name different keys.\nstruct:   %v\ncontract: %v\n"+
			"A key on one side only is dropped from the prescription by whichever client does not read it. "+
			"Update the contract, copy it to crimpy-app and crimpy-frontend, and make their tests pass.",
			overrideStructName, overrideContractPath, sorted(structKeys), sorted(contractKeys))
	}
}

func TestMergeItemOverrideReadsEveryOverrideField(t *testing.T) {
	file, _ := parseTrainingItems(t)
	fields := overrideStructFields(t, file)
	read := mergedFieldNames(t, file)

	for field, key := range fields {
		if !read[field] {
			t.Errorf("%s never reads %s.%s, so an override carrying %q is parsed and thrown away",
				mergeFuncName, overrideStructName, field, key)
		}
	}
}
