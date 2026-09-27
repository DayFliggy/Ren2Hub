// Package billingunits defines the units accepted by prices and usage schemas.
// Scale expresses a unit in its dimension's canonical unit.
package billingunits

type Definition struct {
	Dimension string
	Scale     string
}

var definitions = map[string]Definition{
	"token": {"token", "1"}, "million_tokens": {"token", "1000000"},
	"call": {"call", "1"}, "thousand_calls": {"call", "1000"},
	"image":  {"image", "1"},
	"second": {"time", "1"}, "minute": {"time", "60"}, "hour": {"time", "3600"},
	"character": {"character", "1"}, "million_characters": {"character", "1000000"},
	"credit": {"credit", "1"}, "count": {"count", "1"},
}

func Lookup(unit string) (Definition, bool) {
	definition, ok := definitions[unit]
	return definition, ok
}
