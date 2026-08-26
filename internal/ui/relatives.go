package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/vulcanshen/kbu/internal/k8s"
	"github.com/vulcanshen/kbu/internal/theme"
)

// relativeEntry is one row of the Relatives tab. Two flavours:
//   - section: header row (just a label, not selectable)
//   - drill:   label + value with a ref (Enter fetches & opens YamlPopup)
//
// There's no "info-only selectable row" by design — Relatives is strictly a
// navigation hub. Pure-info content (labels, annotations, container image
// strings, status fields) lives in the `Y` YAML popup. Cursor only lands on
// rows that have somewhere to go.
type relativeEntry struct {
	label   string
	value   string
	ref     *k8s.RefTarget
	section bool
}

func (e relativeEntry) isSelectable() bool { return !e.section && e.ref != nil }

// buildPodRelativeEntries renders the Pod-specific Relatives list using the parsed
// PodRelativesData stashed in ResourceDetail. Returns an empty slice when no
// PodRelatives are present (e.g. detail still loading) — the renderer will then
// show a placeholder hint.
//
// Strict refs only: Owner, Node, ServiceAccount, and Volumes whose source is
// another K8s resource (ConfigMap / Secret / PVC). Volumes with non-K8s
// sources (emptyDir / hostPath / projected / downwardAPI) and container
// images are intentionally excluded — they're not navigable, so they belong
// in the YAML popup, not here.
func buildPodRelativeEntries(detail k8s.ResourceDetail) []relativeEntry {
	if detail.PodRelatives == nil {
		return nil
	}
	po := detail.PodRelatives
	var entries []relativeEntry

	if po.Owner != nil {
		entries = append(entries, relativeEntry{
			label: "Owner",
			value: ownerDisplay(*po.Owner),
			ref:   po.Owner,
		})
	}
	if po.Node != nil {
		entries = append(entries, relativeEntry{
			label: "Node",
			value: po.Node.Name,
			ref:   po.Node,
		})
	}
	if po.ServiceAccount != nil {
		entries = append(entries, relativeEntry{
			label: "ServiceAccount",
			value: po.ServiceAccount.Name,
			ref:   po.ServiceAccount,
		})
	}

	// Only drillable volumes — others would be unreachable cursor positions.
	var drillVols []k8s.VolumeRef
	for _, v := range po.Volumes {
		if v.Ref != nil {
			drillVols = append(drillVols, v)
		}
	}
	if len(drillVols) > 0 {
		entries = append(entries, relativeEntry{section: true, label: "Volumes"})
		for _, v := range drillVols {
			entries = append(entries, relativeEntry{
				label: "  " + v.Name,
				value: v.Kind + "/" + v.Ref.Name,
				ref:   v.Ref,
			})
		}
	}

	return entries
}

// buildServiceRelativeEntries renders Service Relatives: the set of Pods selected
// by the Service's label selector. Each pod is drillable to its YAML.
// Empty when the Service has no selector (ExternalName, headless without
// selector) — the placeholder line takes over.
func buildServiceRelativeEntries(detail k8s.ResourceDetail) []relativeEntry {
	sl := detail.ServiceRelatives
	if sl == nil || len(sl.Pods) == 0 {
		return nil
	}
	entries := []relativeEntry{
		{section: true, label: fmt.Sprintf("Pods (%d)", len(sl.Pods))},
	}
	for i := range sl.Pods {
		p := &sl.Pods[i]
		entries = append(entries, relativeEntry{
			label: "  " + p.Name,
			value: "pod",
			ref:   p,
		})
	}
	return entries
}

// buildGenericRelativeEntries converts the generic detail.Relatives payload
// (populated by per-kind detailXxx + EnrichRelatives in the k8s layer) into
// relativeEntry rows. Empty input returns nil so the renderer falls back to
// the "no relatives — press Y" placeholder.
func buildGenericRelativeEntries(detail k8s.ResourceDetail) []relativeEntry {
	if len(detail.Relatives) == 0 {
		return nil
	}
	var entries []relativeEntry
	for _, sec := range detail.Relatives {
		if sec.Title != "" {
			entries = append(entries, relativeEntry{section: true, label: sec.Title})
		}
		for i := range sec.Entries {
			row := sec.Entries[i]
			entries = append(entries, relativeEntry{
				label: row.Label,
				value: row.Value,
				ref:   row.Ref,
			})
		}
	}
	return entries
}

// relativesApplicable returns false for kinds where the Relatives tab has nothing
// meaningful to surface. Such kinds drop the tab entirely (handled by
// SetResourceType) instead of showing an empty pane.
func relativesApplicable(rt k8s.ResourceType) bool {
	// Contexts are a local kubeconfig view with no cluster relationships.
	return rt != k8s.ResourceNamespaces && rt != k8s.ResourceContexts
}

// relativesPlaceholderEmpty is shown when the active resource has no refs to
// drill into right now. Every non-Namespace kind has a Relatives builder, so
// this is the only placeholder users will see — empty means "this
// instance genuinely has nothing", not "we haven't written the code yet."
const relativesPlaceholderEmpty = "(no relatives to show — press Y for full YAML)"

func ownerDisplay(ref k8s.RefTarget) string {
	// Short kind label + name. Use the registry display name when available,
	// otherwise fall back to the raw type string.
	kind := string(ref.Type)
	if def := k8s.DefaultRegistry.Get(ref.Type); def != nil {
		kind = strings.TrimSuffix(def.DisplayName, "s")
	}
	return fmt.Sprintf("%s/%s", kind, ref.Name)
}

// renderRelativeEntries turns the entry list into display lines, applying
// styles and adding a cursor highlight on `cursor`. The caller picks the
// placeholder text shown when entries is empty — that's how we surface
// "no relatives to show" vs "kind not yet supported" without renderRelativeEntries
// having to know about resource types. Returns:
//   - lines:           rendered display lines
//   - selectableIdxs:  indices into `entries` that the cursor can land on
//   - cursorLine:      display-line index of the cursor row (-1 if none) —
//     used by the caller to auto-scroll the viewport so
//     the cursor stays visible
func renderRelativeEntries(entries []relativeEntry, cursor int, width int, t *theme.Theme, placeholder string, focused bool) (lines []string, selectableIdxs []int, cursorLine int) {
	cursorLine = -1
	labelStyle := t.DetailLabelStyle()
	valueStyle := t.DetailValueStyle()
	sectionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Sidebar.CategoryFg)).Bold(true)
	drillStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Sidebar.CategoryFg)).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7f849c"))
	cursorRowStyle := t.TableSelectedRowStyle()
	if !focused {
		cursorRowStyle = t.TableUnfocusedSelectedRowStyle()
		// Mirror the sidebar/table treatment: when the panel is
		// unfocused, every non-cursor row collapses to overlay1 grey
		// so the cursor row's lavender chip is the single surviving
		// "remembered position" marker. Section headers stay bold so
		// the structural hierarchy is still legible.
		labelStyle = dimStyle
		valueStyle = dimStyle
		drillStyle = dimStyle
		sectionStyle = dimStyle.Bold(true)
	}

	const labelW = 18
	const indent = "  "

	if len(entries) == 0 {
		lines = append(lines, indent+dimStyle.Render(placeholder))
		return lines, nil, -1
	}

	for i, e := range entries {
		if e.isSelectable() {
			selectableIdxs = append(selectableIdxs, i)
		}
	}

	rowWidth := width
	if rowWidth < 20 {
		rowWidth = 20
	}

	arrowSuffix := " " + relativesDrillArrow

	for i, e := range entries {
		if e.section {
			lines = append(lines, indent+sectionStyle.Render(e.label))
			continue
		}
		isCursor := cursor >= 0 && cursor < len(entries) && cursor == i
		hasArrow := e.ref != nil

		// Nested drillable entries (section children — label has its own
		// "  " indent) render in two-line form: alias/name alone on row 1,
		// resource ref + arrow on row 2 one level deeper. Avoids the
		// "alias  configMap/very-long-name " truncation problem on
		// narrow terminals and matches the user's mental model of a
		// hierarchy (category → alias → target). Cursor highlight spans
		// both rows so it reads as one selectable block.
		if hasArrow && strings.HasPrefix(e.label, "  ") {
			nestedLines, line0 := renderNestedDrillEntry(e, indent, rowWidth, isCursor,
				labelStyle, valueStyle, drillStyle, cursorRowStyle, arrowSuffix)
			if isCursor {
				cursorLine = len(lines) + line0
			}
			lines = append(lines, nestedLines...)
			continue
		}

		// Top-level entries (Owner / Node / ServiceAccount / IngressClass
		// / etc.) keep the single-line "label  value " layout — their
		// labels are short relationship words, splitting them off feels
		// gratuitous. Value still wraps under the value column when
		// needed.
		labelText := e.label
		if len(labelText) < labelW {
			labelText = labelText + strings.Repeat(" ", labelW-len(labelText))
		}
		labelPrefix := "  " + labelText + " "
		labelPrefixW := lipgloss.Width(labelPrefix)
		// Wrap the value alone — DON'T glue the arrow on first. Pre-wrap
		// concat ("value ↘") had wrapPlain trim the space at the break,
		// producing a bare "↘" chunk that lost its drillStyle because
		// the chunk content no longer carried the arrowSuffix marker.
		// Reserving width for the arrow against the LAST chunk keeps the
		// trailing arrow flush + properly styled.
		arrowReserve := 0
		if hasArrow {
			arrowReserve = lipgloss.Width(arrowSuffix)
		}
		valueBudget := rowWidth - labelPrefixW - arrowReserve
		if valueBudget < 10 {
			valueBudget = 10
		}
		chunks := wrapPlain(e.value, valueBudget)
		arrowChunkIdx := -1
		if hasArrow && len(chunks) > 0 {
			arrowChunkIdx = len(chunks) - 1
		}
		contIndent := strings.Repeat(" ", labelPrefixW)

		for ci, chunk := range chunks {
			withArrow := ci == arrowChunkIdx
			if isCursor {
				if ci == 0 {
					cursorLine = len(lines)
				}
				var plain string
				if ci == 0 {
					plain = labelPrefix + chunk
				} else {
					plain = contIndent + chunk
				}
				if withArrow {
					plain += arrowSuffix
				}
				if w := lipgloss.Width(plain); w < rowWidth {
					plain = plain + strings.Repeat(" ", rowWidth-w)
				}
				lines = append(lines, cursorRowStyle.Render(plain))
				continue
			}
			var row string
			if ci == 0 {
				row = "  " + labelStyle.Render(labelText) + " " + valueStyle.Render(chunk)
			} else {
				row = contIndent + valueStyle.Render(chunk)
			}
			if withArrow {
				row += " " + drillStyle.Render(relativesDrillArrow)
			}
			lines = append(lines, row)
		}
	}
	return lines, selectableIdxs, cursorLine
}

// renderNestedDrillEntry produces the 2-line (or 3+ lines if value wraps)
// rendering for a nested drillable entry. Layout:
//
//	alias                          ← e.label (carries its own "  " indent)
//	  resourceType/resourceName    ← e.value + arrow, one level deeper
//
// Returns (rendered lines, index of the first line within those lines —
// used by the caller to set cursorLine = baseOffset + line0).
func renderNestedDrillEntry(
	e relativeEntry, outerIndent string, rowWidth int, isCursor bool,
	labelStyle, valueStyle, drillStyle, cursorRowStyle lipgloss.Style,
	arrowSuffix string,
) (lines []string, line0 int) {
	// Label line: outer indent + e.label (which already has its own "  ").
	labelLinePlain := outerIndent + e.label
	// Value indent: outer + label's internal "  " + one more level "  ".
	valueIndentW := lipgloss.Width(outerIndent) + 2 + 2
	valueIndent := strings.Repeat(" ", valueIndentW)

	valueAndArrow := e.value + arrowSuffix
	valueBudget := rowWidth - valueIndentW
	if valueBudget < 10 {
		valueBudget = 10
	}
	chunks := wrapPlain(valueAndArrow, valueBudget)
	arrowChunkIdx := -1
	if len(chunks) > 0 {
		last := len(chunks) - 1
		if strings.HasSuffix(chunks[last], arrowSuffix) {
			chunks[last] = strings.TrimSuffix(chunks[last], arrowSuffix)
			if chunks[last] == "" && last > 0 {
				chunks = chunks[:last]
				arrowChunkIdx = len(chunks) - 1
			} else {
				arrowChunkIdx = last
			}
		}
	}

	// Label line first.
	if isCursor {
		line0 = 0
		plain := labelLinePlain
		if w := lipgloss.Width(plain); w < rowWidth {
			plain += strings.Repeat(" ", rowWidth-w)
		}
		lines = append(lines, cursorRowStyle.Render(plain))
	} else {
		lines = append(lines, outerIndent+labelStyle.Render(e.label))
	}

	// Value line(s).
	for ci, chunk := range chunks {
		withArrow := ci == arrowChunkIdx
		if isCursor {
			plain := valueIndent + chunk
			if withArrow {
				plain += arrowSuffix
			}
			if w := lipgloss.Width(plain); w < rowWidth {
				plain += strings.Repeat(" ", rowWidth-w)
			}
			lines = append(lines, cursorRowStyle.Render(plain))
			continue
		}
		row := valueIndent + valueStyle.Render(chunk)
		if withArrow {
			row += " " + drillStyle.Render(relativesDrillArrow)
		}
		lines = append(lines, row)
	}
	return lines, line0
}

// nextSelectableCursor returns the next/prev cursor index that lands on a
// selectable entry (skipping section headers + non-drillable rows).
// dir=+1 → next, -1 → prev. Clamps at the ends of the list.
func nextSelectableCursor(entries []relativeEntry, cursor, dir int) int {
	if len(entries) == 0 {
		return -1
	}
	for i := cursor + dir; i >= 0 && i < len(entries); i += dir {
		if entries[i].isSelectable() {
			return i
		}
	}
	return cursor
}

// entryLineCount returns the number of display lines the given
// entry takes when renderRelativeEntries lays it out. Mirror of the
// per-entry branch in renderRelativeEntries — nested drillables
// (label has the "  " indent AND a ref) span 2 lines because the
// resource ref / arrow sits on its own line below the alias; every
// other entry is 1 line.
func entryLineCount(e relativeEntry) int {
	if !e.section && e.ref != nil && strings.HasPrefix(e.label, "  ") {
		return 2
	}
	return 1
}

// entryAtLine returns the index of the entry whose rendered span
// contains the given display-line index, or -1 when `line` is out
// of range. Used by the mouse-click handler to map a cursor click
// back to which Relatives entry the user pointed at.
func entryAtLine(entries []relativeEntry, line int) int {
	if line < 0 {
		return -1
	}
	cur := 0
	for i, e := range entries {
		n := entryLineCount(e)
		if line >= cur && line < cur+n {
			return i
		}
		cur += n
	}
	return -1
}

// firstSelectableCursor returns the first selectable entry index, or -1 if
// the list has no selectable entries.
func firstSelectableCursor(entries []relativeEntry) int {
	for i, e := range entries {
		if e.isSelectable() {
			return i
		}
	}
	return -1
}
