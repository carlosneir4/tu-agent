package reconcile

import (
	"fmt"
	"strings"
)

// RenderPlan renders a dry-run reconcile Plan to deterministic, byte-stable plan
// text. It is pure: identical Plans render to identical text. It is the single
// renderer both `memory reconcile` (CLI) and `mem_reconcile` (MCP) call, so the
// two surfaces emit byte-identical plans by construction (§10 parity). For each
// orphan the text names at least the record's topic key and its bound (parsed)
// label; candidate clusters, when present, are listed beneath it. Orphan and
// candidate ordering is fixed by PlanFrom, so no sorting is needed here.
func RenderPlan(p Plan) string {
	if len(p.Orphans) == 0 {
		return "Reconcile plan: no orphaned skill records; memory is reconciled.\n"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Reconcile plan: %d orphaned skill record(s)\n", len(p.Orphans))
	for _, o := range p.Orphans {
		fmt.Fprintf(&sb, "- %s (bound label: %s)\n", o.Topic, o.Label)
		if len(o.Candidates) == 0 {
			sb.WriteString("    no candidate cluster to re-point to\n")
			continue
		}
		for _, c := range o.Candidates {
			fmt.Fprintf(&sb, "    -> %s (overlap %.2f)\n", c.Label, c.Overlap)
		}
	}
	return sb.String()
}

// RenderApplyResult renders an ApplyResult to deterministic, byte-stable text. It
// is pure and a sibling of RenderPlan: identical results render identically.
// Ordering within each category is the slice order as given — the core sorts
// upstream, so the renderer does NOT re-sort. The header counts only
// rebound/renamed/divergence; removed, would-remove, and skipped surface as
// their own lines. An empty result (nothing in any category, including
// WouldRemove) renders a single "nothing to apply" line.
func RenderApplyResult(res ApplyResult) string {
	if len(res.Rebound) == 0 && len(res.Renamed) == 0 && len(res.Divergent) == 0 &&
		len(res.Removed) == 0 && len(res.WouldRemove) == 0 && len(res.Skipped) == 0 {
		return "Applied reconcile: nothing to apply; memory already reconciled.\n"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Applied reconcile: %d rebound, %d renamed, %d divergence\n",
		len(res.Rebound), len(res.Renamed), len(res.Divergent))
	for _, r := range res.Rebound {
		fmt.Fprintf(&sb, "- rebound  %s  (%s -> %s)\n", r.Topic, r.OldLabel, r.NewLabel)
	}
	for _, r := range res.Renamed {
		fmt.Fprintf(&sb, "- renamed  %s -> %s  (label: %s)\n", r.OldTopic, r.NewTopic, r.Label)
	}
	for _, d := range res.Divergent {
		fmt.Fprintf(&sb, "- diverge  %s  (hand-written SKILL.md left intact)\n", d.Topic)
	}
	for _, name := range res.Removed {
		fmt.Fprintf(&sb, "- removed  skill/%s  (folder deleted)\n", name)
	}
	if len(res.WouldRemove) > 0 {
		fmt.Fprintf(&sb, "%d folder(s) would remove (use --prune-folders): %s\n",
			len(res.WouldRemove), strings.Join(res.WouldRemove, ", "))
	}
	if len(res.Skipped) > 0 {
		fmt.Fprintf(&sb, "%d orphan left untouched (ambiguous): %s\n",
			len(res.Skipped), strings.Join(res.Skipped, ", "))
	}
	return sb.String()
}
