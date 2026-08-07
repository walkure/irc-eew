package eewview

import (
	"fmt"
	"html/template"
)

// link builds a self-contained, safely-escaped `<a href="...">label</a>`
// HTML fragment. Both href and label are escaped individually before
// assembly, so this is safe to use even if either ever becomes
// attacker-influenced in the future (today both are always either
// validated integers or fixed-vocabulary lookup-table text).
func link(href, label string) template.HTML {
	return template.HTML(fmt.Sprintf(`<a href="%s">%s</a>`, template.HTMLEscapeString(href), template.HTMLEscapeString(label)))
}

// escaped wraps a plain-text value that's being assembled into a larger
// template.HTML fragment by hand, escaping it first.
func escaped(s string) string {
	return template.HTMLEscapeString(s)
}
