// Package suppress finds inline "# vlotpipe: ignore" comments in a
// parsed YAML tree. Shared by every platform parser (GitHub Actions,
// Azure Pipelines, ...) so the suppression comment syntax stays
// identical regardless of which CI system's file it appears in.
package suppress

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// pattern matches an inline suppression comment, e.g.
// "# vlotpipe: ignore[SEC007]" or a bare "# vlotpipe: ignore" that
// suppresses every code on its line. Comma-separated codes are allowed:
// "# vlotpipe: ignore[SEC007, SEC008]".
var pattern = regexp.MustCompile(`(?i)vlotpipe:\s*ignore(?:\[([^\]]*)\])?`)

// Parse walks the whole node tree and collects every inline
// "# vlotpipe: ignore" comment into a line -> codes map. A present key
// with an empty slice means a bare "ignore" with no code list, which
// suppresses every code on that line. YAML comments attach to whichever
// node sits on their line (usually a scalar value), so this has to walk
// everything rather than look at known fields.
func Parse(root *yaml.Node) map[int][]string {
	out := map[int][]string{}
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		for _, comment := range []string{n.LineComment, n.HeadComment, n.FootComment} {
			m := pattern.FindStringSubmatch(comment)
			if m == nil {
				continue
			}
			if existing, ok := out[n.Line]; ok && len(existing) == 0 {
				continue // already suppressing everything on this line
			}
			if m[1] == "" {
				out[n.Line] = []string{} // bare "ignore": suppress everything
				continue
			}
			for _, c := range strings.Split(m[1], ",") {
				if c = strings.TrimSpace(c); c != "" {
					out[n.Line] = append(out[n.Line], c)
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(root)
	return out
}
