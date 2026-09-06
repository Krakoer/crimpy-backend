package handler_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The three clients each merge program week overrides themselves, and a key one
// of them does not read is dropped from the prescription the athlete plays
// rather than merely ignored there. contract/override-keys.json is the list they
// are all held to; this file is the backend end of it, and asserts the three
// things that can silently drop a key here: a json tag the contract does not
// name, a field mergeItemOverride never writes, and a field it writes onto the
// wrong item column.
const (
	overrideContractPath = "../../contract/override-keys.json"
	handlerDir           = "../../internal/handler"
	overrideStructName   = "itemOverride"
	overridableFuncName  = "overridable"
	mergeFuncName        = "mergeItemOverride"
)

type contractKey struct {
	Key       string `json:"key"`
	ItemField string `json:"item_field"`
	Sample    any    `json:"sample"`
}

func readOverrideContract(t *testing.T) []contractKey {
	t.Helper()
	raw, err := os.ReadFile(overrideContractPath)
	if err != nil {
		t.Fatalf("Failed to read the override contract: %v", err)
	}
	var contract struct {
		Keys []contractKey `json:"keys"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatalf("Failed to parse the override contract: %v", err)
	}
	if len(contract.Keys) == 0 {
		t.Fatalf("The override contract names no key")
	}
	return contract.Keys
}

// parseHandlerPackage reads the handler sources rather than the package, because
// itemOverride is unexported and the tests are an external package by
// convention. The whole directory is parsed so the declarations can move between
// files without this test having to follow them.
func parseHandlerPackage(t *testing.T) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir(handlerDir)
	if err != nil {
		t.Fatalf("Failed to read %s: %v", handlerDir, err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(handlerDir, name), nil, 0)
		if err != nil {
			t.Fatalf("Failed to parse %s: %v", name, err)
		}
		files = append(files, file)
	}
	return files
}

func findStruct(t *testing.T, files []*ast.File, name string) *ast.StructType {
	t.Helper()
	var found *ast.StructType
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok || spec.Name.Name != name {
				return true
			}
			structType, ok := spec.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("%s is not a struct", name)
			}
			found = structType
			return false
		})
	}
	if found == nil {
		t.Fatalf("Failed to find %s under %s", name, handlerDir)
	}
	return found
}

// jsonKeys returns the wire key of every field of the struct, keyed by the Go
// field name. The tag options are dropped, so a field that gains an omitempty
// still reads as the key it always was.
func jsonKeys(t *testing.T, name string, structType *ast.StructType) map[string]string {
	t.Helper()
	keys := map[string]string{}
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			// encoding/json promotes an embedded struct's keys, so one here
			// would carry keys this test cannot see and the closed set would
			// stop being closed.
			t.Fatalf("%s embeds a struct, whose keys this test cannot name", name)
		}
		if field.Tag == nil {
			continue
		}
		tag, err := strconv.Unquote(field.Tag.Value)
		if err != nil {
			t.Fatalf("Failed to read a struct tag on %s: %v", name, err)
		}
		key, _, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
		if key == "" {
			continue
		}
		for _, fieldName := range field.Names {
			keys[fieldName.Name] = key
		}
	}
	return keys
}

// receiverName returns the type a method hangs off, pointer or not.
func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// overridableShapes names every item shape exposing a view the override is
// merged through. Read out of the package rather than listed here, since a shape
// added later merges the same override and has to land every key on the same
// column, and a list in this file would be the drift it exists to stop.
func overridableShapes(t *testing.T, files []*ast.File) []string {
	t.Helper()
	var shapes []string
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != overridableFuncName {
				continue
			}
			if name := receiverName(fn); name != "" {
				shapes = append(shapes, name)
			}
		}
	}
	if len(shapes) == 0 {
		t.Fatalf("Failed to find any %s method under %s", overridableFuncName, handlerDir)
	}
	slices.Sort(shapes)
	return shapes
}

func findMethod(t *testing.T, files []*ast.File, receiver, name string) *ast.FuncDecl {
	t.Helper()
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != name {
				continue
			}
			if receiverName(fn) == receiver {
				return fn
			}
		}
	}
	t.Fatalf("Failed to find %s.%s under %s", receiver, name, handlerDir)
	return nil
}

func findFunc(t *testing.T, files []*ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, file := range files {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name && fn.Recv == nil {
				return fn
			}
		}
	}
	t.Fatalf("Failed to find %s under %s", name, handlerDir)
	return nil
}

// overridableFields reads which field of the item each field of overridableItem
// points at, out of the overridable method of the given shape.
func overridableFields(t *testing.T, files []*ast.File, receiver string) map[string]string {
	t.Helper()
	fields := map[string]string{}
	ast.Inspect(findMethod(t, files, receiver, overridableFuncName).Body, func(node ast.Node) bool {
		pair, ok := node.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok {
			return true
		}
		unary, ok := pair.Value.(*ast.UnaryExpr)
		if !ok {
			return true
		}
		if selector, ok := unary.X.(*ast.SelectorExpr); ok {
			fields[key.Name] = selector.Sel.Name
		}
		return true
	})
	if len(fields) == 0 {
		t.Fatalf("Failed to read the item fields out of (*%s).%s", receiver, overridableFuncName)
	}
	return fields
}

// mergePairs reads which field of the item each override field is written onto,
// out of the tables mergeItemOverride merges through. Reading the pair rather
// than only the override field is what catches a table entry that carries the
// right key onto the wrong column.
func mergePairs(t *testing.T, files []*ast.File) map[string]string {
	t.Helper()
	pairs := map[string]string{}
	ast.Inspect(findFunc(t, files, mergeFuncName).Body, func(node ast.Node) bool {
		lit, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		var override, target string
		for _, elt := range lit.Elts {
			selector, ok := elt.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			ident, ok := selector.X.(*ast.Ident)
			if !ok {
				continue
			}
			switch ident.Name {
			case "over":
				override = selector.Sel.Name
			case "target":
				target = selector.Sel.Name
			}
		}
		if override != "" && target != "" {
			pairs[override] = target
		}
		return true
	})
	if len(pairs) == 0 {
		t.Fatalf("Failed to read the merge tables out of %s", mergeFuncName)
	}
	return pairs
}

func TestOverrideCoversEveryContractKey(t *testing.T) {
	files := parseHandlerPackage(t)
	keys := jsonKeys(t, overrideStructName, findStruct(t, files, overrideStructName))

	structKeys := slices.Sorted(maps.Values(keys))

	var contractKeys []string
	for _, entry := range readOverrideContract(t) {
		contractKeys = append(contractKeys, entry.Key)
	}
	slices.Sort(contractKeys)

	if !slices.Equal(structKeys, contractKeys) {
		t.Errorf("%s and %s name different keys.\nstruct:   %v\ncontract: %v\n"+
			"A key on one side only is dropped from the prescription by whichever client does not read it. "+
			"Update the contract, copy it to crimpy-app and crimpy-frontend, and make their tests pass.",
			overrideStructName, overrideContractPath, structKeys, contractKeys)
	}
}

func TestMergeItemOverrideWritesEveryKeyToItsItemField(t *testing.T) {
	files := parseHandlerPackage(t)
	overrideKeys := jsonKeys(t, overrideStructName, findStruct(t, files, overrideStructName))
	pairs := mergePairs(t, files)

	shapes := overridableShapes(t, files)
	itemKeys := map[string]map[string]string{}
	overridable := map[string]map[string]string{}
	for _, receiver := range shapes {
		itemKeys[receiver] = jsonKeys(t, receiver, findStruct(t, files, receiver))
		overridable[receiver] = overridableFields(t, files, receiver)
	}

	byKey := map[string]string{}
	for field, key := range overrideKeys {
		byKey[key] = field
	}

	for _, entry := range readOverrideContract(t) {
		field, ok := byKey[entry.Key]
		if !ok {
			// TestOverrideCoversEveryContractKey names this one.
			continue
		}
		target, merged := pairs[field]
		if !merged {
			t.Errorf("%s never writes %s.%s, so an override carrying %q is parsed and thrown away",
				mergeFuncName, overrideStructName, field, entry.Key)
			continue
		}
		for _, receiver := range shapes {
			itemField, ok := overridable[receiver][target]
			if !ok {
				t.Errorf("(*%s).%s points nothing at %s, which %s writes %q onto",
					receiver, overridableFuncName, target, mergeFuncName, entry.Key)
				continue
			}
			if key := itemKeys[receiver][itemField]; key != entry.ItemField {
				t.Errorf("%s writes %q onto %s.%s, which is %q, not the %q the contract names. "+
					"An override key merged onto the wrong column is dropped from the prescription "+
					"and quietly rewrites another field.",
					mergeFuncName, entry.Key, receiver, itemField, key, entry.ItemField)
			}
		}
	}
}

// The samples are what the app and the portal feed their own merge, so a sample
// the backend could not decode would leave both suites testing a value this API
// refuses.
func TestOverrideSamplesMatchTheFieldTypes(t *testing.T) {
	files := parseHandlerPackage(t)
	structType := findStruct(t, files, overrideStructName)
	keys := jsonKeys(t, overrideStructName, structType)

	types := map[string]string{}
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			types[keys[name.Name]] = typeName(field.Type)
		}
	}

	for _, entry := range readOverrideContract(t) {
		fieldType, named := types[entry.Key]
		if !named {
			// TestOverrideCoversEveryContractKey names this one.
			continue
		}
		var ok bool
		switch fieldType {
		case "*int32":
			_, ok = entry.Sample.(float64)
		case "*bool":
			_, ok = entry.Sample.(bool)
		case "*string":
			_, ok = entry.Sample.(string)
		case "json.RawMessage":
			// Free-form on the wire, validated against the item it targets.
			ok = entry.Sample != nil
		default:
			t.Fatalf("%s.%s has type %q, which this test does not know how to check a sample against",
				overrideStructName, entry.Key, fieldType)
		}
		if !ok {
			t.Errorf("the %q sample in %s is %#v, which %s.%s (%s) cannot decode",
				entry.Key, overrideContractPath, entry.Sample, overrideStructName, entry.Key, fieldType)
		}
	}
}

func typeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return "*" + typeName(typed.X)
	case *ast.SelectorExpr:
		return typeName(typed.X) + "." + typed.Sel.Name
	}
	return ""
}
