package mcpserver

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stack-research/substrate/internal/substrate"
)

type SpaceSource struct {
	Paths        []string
	RegistryFile string
}

func (s SpaceSource) Describe() string {
	if len(s.Paths) > 0 {
		return "pinned: " + strings.Join(s.Paths, ", ")
	}
	path := s.RegistryFile
	if path == "" {
		path = filepath.Join(substrate.HomeDir(), substrate.SpacesFile)
	}
	return "registry: " + path
}

type LabeledSpace struct {
	Label string
	Space *substrate.Space
}

type SpaceSet struct{ Spaces []LabeledSpace }

func (s SpaceSource) Load() (SpaceSet, error) {
	paths := make(map[string]string)
	if len(s.Paths) > 0 {
		for _, path := range s.Paths {
			label := substrate.LabelFor(path)
			if previous, exists := paths[label]; exists && filepath.Clean(previous) != filepath.Clean(path) {
				return SpaceSet{}, fmt.Errorf("duplicate space label %q", label)
			}
			paths[label] = path
		}
	} else {
		registry, err := substrate.LoadSpacesRegistry(s.RegistryFile)
		if err != nil {
			return SpaceSet{}, err
		}
		paths = registry.Spaces
	}
	labels := make([]string, 0, len(paths))
	for label := range paths {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	set := SpaceSet{}
	for _, label := range labels {
		if _, err := substrate.ParseName(label); err != nil {
			return SpaceSet{}, fmt.Errorf("invalid space label %q: %w", label, err)
		}
		space, err := substrate.OpenSpace(paths[label])
		if err != nil {
			continue
		}
		set.Spaces = append(set.Spaces, LabeledSpace{Label: label, Space: space})
	}
	return set, nil
}

func (s SpaceSet) Labels() []string {
	labels := make([]string, len(s.Spaces))
	for i, space := range s.Spaces {
		labels[i] = space.Label
	}
	return labels
}

func (s SpaceSet) Resolve(label string) (*substrate.Space, error) {
	if label != "" {
		for _, space := range s.Spaces {
			if space.Label == label {
				return space.Space, nil
			}
		}
		return nil, fmt.Errorf("unknown space %q — configured spaces: %s", label, strings.Join(s.Labels(), ", "))
	}
	switch len(s.Spaces) {
	case 0:
		return nil, fmt.Errorf("no spaces exist yet — a moderator creates one with `substrate init`")
	case 1:
		return s.Spaces[0].Space, nil
	default:
		return nil, fmt.Errorf("several spaces are configured — pass `space`: %s", strings.Join(s.Labels(), ", "))
	}
}
