package elephantdocs

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/ttab/revisor"
)

// SchemaDoc is the resolved documentation model for all schemas.
type SchemaDoc struct {
	Sets      []SchemaSetDoc
	Documents []DocumentDoc
	Blocks    []BlockDoc
	Enums     []EnumDoc
	Policies  []PolicyDoc
}

// SchemaSetDoc holds raw set data plus its name/title.
type SchemaSetDoc struct {
	Name  string
	Title string
	Set   revisor.ConstraintSet
}

// DocumentDoc is a merged view of a document type across constraint sets.
type DocumentDoc struct {
	Type        string
	Name        string
	Description string
	DeclaredIn  string
	ExtendedBy  []string
	Attributes  revisor.ConstraintMap
	Meta        []ResolvedBlock
	Links       []ResolvedBlock
	Content     []ResolvedBlock
	Deprecated  *revisor.Deprecation
}

// ResolvedBlock tracks where each block constraint came from.
type ResolvedBlock struct {
	Source     string // which set contributed this
	Ref       string // non-empty if from a ref
	Block     revisor.BlockConstraint
	BlockKind string // "meta", "link", or "content"
}

// BlockDoc documents a reusable block definition.
type BlockDoc struct {
	ID         string
	Kind       string // "meta", "link", or "content"
	DeclaredIn string
	Block      revisor.BlockConstraint
	UsedBy     []string // document types that ref this
}

// EnumDoc is a merged view of an enum across sets.
type EnumDoc struct {
	ID          string
	Name        string
	Description string
	DeclaredIn  string
	ExtendedBy  []string
	Values      []EnumValueDoc
}

// EnumValueDoc represents one enum value.
type EnumValueDoc struct {
	Value       string
	Description string
	Source      string
	Forbidden   bool
	Deprecated  *revisor.Deprecation
}

// PolicyDoc documents an HTML policy.
type PolicyDoc struct {
	Name        string
	Description string
	DeclaredIn  string
	Uses        string
	Extends     string
	Elements    map[string]revisor.HTMLElement
}

type blockDef struct {
	setName string
	kind    string
	block   revisor.BlockConstraint
}

// loadConstraintSets reads and deserializes schema JSON files from a git commit tree.
func loadConstraintSets(commit *object.Commit, conf SchemaGroupConfig) ([]revisor.ConstraintSet, error) {
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("get commit tree: %w", err)
	}

	sets := make([]revisor.ConstraintSet, 0, len(conf.Sets))

	for _, sc := range conf.Sets {
		f, err := tree.File(sc.File)
		if err != nil {
			return nil, fmt.Errorf("open schema %q: %w", sc.File, err)
		}

		reader, err := f.Reader()
		if err != nil {
			return nil, fmt.Errorf("read schema %q: %w", sc.File, err)
		}

		dec := json.NewDecoder(reader)

		var cs revisor.ConstraintSet

		err = dec.Decode(&cs)

		reader.Close()

		if err != nil {
			return nil, fmt.Errorf("decode schema %q: %w", sc.File, err)
		}

		sets = append(sets, cs)
	}

	return sets, nil
}

// resolveSchemas merges all constraint sets into a unified documentation model.
func resolveSchemas(
	sets []revisor.ConstraintSet, conf SchemaGroupConfig,
) (*SchemaDoc, error) {
	doc := &SchemaDoc{}

	// Build set docs.
	for i, sc := range conf.Sets {
		doc.Sets = append(doc.Sets, SchemaSetDoc{
			Name:  sc.Name,
			Title: sc.Title,
			Set:   sets[i],
		})
	}

	// Index block definitions by ID across all sets.
	blockIndex := make(map[string]blockDef)

	for i, cs := range sets {
		setName := conf.Sets[i].Name

		for _, bd := range cs.Meta {
			blockIndex[bd.ID] = blockDef{
				setName: setName,
				kind:    "meta",
				block:   bd.Block,
			}
		}

		for _, bd := range cs.Links {
			blockIndex[bd.ID] = blockDef{
				setName: setName,
				kind:    "link",
				block:   bd.Block,
			}
		}

		for _, bd := range cs.Content {
			blockIndex[bd.ID] = blockDef{
				setName: setName,
				kind:    "content",
				block:   bd.Block,
			}
		}
	}

	// Build block docs.
	blockUsedBy := make(map[string][]string) // block ID -> doc types

	for id, bd := range blockIndex {
		doc.Blocks = append(doc.Blocks, BlockDoc{
			ID:         id,
			Kind:       bd.kind,
			DeclaredIn: bd.setName,
			Block:      bd.block,
		})
	}

	// Group documents by declared type.
	type docEntry struct {
		declaredIn string
		doc        revisor.DocumentConstraint
		extendedBy []string
		meta       []ResolvedBlock
		links      []ResolvedBlock
		content    []ResolvedBlock
	}

	docsByType := make(map[string]*docEntry)
	var docOrder []string

	for i, cs := range sets {
		setName := conf.Sets[i].Name

		for _, dc := range cs.Documents {
			if dc.Declares != "" {
				// This declares a new document type.
				if _, exists := docsByType[dc.Declares]; !exists {
					docsByType[dc.Declares] = &docEntry{
						declaredIn: setName,
						doc:        dc,
					}
					docOrder = append(docOrder, dc.Declares)
				}

				entry := docsByType[dc.Declares]

				// Resolve blocks for this declaration.
				entry.meta = append(entry.meta,
					resolveBlocks(setName, "meta", dc.Meta, blockIndex, blockUsedBy, dc.Declares)...)
				entry.links = append(entry.links,
					resolveBlocks(setName, "link", dc.Links, blockIndex, blockUsedBy, dc.Declares)...)
				entry.content = append(entry.content,
					resolveBlocks(setName, "content", dc.Content, blockIndex, blockUsedBy, dc.Declares)...)
			} else if len(dc.Match.Keys) > 0 {
				// This extends an existing document type via match.
				docType := matchDocType(dc.Match)
				if docType == "" {
					continue
				}

				entry, exists := docsByType[docType]
				if !exists {
					// Create a placeholder that will be populated
					// if the declaring set is loaded.
					entry = &docEntry{
						declaredIn: "",
						doc:        dc,
					}
					docsByType[docType] = entry
					docOrder = append(docOrder, docType)
				}

				entry.extendedBy = appendUnique(entry.extendedBy, setName)

				entry.meta = append(entry.meta,
					resolveBlocks(setName, "meta", dc.Meta, blockIndex, blockUsedBy, docType)...)
				entry.links = append(entry.links,
					resolveBlocks(setName, "link", dc.Links, blockIndex, blockUsedBy, docType)...)
				entry.content = append(entry.content,
					resolveBlocks(setName, "content", dc.Content, blockIndex, blockUsedBy, docType)...)

				// Merge attributes from match extensions.
				if len(dc.Attributes.Keys) > 0 && entry.doc.Attributes.Constraints == nil {
					entry.doc.Attributes = dc.Attributes
				} else if len(dc.Attributes.Keys) > 0 {
					for _, k := range dc.Attributes.Keys {
						if _, exists := entry.doc.Attributes.Constraints[k]; !exists {
							entry.doc.Attributes.Keys = append(entry.doc.Attributes.Keys, k)
							entry.doc.Attributes.Constraints[k] = dc.Attributes.Constraints[k]
						}
					}
				}
			}
		}
	}

	// Merge match block extensions into their target declares blocks.
	for _, entry := range docsByType {
		entry.meta = mergeBlockExtensions(entry.meta)
		entry.links = mergeBlockExtensions(entry.links)
		entry.content = mergeBlockExtensions(entry.content)
	}

	// Build document docs from merged entries.
	for _, docType := range docOrder {
		entry := docsByType[docType]

		dd := DocumentDoc{
			Type:        docType,
			Name:        entry.doc.Name,
			Description: entry.doc.Description,
			DeclaredIn:  entry.declaredIn,
			ExtendedBy:  entry.extendedBy,
			Attributes:  entry.doc.Attributes,
			Meta:        entry.meta,
			Links:       entry.links,
			Content:     entry.content,
			Deprecated:  entry.doc.Deprecated,
		}

		doc.Documents = append(doc.Documents, dd)
	}

	// Sort documents by type.
	slices.SortFunc(doc.Documents, func(a, b DocumentDoc) int {
		return strings.Compare(a.Type, b.Type)
	})

	// Update block UsedBy from collected data.
	for i := range doc.Blocks {
		if refs, ok := blockUsedBy[doc.Blocks[i].ID]; ok {
			doc.Blocks[i].UsedBy = refs
		}
	}

	// Sort blocks by ID.
	slices.SortFunc(doc.Blocks, func(a, b BlockDoc) int {
		return strings.Compare(a.ID, b.ID)
	})

	// Merge enums.
	type enumEntry struct {
		id          string
		name        string
		description string
		declaredIn  string
		extendedBy  []string
		values      []EnumValueDoc
	}

	enumsByID := make(map[string]*enumEntry)
	var enumOrder []string

	for i, cs := range sets {
		setName := conf.Sets[i].Name

		for _, e := range cs.Enums {
			if e.Declare != "" {
				if _, exists := enumsByID[e.Declare]; !exists {
					enumsByID[e.Declare] = &enumEntry{
						id:          e.Declare,
						name:        e.Name,
						description: e.Description,
						declaredIn:  setName,
					}
					enumOrder = append(enumOrder, e.Declare)
				}

				entry := enumsByID[e.Declare]

				for v, ec := range e.Values {
					entry.values = append(entry.values, EnumValueDoc{
						Value:       v,
						Description: ec.Description,
						Source:      setName,
						Forbidden:   ec.Forbidden,
						Deprecated:  ec.Deprecated,
					})
				}
			} else if e.Match != "" {
				entry, exists := enumsByID[e.Match]
				if !exists {
					entry = &enumEntry{
						id: e.Match,
					}
					enumsByID[e.Match] = entry
					enumOrder = append(enumOrder, e.Match)
				}

				entry.extendedBy = appendUnique(entry.extendedBy, setName)

				for v, ec := range e.Values {
					entry.values = append(entry.values, EnumValueDoc{
						Value:       v,
						Description: ec.Description,
						Source:      setName,
						Forbidden:   ec.Forbidden,
						Deprecated:  ec.Deprecated,
					})
				}
			}
		}
	}

	for _, id := range enumOrder {
		entry := enumsByID[id]

		slices.SortFunc(entry.values, func(a, b EnumValueDoc) int {
			return strings.Compare(a.Value, b.Value)
		})

		doc.Enums = append(doc.Enums, EnumDoc{
			ID:          entry.id,
			Name:        entry.name,
			Description: entry.description,
			DeclaredIn:  entry.declaredIn,
			ExtendedBy:  entry.extendedBy,
			Values:      entry.values,
		})
	}

	// Merge HTML policies.
	for i, cs := range sets {
		setName := conf.Sets[i].Name

		for _, p := range cs.HTMLPolicies {
			doc.Policies = append(doc.Policies, PolicyDoc{
				Name:        p.Name,
				Description: p.Description,
				DeclaredIn:  setName,
				Uses:        p.Uses,
				Extends:     p.Extends,
				Elements:    p.Elements,
			})
		}
	}

	return doc, nil
}

// resolveBlocks processes a list of block constraints, resolving refs and
// tracking provenance.
func resolveBlocks(
	setName, kind string,
	blocks []*revisor.BlockConstraint,
	index map[string]blockDef,
	usedBy map[string][]string,
	docType string,
) []ResolvedBlock {
	var result []ResolvedBlock

	for _, bc := range blocks {
		if bc == nil {
			continue
		}

		if bc.Ref != "" {
			// This is a reference to a block definition.
			if _, ok := index[bc.Ref]; ok {
				def := index[bc.Ref]

				// Merge any overrides from the ref usage.
				resolved := def.block
				if bc.Count != nil {
					resolved.Count = bc.Count
				}
				if bc.MaxCount != nil {
					resolved.MaxCount = bc.MaxCount
				}
				if bc.MinCount != nil {
					resolved.MinCount = bc.MinCount
				}

				result = append(result, ResolvedBlock{
					Source:    setName,
					Ref:       bc.Ref,
					Block:     resolved,
					BlockKind: kind,
				})

				usedBy[bc.Ref] = appendUnique(usedBy[bc.Ref], docType)
			} else {
				// Unknown ref, include as-is.
				result = append(result, ResolvedBlock{
					Source:    setName,
					Ref:       bc.Ref,
					Block:     *bc,
					BlockKind: kind,
				})
			}
		} else {
			result = append(result, ResolvedBlock{
				Source:    setName,
				Block:     *bc,
				BlockKind: kind,
			})
		}
	}

	return result
}

// matchDocType extracts the document type from a match ConstraintMap.
// It looks for a "type" key with a "const" value.
func matchDocType(m revisor.ConstraintMap) string {
	sc, ok := m.Constraints["type"]
	if !ok {
		return ""
	}

	if sc.Const != nil {
		return *sc.Const
	}

	return ""
}

func appendUnique(s []string, v string) []string {
	for _, existing := range s {
		if existing == v {
			return s
		}
	}

	return append(s, v)
}

// blockMatchesDeclares checks if a block's Match ConstraintMap targets
// a block with the given Declares BlockSignature.
func blockMatchesDeclares(match revisor.ConstraintMap, declares *revisor.BlockSignature) bool {
	if declares == nil || len(match.Keys) == 0 {
		return false
	}

	for _, k := range match.Keys {
		sc := match.Constraints[k]
		if sc.Const == nil {
			continue
		}

		switch k {
		case "type":
			if declares.Type != *sc.Const {
				return false
			}
		case "rel":
			if declares.Rel != *sc.Const {
				return false
			}
		case "role":
			if declares.Role != *sc.Const {
				return false
			}
		default:
			return false
		}
	}

	return true
}

// mergeBlockExtensions merges match blocks into the declares blocks they
// target at the top level (ResolvedBlock). It also recurses into nested
// sub-blocks within each block.
func mergeBlockExtensions(blocks []ResolvedBlock) []ResolvedBlock {
	// First, recursively merge within each block's nested sub-blocks.
	for i := range blocks {
		blocks[i].Block.Meta = mergeNestedBlockExtensions(blocks[i].Block.Meta)
		blocks[i].Block.Links = mergeNestedBlockExtensions(blocks[i].Block.Links)
		blocks[i].Block.Content = mergeNestedBlockExtensions(blocks[i].Block.Content)
	}

	// Separate declares/ref blocks from match blocks at this level.
	var result []ResolvedBlock
	var matchBlocks []ResolvedBlock

	for _, b := range blocks {
		if len(b.Block.Match.Keys) > 0 && b.Block.Declares == nil && b.Ref == "" {
			matchBlocks = append(matchBlocks, b)
		} else {
			result = append(result, b)
		}
	}

	// Merge each match block into its target.
	for _, mb := range matchBlocks {
		merged := false

		for i := range result {
			if blockMatchesDeclares(mb.Block.Match, result[i].Block.Declares) {
				result[i].Block.Meta = append(result[i].Block.Meta, mb.Block.Meta...)
				result[i].Block.Links = append(result[i].Block.Links, mb.Block.Links...)
				result[i].Block.Content = append(result[i].Block.Content, mb.Block.Content...)
				mergeConstraintMap(&result[i].Block.Attributes, mb.Block.Attributes)
				mergeConstraintMap(&result[i].Block.Data, mb.Block.Data)

				merged = true

				break
			}
		}

		if !merged {
			// No target found, keep as-is.
			result = append(result, mb)
		}
	}

	return result
}

// mergeNestedBlockExtensions does the same merge for nested
// []*BlockConstraint sub-blocks, recursively.
func mergeNestedBlockExtensions(blocks []*revisor.BlockConstraint) []*revisor.BlockConstraint {
	if len(blocks) == 0 {
		return blocks
	}

	// Recurse into each block's sub-blocks first.
	for _, b := range blocks {
		if b == nil {
			continue
		}

		b.Meta = mergeNestedBlockExtensions(b.Meta)
		b.Links = mergeNestedBlockExtensions(b.Links)
		b.Content = mergeNestedBlockExtensions(b.Content)
	}

	// Separate declares blocks from match blocks.
	var result []*revisor.BlockConstraint
	var matchBlocks []*revisor.BlockConstraint

	for _, b := range blocks {
		if b == nil {
			continue
		}

		if len(b.Match.Keys) > 0 && b.Declares == nil && b.Ref == "" {
			matchBlocks = append(matchBlocks, b)
		} else {
			result = append(result, b)
		}
	}

	for _, mb := range matchBlocks {
		merged := false

		for _, target := range result {
			if blockMatchesDeclares(mb.Match, target.Declares) {
				target.Meta = append(target.Meta, mb.Meta...)
				target.Links = append(target.Links, mb.Links...)
				target.Content = append(target.Content, mb.Content...)
				mergeConstraintMap(&target.Attributes, mb.Attributes)
				mergeConstraintMap(&target.Data, mb.Data)

				merged = true

				break
			}
		}

		if !merged {
			result = append(result, mb)
		}
	}

	return result
}

// mergeConstraintMap adds keys from src into dst that don't already exist.
func mergeConstraintMap(dst *revisor.ConstraintMap, src revisor.ConstraintMap) {
	if len(src.Keys) == 0 {
		return
	}

	if dst.Constraints == nil {
		dst.Constraints = make(map[string]revisor.StringConstraint)
	}

	for _, k := range src.Keys {
		if _, exists := dst.Constraints[k]; !exists {
			dst.Keys = append(dst.Keys, k)
			dst.Constraints[k] = src.Constraints[k]
		}
	}
}

// SchemaSlug converts a schema ID like "core://note" to a URL-safe path
// like "core/note".
func SchemaSlug(id string) string {
	s := strings.ReplaceAll(id, "://", "/")

	return s
}
