package substrate

import (
	"fmt"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const MaxNameLen = 64

// Name is a validated participant, thread, or space label. A canonical name
// may contain '/', but ToPathComponent always encodes it as one safe leaf.
type Name string

func ParseName(raw string) (Name, error) {
	fail := func(reason string) (Name, error) {
		return "", &InvalidNameError{Name: raw, Reason: reason}
	}
	if raw == "" {
		return fail("empty")
	}
	if len(raw) > MaxNameLen {
		return fail("longer than 64 chars")
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return fail("control characters not allowed")
		}
	}
	first := raw[0]
	if first == '.' {
		return fail("must not start with '.'")
	}
	if !isLowerOrDigit(first) {
		return fail("must start with a lowercase letter or digit")
	}
	if strings.HasSuffix(raw, "/") || strings.HasSuffix(raw, ".") || strings.HasSuffix(raw, "-") {
		return fail("must not end with '/', '.', or '-'")
	}
	if strings.Contains(raw, "..") {
		return fail("must not contain '..'")
	}
	if strings.Contains(raw, "//") {
		return fail("must not contain '//'")
	}
	if strings.Contains(raw, "__") {
		return fail("must not contain '__'")
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !isLowerOrDigit(c) && c != '-' && c != '/' && c != '.' {
			return fail("only lowercase letters, digits, '-', '/', and '.' allowed")
		}
	}
	return Name(raw), nil
}

// ParseNames parses a list of raw values into names. Each value may itself be
// a comma-separated list; surrounding whitespace and empty items are ignored.
func ParseNames(values []string) ([]Name, error) {
	var names []Name
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			name, err := ParseName(raw)
			if err != nil {
				return nil, err
			}
			names = append(names, name)
		}
	}
	return names, nil
}

// JoinNames renders names as a single string with the given separator.
func JoinNames(names []Name, separator string) string {
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = name.String()
	}
	return strings.Join(parts, separator)
}

// MustName parses raw and panics on failure; for tests and constants.
func MustName(raw string) Name {
	name, err := ParseName(raw)
	if err != nil {
		panic(err)
	}
	return name
}

func isLowerOrDigit(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

func (n Name) String() string { return string(n) }

func (n Name) ToPathComponent() string { return strings.ReplaceAll(string(n), "/", "%2F") }

func NameFromPathComponent(component string) (Name, error) {
	if strings.Contains(component, "/") {
		return "", &InvalidNameError{Name: component, Reason: "path component must not contain '/'"}
	}
	var out strings.Builder
	for i := 0; i < len(component); i++ {
		if component[i] != '%' {
			out.WriteByte(component[i])
			continue
		}
		if i+2 >= len(component) || component[i+1:i+3] != "2F" {
			return "", &InvalidNameError{Name: component, Reason: "invalid filesystem escape"}
		}
		out.WriteByte('/')
		i += 2
	}
	return ParseName(out.String())
}

func (n *Name) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("name must be a string")
	}
	parsed, err := ParseName(node.Value)
	if err != nil {
		return err
	}
	*n = parsed
	return nil
}

func (n Name) MarshalYAML() (any, error) { return string(n), nil }
