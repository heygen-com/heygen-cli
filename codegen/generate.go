package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/heygen-com/heygen-cli/internal/command"
	"github.com/iancoleman/strcase"
)

// Generate writes Go source files from command.Groups using text/templates.
//
// For each group, it produces one .go file containing command.Spec struct
// literals as exported variables. It also produces a registry.go with a
// Groups map that the runtime builder uses to register commands.
//
// Example: given a group "video" with a Spec{Name: "list", Endpoint: "/v3/videos"},
// Generate writes gen/video.go containing:
//
//	var VideoList = &command.Spec{
//	    Group: "video",
//	    Name: "list",
//	    Endpoint: "/v3/videos",
//	    Method: "GET",
//	    ...
//	}
//
// All output is gofmt'd. Variable names are PascalCase derived from
// group + command name via strcase.ToCamel.
func Generate(groups command.Groups, tmplDir, outDir string) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	cmdTmpl, err := loadTemplate(filepath.Join(tmplDir, "command.go.tmpl"))
	if err != nil {
		return fmt.Errorf("loading command template: %w", err)
	}

	regTmpl, err := loadTemplate(filepath.Join(tmplDir, "registry.go.tmpl"))
	if err != nil {
		return fmt.Errorf("loading registry template: %w", err)
	}

	// One file per group (sorted for deterministic output)
	groupNames := groups.SortedNames()

	for _, name := range groupNames {
		data := struct {
			GroupName string
			Specs     []*command.Spec
		}{name, groups[name]}
		filename := filepath.Join(outDir, name+".go")
		if err := writeFromTemplate(cmdTmpl, data, filename); err != nil {
			return fmt.Errorf("generating %s: %w", filename, err)
		}
	}

	// Registry file
	regData := struct {
		Groups     command.Groups
		GroupNames []string
	}{groups, groupNames}
	regFilename := filepath.Join(outDir, "registry.go")
	if err := writeFromTemplate(regTmpl, regData, regFilename); err != nil {
		return fmt.Errorf("generating registry: %w", err)
	}

	return nil
}

func loadTemplate(path string) (*template.Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	funcMap := template.FuncMap{
		"quote":         quoteString,
		"intPtrLiteral": intPtrLiteral,
		"stringSlice":   stringSliceLiteral,
		"pascalCase":    strcase.ToCamel,
	}

	return template.New(filepath.Base(path)).Funcs(funcMap).Parse(string(data))
}

// writeFromTemplate executes a template with data and writes gofmt'd output.
func writeFromTemplate(tmpl *template.Template, data interface{}, filename string) error {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// Write unformatted for debugging
		_ = os.WriteFile(filename+".raw", buf.Bytes(), 0644)
		return fmt.Errorf("gofmt %s: %w (raw output written to %s.raw)", filename, err, filename)
	}

	return os.WriteFile(filename, formatted, 0644)
}

// Template helper functions

func quoteString(s string) string {
	return fmt.Sprintf("%q", s)
}

func intPtrLiteral(p *int) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("intPtr(%d)", *p)
}

func stringSliceLiteral(ss []string) string {
	if len(ss) == 0 {
		return "nil"
	}
	quoted := make([]string, len(ss))
	for i, s := range ss {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return "[]string{" + strings.Join(quoted, ", ") + "}"
}
