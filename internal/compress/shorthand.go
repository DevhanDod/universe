package compress

import (
	"strconv"
	"strings"
)

type GraphNodeInfo struct {
	ID          string
	Name        string
	Kind        string
	Repo        string
	Package     string
	File        string
	Line        int
	Exported    bool
	Callers     []string
	Callees     []string
	CallerNames []string
	CalleeNames []string
}

func BuildShorthand(nodes []GraphNodeInfo) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString("• ")
		b.WriteString(n.Package)
		b.WriteString(".")
		b.WriteString(n.Name)
		b.WriteString(" [")
		b.WriteString(n.Kind)
		b.WriteString("] (")
		b.WriteString(n.File)
		b.WriteString(":")
		b.WriteString(strconv.Itoa(n.Line))
		b.WriteString(")")
		if len(n.CallerNames) > 0 {
			b.WriteString(" ← ")
			b.WriteString(formatNameList(n.CallerNames, 5))
		}
		if len(n.CalleeNames) > 0 {
			b.WriteString(" → ")
			b.WriteString(formatNameList(n.CalleeNames, 5))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func BuildShorthandCompact(nodes []GraphNodeInfo) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(n.Package)
		b.WriteString(".")
		b.WriteString(n.Name)
		b.WriteString(" ")
		b.WriteString(n.File)
		b.WriteString(":")
		b.WriteString(strconv.Itoa(n.Line))
		if len(n.CallerNames) > 0 {
			b.WriteString(" ←")
			b.WriteString(strconv.Itoa(len(n.CallerNames)))
		}
		if len(n.CalleeNames) > 0 {
			b.WriteString(" →")
			b.WriteString(strconv.Itoa(len(n.CalleeNames)))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatNameList(names []string, maxShow int) string {
	if len(names) <= maxShow {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:maxShow], ", ") + " and " + strconv.Itoa(len(names)-maxShow) + " more"
}
