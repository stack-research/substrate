package substrate

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	IdentityFile     = "identity.yaml"
	SpacesFile       = "spaces.yaml"
	ParticipantsFile = "participants.yaml"
	AgentsFile       = "agents.yaml"
)

func HomeDir() string {
	if home := os.Getenv("SUBSTRATE_HOME"); home != "" {
		return filepath.Clean(home)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".substrate")
}

type identityFile struct {
	Name Name `yaml:"name"`
}

func LoadIdentity() (Name, bool) {
	data, err := os.ReadFile(filepath.Join(HomeDir(), IdentityFile))
	if err != nil {
		return "", false
	}
	var identity identityFile
	if yaml.Unmarshal(data, &identity) != nil {
		return "", false
	}
	return identity.Name, true
}

func SaveIdentity(name Name) error {
	if HomeDir() == "" {
		return nil
	}
	data, err := yaml.Marshal(identityFile{Name: name})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(HomeDir(), IdentityFile), data)
}

type SpacesRegistry struct {
	Spaces map[string]string `yaml:"spaces,omitempty"`
	Extra  map[string]any    `yaml:",inline"`
}

func LoadSpacesRegistry(path string) (SpacesRegistry, error) {
	if path == "" {
		path = filepath.Join(HomeDir(), SpacesFile)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return SpacesRegistry{Spaces: make(map[string]string)}, nil
	}
	if err != nil {
		return SpacesRegistry{}, err
	}
	var registry SpacesRegistry
	if err := yaml.Unmarshal(data, &registry); err != nil {
		return SpacesRegistry{}, err
	}
	if registry.Spaces == nil {
		registry.Spaces = make(map[string]string)
	}
	return registry, nil
}

func (r SpacesRegistry) Save(path string) error {
	if path == "" {
		path = filepath.Join(HomeDir(), SpacesFile)
	}
	data, err := yaml.Marshal(r)
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

func (r *SpacesRegistry) Add(label, path string) string {
	for existing, existingPath := range r.Spaces {
		if filepath.Clean(existingPath) == filepath.Clean(path) {
			return existing
		}
	}
	base := label
	for i := 2; ; i++ {
		if _, exists := r.Spaces[label]; !exists {
			r.Spaces[label] = path
			return label
		}
		label = base + "-" + itoa(i)
	}
}

func LabelFor(path string) string {
	raw := strings.ToLower(filepath.Base(filepath.Clean(path)))
	var out strings.Builder
	for _, r := range raw {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			out.WriteRune(r)
		} else if out.Len() > 0 && !strings.HasSuffix(out.String(), "-") {
			out.WriteByte('-')
		}
	}
	label := strings.TrimSuffix(out.String(), "-")
	if label == "" {
		return "space"
	}
	return label
}

type participantTemplate struct {
	Participants []Participant `yaml:"participants,omitempty"`
}

func LoadParticipantTemplate() ([]Participant, error) {
	data, err := os.ReadFile(filepath.Join(HomeDir(), ParticipantsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var template participantTemplate
	if err := yaml.Unmarshal(data, &template); err != nil {
		return nil, err
	}
	return template.Participants, nil
}

type agentsFile struct {
	Agents map[Name]struct {
		Run string `yaml:"run"`
	} `yaml:"agents,omitempty"`
}

func LoadAgentCommand(name Name) (string, bool, error) {
	data, err := os.ReadFile(filepath.Join(HomeDir(), AgentsFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var agents agentsFile
	if err := yaml.Unmarshal(data, &agents); err != nil {
		return "", false, err
	}
	entry, ok := agents.Agents[name]
	return entry.Run, ok, nil
}

type BootstrapResult struct {
	Label  string
	Seeded []Name
}

func BootstrapSpace(root string, human *Name) (BootstrapResult, error) {
	space, err := InitSpace(root)
	if err != nil {
		return BootstrapResult{}, err
	}
	crew, err := LoadParticipantTemplate()
	if err != nil {
		return BootstrapResult{}, err
	}
	seeded := make([]Name, 0, len(crew)+1)
	for _, participant := range crew {
		if err := space.AddParticipant(participant.Name, participant.Kind); err != nil {
			var duplicate *DuplicateParticipantError
			if !errors.As(err, &duplicate) {
				return BootstrapResult{}, err
			}
			continue
		}
		seeded = append(seeded, participant.Name)
	}
	if human != nil {
		if _, err := space.Participant(*human); err != nil {
			if err := space.AddParticipant(*human, Human); err != nil {
				return BootstrapResult{}, err
			}
			seeded = append(seeded, *human)
		}
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return BootstrapResult{}, err
	}
	registry, err := LoadSpacesRegistry("")
	if err != nil {
		return BootstrapResult{}, err
	}
	label := registry.Add(LabelFor(absolute), absolute)
	if err := registry.Save(""); err != nil {
		return BootstrapResult{}, err
	}
	sort.Slice(seeded, func(i, j int) bool { return seeded[i] < seeded[j] })
	return BootstrapResult{Label: label, Seeded: seeded}, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
