package cognition

import "strings"

// escapeNQL escapes double quotes in a string value before embedding it
// in an NQL query via fmt.Sprintf. This prevents NQL injection when
// user-provided strings contain quote characters or NQL syntax.
func escapeNQL(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
