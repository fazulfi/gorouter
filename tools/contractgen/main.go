package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	openapiPath   = "api/admin-v1.openapi.yaml"
	outputPath    = "frontend/src/generated/admin-v1.ts"
	headerComment = "// Generated Admin API v1 client\n// Source: api/admin-v1.openapi.yaml\n// Do not edit manually; re-run the contract generator.\n"
)

type openapiDoc struct {
	OpenAPI string `yaml:"openapi"`
	Info    struct {
		Title       string `yaml:"title"`
		Version     string `yaml:"version"`
		Description string `yaml:"description"`
	} `yaml:"info"`
	Servers []struct {
		URL         string `yaml:"url"`
		Description string `yaml:"description"`
	} `yaml:"servers"`
	Components struct {
		Schemas map[string]schemaDef `yaml:"schemas"`
	} `yaml:"components"`
	Paths map[string]pathItem `yaml:"paths"`
}

type schemaDef struct {
	Ref         string               `yaml:"$ref"`
	Type        string               `yaml:"type"`
	Format      string               `yaml:"format"`
	Description string               `yaml:"description"`
	Enum        []string             `yaml:"enum"`
	Items       *schemaDef           `yaml:"items"`
	Properties  map[string]schemaDef `yaml:"properties"`
	Required    []string             `yaml:"required"`
	AllOf       []schemaRef          `yaml:"allOf"`
	Nullable    bool                 `yaml:"nullable"`
}

type schemaRef struct {
	Ref         string               `yaml:"$ref"`
	Type        string               `yaml:"type"`
	Format      string               `yaml:"format"`
	Description string               `yaml:"description"`
	Enum        []string             `yaml:"enum"`
	Items       *schemaRef           `yaml:"items"`
	Properties  map[string]schemaRef `yaml:"properties"`
	Required    []string             `yaml:"required"`
	AllOf       []schemaRef          `yaml:"allOf"`
	Nullable    bool                 `yaml:"nullable"`
}

type pathItem struct {
	Get    *operation `yaml:"get"`
	Post   *operation `yaml:"post"`
	Put    *operation `yaml:"put"`
	Patch  *operation `yaml:"patch"`
	Delete *operation `yaml:"delete"`
}

type operation struct {
	OperationID string                `yaml:"operationId"`
	Summary     string                `yaml:"summary"`
	Description string                `yaml:"description"`
	Tags        []string              `yaml:"tags"`
	Security    []map[string][]string `yaml:"security"`
	Parameters  []parameter           `yaml:"parameters"`
	RequestBody *requestBody          `yaml:"requestBody"`
	Responses   map[string]response   `yaml:"responses"`
}

type parameter struct {
	Name     string    `yaml:"name"`
	In       string    `yaml:"in"`
	Required bool      `yaml:"required"`
	Schema   schemaRef `yaml:"schema"`
}

type requestBody struct {
	Required bool             `yaml:"required"`
	Content  map[string]media `yaml:"content"`
}

type media struct {
	Schema schemaRef `yaml:"schema"`
}

type response struct {
	Description string           `yaml:"description"`
	Content     map[string]media `yaml:"content"`
}

func main() {
	checkFlag := flag.Bool("check", false, "exit non-zero if generated output differs from committed file")
	flag.Parse()

	repoRoot, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	yamlPath := filepath.Join(repoRoot, openapiPath)
	outPath := filepath.Join(repoRoot, outputPath)

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", yamlPath, err)
		os.Exit(1)
	}

	var doc openapiDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing %s: %v\n", yamlPath, err)
		os.Exit(1)
	}

	generated, err := generateTS(doc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generating TypeScript: %v\n", err)
		os.Exit(1)
	}

	if *checkFlag {
		existing, err := os.ReadFile(outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading %s: %v\n", outPath, err)
			os.Exit(1)
		}
		if !bytes.Equal(existing, generated) {
			fmt.Printf("FAIL: %s does not match regenerated output\n", outputPath)
			fmt.Println("Run: go run ./tools/contractgen")
			os.Exit(1)
		}
		fmt.Printf("PASS: %s matches regenerated output\n", outputPath)
		os.Exit(0)
	}

	if err := os.WriteFile(outPath, generated, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", outPath, err)
		os.Exit(1)
	}

	fmt.Printf("Generated %s from %s\n", outputPath, yamlPath)
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repo root (go.mod not found)")
		}
		dir = parent
	}
}

func generateTS(doc openapiDoc) ([]byte, error) {
	var buf bytes.Buffer

	fmt.Fprint(&buf, headerComment)
	fmt.Fprintln(&buf)

	schemas := doc.Components.Schemas
	schemaNames := sortedSchemaKeys(schemas)

	fmt.Fprintln(&buf, "// Type definitions")
	fmt.Fprintln(&buf)
	for _, name := range schemaNames {
		schema := schemas[name]
		tsType, err := schemaToTS(name, schema, schemas)
		if err != nil {
			return nil, fmt.Errorf("schema %s: %w", name, err)
		}
		fmt.Fprintln(&buf, tsType)
	}

	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "// Client")
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "const BASE = \"/api/admin/v1\";")
	fmt.Fprintln(&buf)
	fmt.Fprintln(&buf, "async function request<T>(path: string, init?: RequestInit): Promise<T> {")
	fmt.Fprintln(&buf, "\tconst res = await fetch(BASE + path, {")
	fmt.Fprintln(&buf, "\t\theaders: {")
	fmt.Fprintln(&buf, "\t\t\t'Content-Type': 'application/json',")
	fmt.Fprintln(&buf, "\t\t\t...init?.headers,")
	fmt.Fprintln(&buf, "\t\t},")
	fmt.Fprintln(&buf, "\t\t...init,")
	fmt.Fprintln(&buf, "\t});")
	fmt.Fprintln(&buf, "\tif (!res.ok) {")
	fmt.Fprintln(&buf, "\t\tconst body = await res.text();")
	fmt.Fprintln(&buf, "\t\tthrow new Error(body);")
	fmt.Fprintln(&buf, "\t}")
	fmt.Fprintln(&buf, "\treturn res.json();")
	fmt.Fprintln(&buf, "}")
	fmt.Fprintln(&buf)

	paths := sortedPathItems(doc.Paths)
	for _, pi := range paths {
		op := pi.op
		if op == nil {
			continue
		}
		method := strings.ToUpper(pi.method)
		opID := op.OperationID
		if opID == "" {
			opID = pi.path
		}

		params := extractPathParams(op)
		bodySchema := extractBody(op)
		respSchema := extractResponse(op)

		argList := []string{}
		for _, p := range params {
			argList = append(argList, p+": string")
		}
		if bodySchema != nil {
			bodyName := refName(bodySchema)
			argList = append(argList, "body: "+bodyName)
		}

		args := ""
		if len(argList) > 0 {
			args = "\t" + strings.Join(argList, ", ")
		}

		retType := "void"
		if respSchema != nil {
			retType = refType(respSchema)
		}

		fmt.Fprintf(&buf, "export function %s(%s): Promise<%s> {\n", camelOpID(opID), args, retType)
		initObj := buildInitObj(method, bodySchema, pi.path, params)
		fmt.Fprintf(&buf, "\treturn request<%s>(%s, %s);\n", retType, pathLit(pi.path, params), initObj)
		fmt.Fprintln(&buf, "}")
		fmt.Fprintln(&buf)
	}

	return append(bytes.TrimRight(buf.Bytes(), "\n"), '\n'), nil
}

type pathOp struct {
	path   string
	op     *operation
	method string
}

func sortedPathItems(paths map[string]pathItem) []pathOp {
	var result []pathOp
	for p, item := range paths {
		if item.Get != nil {
			result = append(result, pathOp{path: p, op: item.Get, method: "GET"})
		}
		if item.Post != nil {
			result = append(result, pathOp{path: p, op: item.Post, method: "POST"})
		}
		if item.Put != nil {
			result = append(result, pathOp{path: p, op: item.Put, method: "PUT"})
		}
		if item.Patch != nil {
			result = append(result, pathOp{path: p, op: item.Patch, method: "PATCH"})
		}
		if item.Delete != nil {
			result = append(result, pathOp{path: p, op: item.Delete, method: "DELETE"})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].path == result[j].path {
			return result[i].method < result[j].method
		}
		return result[i].path < result[j].path
	})
	return result
}

func sortedSchemaKeys(m map[string]schemaDef) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func schemaToTS(name string, s schemaDef, all map[string]schemaDef) (string, error) {
	if len(s.Enum) > 0 {
		vals := make([]string, len(s.Enum))
		for i, v := range s.Enum {
			vals[i] = fmt.Sprintf("%q", v)
		}
		return fmt.Sprintf("export type %s = %s;", name, strings.Join(vals, " | ")), nil
	}

	if s.Type == "array" && s.Items != nil {
		inner := tsTypeForDef(s.Items, all)
		return fmt.Sprintf("export type %s = %s[];", name, inner), nil
	}

	if s.Type == "object" || len(s.Properties) > 0 || len(s.AllOf) > 0 {
		var lines []string
		lines = append(lines, fmt.Sprintf("export interface %s {", name))

		if len(s.AllOf) > 0 {
			for _, ref := range s.AllOf {
				if ref.Ref != "" {
					base := refBaseName(ref.Ref)
					lines = append(lines, fmt.Sprintf("\textends %s;", base))
				}
			}
		}

		props := sortedPropKeys(s.Properties)
		for _, pname := range props {
			p := s.Properties[pname]
			tsType := tsTypeForDef(&p, all)
			opt := ""
			if !contains(s.Required, pname) {
				opt = "?"
			}
			desc := ""
			if p.Description != "" {
				desc = " // " + p.Description
			}
			lines = append(lines, fmt.Sprintf("\t%s%s: %s;%s", pname, opt, tsType, desc))
		}
		lines = append(lines, "}")
		return strings.Join(lines, "\n"), nil
	}

	if s.Type != "" {
		return fmt.Sprintf("export type %s = %s;", name, tsPrimitive(s.Type, s.Format)), nil
	}

	return fmt.Sprintf("export type %s = unknown;", name), nil
}

func tsTypeForDef(s *schemaDef, all map[string]schemaDef) string {
	if s == nil {
		return "unknown"
	}
	if s.Ref != "" {
		return refBaseName(s.Ref)
	}
	if len(s.AllOf) > 0 {
		var parts []string
		for _, ref := range s.AllOf {
			if ref.Ref != "" {
				parts = append(parts, refBaseName(ref.Ref))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " & ")
		}
	}
	if s.Type == "array" && s.Items != nil {
		return tsTypeForDef(s.Items, all) + "[]"
	}
	return tsPrimitive(s.Type, s.Format)
}

func refType(ref *schemaRef) string {
	if ref == nil {
		return "void"
	}
	if ref.Ref != "" {
		return refBaseName(ref.Ref)
	}
	if ref.Type == "array" && ref.Items != nil {
		return refType(ref.Items) + "[]"
	}
	return tsPrimitive(ref.Type, ref.Format)
}

func refName(ref *schemaRef) string {
	if ref == nil {
		return "unknown"
	}
	return refType(ref)
}

func refBaseName(r string) string {
	if strings.HasPrefix(r, "#/components/schemas/") {
		return r[len("#/components/schemas/"):]
	}
	return r
}

func tsPrimitive(t, format string) string {
	switch t {
	case "string":
		return "string"
	case "integer":
		return "number"
	case "number":
		return "number"
	case "boolean":
		return "boolean"
	default:
		return "unknown"
	}
}

func extractPathParams(op *operation) []string {
	var result []string
	for _, p := range op.Parameters {
		if p.In == "path" {
			result = append(result, p.Name)
		}
	}
	return result
}

func extractBody(op *operation) *schemaRef {
	if op.RequestBody == nil {
		return nil
	}
	for _, mt := range op.RequestBody.Content {
		return &mt.Schema
	}
	return nil
}

func extractResponse(op *operation) *schemaRef {
	for _, code := range []string{"200", "201", "202"} {
		if r, ok := op.Responses[code]; ok {
			for _, mt := range r.Content {
				return &mt.Schema
			}
		}
	}
	for _, r := range op.Responses {
		for _, mt := range r.Content {
			return &mt.Schema
		}
	}
	return nil
}

func buildInitObj(method string, body *schemaRef, path string, params []string) string {
	if body != nil {
		return "{\n\t\tmethod: \"" + method + "\",\n\t\tbody: JSON.stringify(body),\n\t}"
	}
	return "{ method: \"" + method + "\" }"
}

func pathLit(p string, params []string) string {
	if len(params) == 0 {
		return fmt.Sprintf("%q", p)
	}
	tmpl := p
	for _, param := range params {
		tmpl = strings.Replace(tmpl, "{"+param+"}", "${"+param+"}", 1)
	}
	return "`" + tmpl + "`"
}

func camelOpID(id string) string {
	if id == "" {
		return "request"
	}
	var b strings.Builder
	upper := true
	for _, ch := range id {
		if ch == '_' || ch == '-' || ch == ' ' {
			upper = true
			continue
		}
		if upper {
			b.WriteString(strings.ToUpper(string(ch)))
			upper = false
		} else {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func sortedPropKeys(m map[string]schemaDef) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
