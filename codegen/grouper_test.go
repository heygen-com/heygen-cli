package main

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// loadGroupTestSpec loads the grouper-specific test spec.
func loadGroupTestSpec(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile("testdata/test_spec.yaml")
	if err != nil {
		t.Fatalf("loading test spec: %v", err)
	}
	return doc
}

func loadTestOverrides(t *testing.T) *Overrides {
	t.Helper()
	o, err := LoadOverrides("testdata/test_overrides.yaml")
	if err != nil {
		t.Fatalf("loading test overrides: %v", err)
	}
	return o
}

func TestGroupEndpoints_FilterV1(t *testing.T) {
	doc := loadGroupTestSpec(t)
	overrides := loadTestOverrides(t)
	groups := GroupEndpoints(doc, overrides)

	for _, g := range groups {
		if g.Name == "legacy" {
			t.Errorf("v1 endpoint should be filtered out, but found group %q", g.Name)
		}
	}
}

func TestGroupEndpoints_FilterHidden(t *testing.T) {
	doc := loadGroupTestSpec(t)
	overrides := loadTestOverrides(t)
	groups := GroupEndpoints(doc, overrides)

	for _, g := range groups {
		if g.Name == "hidden" {
			t.Errorf("x-cli-visible=false endpoint should be filtered out, but found group %q", g.Name)
		}
	}
}

func TestGroupEndpoints_GroupNames(t *testing.T) {
	doc := loadGroupTestSpec(t)
	overrides := loadTestOverrides(t)
	groups := GroupEndpoints(doc, overrides)

	names := make(map[string]bool)
	for _, g := range groups {
		names[g.Name] = true
	}

	expected := []string{"widget", "upload", "gadget", "thing"}
	for _, e := range expected {
		if !names[e] {
			t.Errorf("expected group %q, got groups: %v", e, names)
		}
	}
}

func TestGroupEndpoints_WidgetCommands(t *testing.T) {
	doc := loadGroupTestSpec(t)
	overrides := loadTestOverrides(t)
	groups := GroupEndpoints(doc, overrides)

	var widget *CommandGroup
	for i := range groups {
		if groups[i].Name == "widget" {
			widget = &groups[i]
			break
		}
	}
	if widget == nil {
		t.Fatal("expected 'widget' group")
	}

	cmds := make(map[string]GroupedCommand)
	for _, c := range widget.Commands {
		cmds[c.Name] = c
	}

	// Check expected subcommands
	expectedCmds := []string{"list", "create", "get", "delete", "activate"}
	for _, name := range expectedCmds {
		if _, ok := cmds[name]; !ok {
			t.Errorf("expected widget subcommand %q, got: %v", name, keys(cmds))
		}
	}

	// list should have query flags
	list := cmds["list"]
	if list.Method != "GET" {
		t.Errorf("list.Method = %q, want GET", list.Method)
	}
	if list.Endpoint != "/v3/widgets" {
		t.Errorf("list.Endpoint = %q, want /v3/widgets", list.Endpoint)
	}
	limitFlag := findFlag(list.Flags, "limit")
	if limitFlag == nil {
		t.Error("expected 'limit' flag on list command")
	} else {
		if limitFlag.Type != "int" {
			t.Errorf("limit.Type = %q, want int", limitFlag.Type)
		}
		if limitFlag.Default != "20" {
			t.Errorf("limit.Default = %q, want 20", limitFlag.Default)
		}
		if limitFlag.Source != "query" {
			t.Errorf("limit.Source = %q, want query", limitFlag.Source)
		}
		if limitFlag.Min == nil || *limitFlag.Min != 1 {
			t.Errorf("limit.Min = %v, want 1", limitFlag.Min)
		}
		if limitFlag.Max == nil || *limitFlag.Max != 100 {
			t.Errorf("limit.Max = %v, want 100", limitFlag.Max)
		}
	}

	// create should have body flags
	create := cmds["create"]
	if create.BodyEncoding != "json" {
		t.Errorf("create.BodyEncoding = %q, want json", create.BodyEncoding)
	}
	nameFlag := findFlag(create.Flags, "name")
	if nameFlag == nil {
		t.Error("expected 'name' flag on create command")
	} else {
		if !nameFlag.Required {
			t.Error("name flag should be required")
		}
		if nameFlag.Source != "body" {
			t.Errorf("name.Source = %q, want body", nameFlag.Source)
		}
	}
	colorFlag := findFlag(create.Flags, "color")
	if colorFlag == nil {
		t.Error("expected 'color' flag on create command")
	} else {
		if len(colorFlag.Enum) != 3 {
			t.Errorf("color.Enum = %v, want [red green blue]", colorFlag.Enum)
		}
	}
	// metadata (object) should be skipped (complex)
	metaFlag := findFlag(create.Flags, "metadata")
	if metaFlag != nil {
		t.Error("metadata flag should be skipped (complex object)")
	}
	// tags (array of strings) should be present
	tagsFlag := findFlag(create.Flags, "tags")
	if tagsFlag == nil {
		t.Error("expected 'tags' flag on create command (string array)")
	} else {
		if tagsFlag.Type != "stringSlice" {
			t.Errorf("tags.Type = %q, want stringSlice", tagsFlag.Type)
		}
	}

	// get should have path arg
	get := cmds["get"]
	if len(get.Args) == 0 {
		t.Error("expected path arg on get command")
	} else {
		if get.Args[0].Target != "path" {
			t.Errorf("get.Args[0].Target = %q, want path", get.Args[0].Target)
		}
		if get.Args[0].Param != "widget_id" {
			t.Errorf("get.Args[0].Param = %q, want widget_id", get.Args[0].Param)
		}
	}

	// activate should have path arg
	activate := cmds["activate"]
	if len(activate.Args) == 0 {
		t.Error("expected path arg on activate command")
	} else {
		if activate.Args[0].Param != "widget_id" {
			t.Errorf("activate.Args[0].Param = %q, want widget_id", activate.Args[0].Param)
		}
	}
}

func TestGroupEndpoints_UploadCommand(t *testing.T) {
	doc := loadGroupTestSpec(t)
	overrides := loadTestOverrides(t)
	groups := GroupEndpoints(doc, overrides)

	var upload *CommandGroup
	for i := range groups {
		if groups[i].Name == "upload" {
			upload = &groups[i]
			break
		}
	}
	if upload == nil {
		t.Fatal("expected 'upload' group")
	}

	if len(upload.Commands) != 1 {
		t.Fatalf("expected 1 command in upload group, got %d", len(upload.Commands))
	}

	cmd := upload.Commands[0]
	if cmd.Name != "upload" {
		t.Errorf("upload command name = %q, want upload", cmd.Name)
	}
	if cmd.BodyEncoding != "multipart" {
		t.Errorf("upload.BodyEncoding = %q, want multipart", cmd.BodyEncoding)
	}

	// File should be promoted to positional arg via override
	fileArg := findArg(cmd.Args, "file")
	if fileArg == nil {
		t.Error("expected 'file' positional arg (from override)")
	} else {
		if fileArg.Target != "file" {
			t.Errorf("file.Target = %q, want file", fileArg.Target)
		}
	}
}

func TestGroupEndpoints_Pagination(t *testing.T) {
	doc := loadGroupTestSpec(t)
	overrides := loadTestOverrides(t)
	groups := GroupEndpoints(doc, overrides)

	var widget *CommandGroup
	for i := range groups {
		if groups[i].Name == "widget" {
			widget = &groups[i]
			break
		}
	}
	if widget == nil {
		t.Fatal("expected 'widget' group")
	}

	var list *GroupedCommand
	for i := range widget.Commands {
		if widget.Commands[i].Name == "list" {
			list = &widget.Commands[i]
			break
		}
	}
	if list == nil {
		t.Fatal("expected 'list' command")
	}

	if list.TokenField != "token" {
		t.Errorf("list.TokenField = %q, want token", list.TokenField)
	}
	if list.DataField != "data" {
		t.Errorf("list.DataField = %q, want data", list.DataField)
	}
}

func TestGroupEndpoints_VarNames(t *testing.T) {
	doc := loadGroupTestSpec(t)
	overrides := loadTestOverrides(t)
	groups := GroupEndpoints(doc, overrides)

	varNames := make(map[string]bool)
	for _, g := range groups {
		for _, c := range g.Commands {
			varNames[c.VarName] = true
		}
	}

	expected := []string{"WidgetList", "WidgetCreate", "WidgetGet", "WidgetDelete", "WidgetActivate"}
	for _, e := range expected {
		if !varNames[e] {
			t.Errorf("expected VarName %q, got: %v", e, varNames)
		}
	}
}

func TestGroupEndpoints_Examples(t *testing.T) {
	doc := loadGroupTestSpec(t)
	overrides := loadTestOverrides(t)
	groups := GroupEndpoints(doc, overrides)

	var widget *CommandGroup
	for i := range groups {
		if groups[i].Name == "widget" {
			widget = &groups[i]
			break
		}
	}
	if widget == nil {
		t.Fatal("expected 'widget' group")
	}

	for _, c := range widget.Commands {
		if c.Name == "list" {
			if len(c.Examples) != 1 || c.Examples[0] != "heygen widget list --limit 10" {
				t.Errorf("list.Examples = %v, want [heygen widget list --limit 10]", c.Examples)
			}
		}
	}
}

func TestGroupEndpoints_AnyOfType(t *testing.T) {
	doc := loadGroupTestSpec(t)
	overrides := loadTestOverrides(t)
	groups := GroupEndpoints(doc, overrides)

	var gadget *CommandGroup
	for i := range groups {
		if groups[i].Name == "gadget" {
			gadget = &groups[i]
			break
		}
	}
	if gadget == nil {
		t.Fatal("expected 'gadget' group")
	}

	var list *GroupedCommand
	for i := range gadget.Commands {
		if gadget.Commands[i].Name == "list" {
			list = &gadget.Commands[i]
			break
		}
	}
	if list == nil {
		t.Fatal("expected 'list' command in gadget group")
	}

	statusFlag := findFlag(list.Flags, "status")
	if statusFlag == nil {
		t.Error("expected 'status' flag on gadget list")
	} else {
		if statusFlag.Type != "string" {
			t.Errorf("status.Type = %q, want string (first anyOf type)", statusFlag.Type)
		}
	}
}

func TestGroupEndpoints_BoolAndFloatDefaults(t *testing.T) {
	doc := loadGroupTestSpec(t)
	overrides := loadTestOverrides(t)
	groups := GroupEndpoints(doc, overrides)

	var thing *CommandGroup
	for i := range groups {
		if groups[i].Name == "thing" {
			thing = &groups[i]
			break
		}
	}
	if thing == nil {
		t.Fatal("expected 'thing' group")
	}

	cmd := thing.Commands[0]
	enabledFlag := findFlag(cmd.Flags, "enabled")
	if enabledFlag == nil {
		t.Error("expected 'enabled' flag")
	} else {
		if enabledFlag.Type != "bool" {
			t.Errorf("enabled.Type = %q, want bool", enabledFlag.Type)
		}
		if enabledFlag.Default != "true" {
			t.Errorf("enabled.Default = %q, want true", enabledFlag.Default)
		}
	}

	ratioFlag := findFlag(cmd.Flags, "ratio")
	if ratioFlag == nil {
		t.Error("expected 'ratio' flag")
	} else {
		if ratioFlag.Type != "float" {
			t.Errorf("ratio.Type = %q, want float", ratioFlag.Type)
		}
	}
}

func TestGroupEndpoints_SortedOutput(t *testing.T) {
	doc := loadGroupTestSpec(t)
	overrides := loadTestOverrides(t)
	groups := GroupEndpoints(doc, overrides)

	// Groups should be sorted alphabetically
	for i := 1; i < len(groups); i++ {
		if groups[i].Name < groups[i-1].Name {
			t.Errorf("groups not sorted: %q before %q", groups[i-1].Name, groups[i].Name)
		}
	}

	// Commands within each group should be sorted
	for _, g := range groups {
		for i := 1; i < len(g.Commands); i++ {
			if g.Commands[i].Name < g.Commands[i-1].Name {
				t.Errorf("commands in group %q not sorted: %q before %q",
					g.Name, g.Commands[i-1].Name, g.Commands[i].Name)
			}
		}
	}
}

func TestGroupEndpoints_DataFieldWithoutPagination(t *testing.T) {
	doc := loadGroupTestSpec(t)
	overrides := loadTestOverrides(t)
	groups := GroupEndpoints(doc, overrides)

	var gadget *CommandGroup
	for i := range groups {
		if groups[i].Name == "gadget" {
			gadget = &groups[i]
			break
		}
	}
	if gadget == nil {
		t.Fatal("expected 'gadget' group")
	}

	var list *GroupedCommand
	for i := range gadget.Commands {
		if gadget.Commands[i].Name == "list" {
			list = &gadget.Commands[i]
			break
		}
	}
	if list == nil {
		t.Fatal("expected 'list' command in gadget")
	}

	// data field is present in response but no pagination token
	if list.DataField != "data" {
		t.Errorf("list.DataField = %q, want data", list.DataField)
	}
	if list.TokenField != "" {
		t.Errorf("list.TokenField = %q, want empty (no pagination)", list.TokenField)
	}
}

// --- Helpers ---

func findFlag(flags []FlagDef, name string) *FlagDef {
	for i := range flags {
		if flags[i].Name == name {
			return &flags[i]
		}
	}
	return nil
}

func findArg(args []ArgDef, name string) *ArgDef {
	for i := range args {
		if args[i].Name == name {
			return &args[i]
		}
	}
	return nil
}

func keys(m map[string]GroupedCommand) []string {
	var result []string
	for k := range m {
		result = append(result, k)
	}
	return result
}
