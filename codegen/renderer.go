package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// Render writes generated Go files for each command group and a registry file.
func Render(groups []CommandGroup, tmplDir, outDir string) error {
	// Ensure output directory exists
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// Load templates
	cmdTmpl, err := loadTemplate(filepath.Join(tmplDir, "command.go.tmpl"))
	if err != nil {
		return fmt.Errorf("loading command template: %w", err)
	}

	regTmpl, err := loadTemplate(filepath.Join(tmplDir, "registry.go.tmpl"))
	if err != nil {
		return fmt.Errorf("loading registry template: %w", err)
	}

	// Render one file per command group
	for _, group := range groups {
		filename := filepath.Join(outDir, group.Name+".go")
		if err := renderFile(cmdTmpl, group, filename); err != nil {
			return fmt.Errorf("rendering %s: %w", filename, err)
		}
	}

	// Render registry file
	regFilename := filepath.Join(outDir, "registry.go")
	if err := renderFile(regTmpl, groups, regFilename); err != nil {
		return fmt.Errorf("rendering registry: %w", err)
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
		"quoteOrNil":    quoteOrNil,
		"intPtrLiteral": intPtrLiteral,
		"stringSlice":   stringSliceLiteral,
		"hasFlags":      hasFlags,
		"hasArgs":       hasArgs,
		"hasExamples":   hasExamples,
		"hasEnum":       hasEnum,
	}

	return template.New(filepath.Base(path)).Funcs(funcMap).Parse(string(data))
}

func renderFile(tmpl *template.Template, data interface{}, filename string) error {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	// Format the Go source
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// Write unformatted for debugging
		_ = os.WriteFile(filename+".raw", buf.Bytes(), 0644)
		return fmt.Errorf("formatting %s: %w\nRaw output written to %s.raw", filename, err, filename)
	}

	return os.WriteFile(filename, formatted, 0644)
}

// Template helper functions

func quoteString(s string) string {
	return fmt.Sprintf("%q", s)
}

func quoteOrNil(s string) string {
	if s == "" {
		return `""`
	}
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

func hasFlags(cmd GroupedCommand) bool {
	return len(cmd.Flags) > 0
}

func hasArgs(cmd GroupedCommand) bool {
	return len(cmd.Args) > 0
}

func hasExamples(cmd GroupedCommand) bool {
	return len(cmd.Examples) > 0
}

func hasEnum(f FlagDef) bool {
	return len(f.Enum) > 0
}
