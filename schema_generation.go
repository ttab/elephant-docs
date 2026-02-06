package elephantdocs

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/ttab/revisor"
)

// SchemaOverviewPage is the data for the schema overview template.
type SchemaOverviewPage struct {
	Title     string
	Version   string
	Documents []DocumentDoc
	Blocks    []BlockDoc
	Enums     []EnumDoc
	Policies  []PolicyDoc
	Sets      []SchemaSetDoc
}

// SchemaSetPage is the data for the schema set detail template.
type SchemaSetPage struct {
	Version string
	Set     SchemaSetDoc
}

// SchemaDocumentPage is the data for the document type template.
type SchemaDocumentPage struct {
	Version  string
	Document DocumentDoc
	Enums    []EnumDoc
	Blocks   []BlockDoc
}

// SchemaBlockPage is the data for the block definition template.
type SchemaBlockPage struct {
	Version string
	Block   BlockDoc
	Enums   []EnumDoc
}

// SchemaEnumPage is the data for the enum template.
type SchemaEnumPage struct {
	Version string
	Enum    EnumDoc
}

// SchemaPolicyPage is the data for the HTML policy template.
type SchemaPolicyPage struct {
	Version string
	Policy  PolicyDoc
}

// renderSchemaPages generates all schema documentation pages.
func renderSchemaPages(
	outDir string,
	doc *SchemaDoc,
	tpl *template.Template,
	menu []MenuItem,
	version string,
) error {
	localTpl, err := tpl.Clone()
	if err != nil {
		return fmt.Errorf("clone templates: %w", err)
	}

	// Render overview page.
	overviewDir := filepath.Join(outDir, "schemas")

	err = renderPage(overviewDir, localTpl, "schema_overview.html", Page{
		Title: "Document Schemas",
		Menu:  markActive(menu, "/schemas"),
		Contents: SchemaOverviewPage{
			Title:     "Document Schemas",
			Version:   version,
			Documents: doc.Documents,
			Blocks:    doc.Blocks,
			Enums:     doc.Enums,
			Policies:  doc.Policies,
			Sets:      doc.Sets,
		},
		Breadcrumb: []MenuItem{
			{Title: "Home", HRef: "/"},
			{Title: "Schemas"},
		},
	})
	if err != nil {
		return fmt.Errorf("render schema overview: %w", err)
	}

	// Render set detail pages.
	for _, set := range doc.Sets {
		setDir := filepath.Join(outDir, "schemas", set.Name)

		err = renderPage(setDir, localTpl, "schema_set.html", Page{
			Title: set.Title + " Schema Set",
			Menu:  markActive(menu, "/schemas/"+set.Name),
			Contents: SchemaSetPage{
				Version: version,
				Set:     set,
			},
			Breadcrumb: []MenuItem{
				{Title: "Home", HRef: "/"},
				{Title: "Schemas", HRef: "/schemas"},
				{Title: set.Title},
			},
		})
		if err != nil {
			return fmt.Errorf("render schema set %q: %w", set.Name, err)
		}
	}

	// Render document type pages.
	for _, d := range doc.Documents {
		docSlug := d.Type
		docDir := filepath.Join(outDir, "schemas", "documents", docSlug)

		err = os.MkdirAll(docDir, 0o770)
		if err != nil {
			return fmt.Errorf("create doc dir %q: %w", docDir, err)
		}

		err = renderPage(docDir, localTpl, "schema_document.html", Page{
			Title: docDisplayName(d),
			Menu:  markActive(menu, "/schemas/documents/"+d.Type),
			Contents: SchemaDocumentPage{
				Version:  version,
				Document: d,
				Enums:    doc.Enums,
				Blocks:   doc.Blocks,
			},
			Breadcrumb: []MenuItem{
				{Title: "Home", HRef: "/"},
				{Title: "Schemas", HRef: "/schemas"},
				{Title: docDisplayName(d)},
			},
		})
		if err != nil {
			return fmt.Errorf("render schema document %q: %w", d.Type, err)
		}
	}

	// Render block definition pages.
	for _, b := range doc.Blocks {
		blockSlug := SchemaSlug(b.ID)
		blockDir := filepath.Join(outDir, "schemas", "blocks", b.Kind, blockSlug)

		err = os.MkdirAll(blockDir, 0o770)
		if err != nil {
			return fmt.Errorf("create block dir %q: %w", blockDir, err)
		}

		err = renderPage(blockDir, localTpl, "schema_block.html", Page{
			Title: b.ID,
			Menu:  markActive(menu, "/schemas/blocks/"+b.Kind+"/"+SchemaSlug(b.ID)),
			Contents: SchemaBlockPage{
				Version: version,
				Block:   b,
				Enums:   doc.Enums,
			},
			Breadcrumb: []MenuItem{
				{Title: "Home", HRef: "/"},
				{Title: "Schemas", HRef: "/schemas"},
				{Title: "Blocks"},
				{Title: b.ID},
			},
		})
		if err != nil {
			return fmt.Errorf("render schema block %q: %w", b.ID, err)
		}
	}

	// Render enum pages.
	for _, e := range doc.Enums {
		enumSlug := SchemaSlug(e.ID)
		enumDir := filepath.Join(outDir, "schemas", "enums", enumSlug)

		err = os.MkdirAll(enumDir, 0o770)
		if err != nil {
			return fmt.Errorf("create enum dir %q: %w", enumDir, err)
		}

		err = renderPage(enumDir, localTpl, "schema_enum.html", Page{
			Title: enumDisplayName(e),
			Menu:  markActive(menu, "/schemas/enums/"+SchemaSlug(e.ID)),
			Contents: SchemaEnumPage{
				Version: version,
				Enum:    e,
			},
			Breadcrumb: []MenuItem{
				{Title: "Home", HRef: "/"},
				{Title: "Schemas", HRef: "/schemas"},
				{Title: "Enums"},
				{Title: enumDisplayName(e)},
			},
		})
		if err != nil {
			return fmt.Errorf("render schema enum %q: %w", e.ID, err)
		}
	}

	// Render HTML policy pages.
	for _, p := range doc.Policies {
		policyDir := filepath.Join(outDir, "schemas", "policies", p.Name)

		err = os.MkdirAll(policyDir, 0o770)
		if err != nil {
			return fmt.Errorf("create policy dir %q: %w", policyDir, err)
		}

		err = renderPage(policyDir, localTpl, "schema_policy.html", Page{
			Title: "HTML Policy: " + p.Name,
			Menu:  markActive(menu, "/schemas/policies/"+p.Name),
			Contents: SchemaPolicyPage{
				Version: version,
				Policy:  p,
			},
			Breadcrumb: []MenuItem{
				{Title: "Home", HRef: "/"},
				{Title: "Schemas", HRef: "/schemas"},
				{Title: "Policies"},
				{Title: p.Name},
			},
		})
		if err != nil {
			return fmt.Errorf("render schema policy %q: %w", p.Name, err)
		}
	}

	return nil
}

func docDisplayName(d DocumentDoc) string {
	if d.Name != "" {
		return d.Name
	}

	return d.Type
}

func enumDisplayName(e EnumDoc) string {
	if e.Name != "" {
		return e.Name
	}

	return e.ID
}

// schemaTemplateFuncs returns template functions for schema pages.
func schemaTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"schema_slug":     SchemaSlug,
		"schema_doc_name": docDisplayName,
		"schema_enum_name": enumDisplayName,
		"has_prefix":      strings.HasPrefix,
		"deref_int": func(p *int) int {
			if p == nil {
				return 0
			}
			return *p
		},
		"deref_string": func(p *string) string {
			if p == nil {
				return ""
			}
			return *p
		},
		"block_anchor": func(bc revisor.BlockConstraint) string {
			if bc.Declares != nil {
				parts := []string{}
				if bc.Declares.Type != "" {
					parts = append(parts, bc.Declares.Type)
				}
				if bc.Declares.Rel != "" {
					parts = append(parts, bc.Declares.Rel)
				}
				if bc.Declares.Role != "" {
					parts = append(parts, bc.Declares.Role)
				}

				anchor := strings.Join(parts, "-")
				anchor = strings.ReplaceAll(anchor, "/", "-")
				anchor = strings.ReplaceAll(anchor, " ", "-")

				return anchor
			}

			return ""
		},
		"match_doc_type": matchDocType,
		"nested_block_ctx": func(kind string, bc *revisor.BlockConstraint) map[string]interface{} {
			return map[string]interface{}{
				"Kind":  kind,
				"Block": bc,
			}
		},
		"glob_patterns": func(gl revisor.GlobList) string {
			var patterns []string
			for _, g := range gl {
				b, _ := g.MarshalJSON()
				var s string
				if json.Unmarshal(b, &s) == nil {
					patterns = append(patterns, s)
				}
			}
			return strings.Join(patterns, ", ")
		},
	}
}

// SchemaCard represents a schema document type for the home page.
type SchemaCard struct {
	Name        string
	Type        string
	URL         string
	Description string
	SetName     string
}
