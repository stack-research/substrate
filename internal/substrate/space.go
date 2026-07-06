package substrate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

const (
	SubstrateDir     = ".substrate"
	SpaceConfigFile  = ".substrate/config.yaml"
	ThreadsDir       = ".substrate/threads"
	ThreadConfigFile = "config.yaml"
)

type ParticipantKind string

const (
	Human ParticipantKind = "human"
	Agent ParticipantKind = "agent"
	Other ParticipantKind = "other"
)

func ParseParticipantKind(raw string) (ParticipantKind, error) {
	switch ParticipantKind(raw) {
	case Human, Agent, Other:
		return ParticipantKind(raw), nil
	default:
		return "", fmt.Errorf("unknown kind %q (human|agent|other)", raw)
	}
}

func (k *ParticipantKind) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("participant kind must be a string")
	}
	parsed, err := ParseParticipantKind(node.Value)
	if err != nil {
		return err
	}
	*k = parsed
	return nil
}

func (k ParticipantKind) MarshalYAML() (any, error) { return string(k), nil }

type Participant struct {
	Name Name            `yaml:"name"`
	Kind ParticipantKind `yaml:"kind"`
}

type SpaceConfig struct {
	Version      int            `yaml:"version"`
	Participants []Participant  `yaml:"participants,omitempty"`
	Extra        map[string]any `yaml:",inline"`
}

type Space struct{ root string }

func InitSpace(root string) (*Space, error) {
	root = filepath.Clean(root)
	configPath := filepath.Join(root, SpaceConfigFile)
	if _, err := os.Stat(configPath); err == nil {
		return nil, fmt.Errorf("%s already exists", configPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := ensureDir(filepath.Join(root, ThreadsDir)); err != nil {
		return nil, err
	}
	space := &Space{root: root}
	if err := space.SaveConfig(SpaceConfig{Version: 1}); err != nil {
		return nil, err
	}
	return space, nil
}

func OpenSpace(root string) (*Space, error) {
	root = filepath.Clean(root)
	info, err := os.Stat(filepath.Join(root, SpaceConfigFile))
	if err != nil || !info.Mode().IsRegular() {
		return nil, &NotASpaceError{Root: root}
	}
	return &Space{root: root}, nil
}

func (s *Space) Root() string         { return s.root }
func (s *Space) SubstrateDir() string { return filepath.Join(s.root, SubstrateDir) }
func (s *Space) ThreadDir(thread Name) string {
	return filepath.Join(s.root, ThreadsDir, thread.ToPathComponent())
}

func (s *Space) Config() (SpaceConfig, error) {
	data, err := os.ReadFile(filepath.Join(s.root, SpaceConfigFile))
	if err != nil {
		return SpaceConfig{}, err
	}
	var cfg SpaceConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return SpaceConfig{}, err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	return cfg, nil
}

func (s *Space) SaveConfig(cfg SpaceConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.root, SpaceConfigFile), data)
}

func (s *Space) AddParticipant(name Name, kind ParticipantKind) error {
	return withFileLock(filepath.Join(s.SubstrateDir(), ".space.lock"), func() error {
		cfg, err := s.Config()
		if err != nil {
			return err
		}
		for _, p := range cfg.Participants {
			if p.Name == name {
				return &DuplicateParticipantError{Name: name}
			}
		}
		cfg.Participants = append(cfg.Participants, Participant{Name: name, Kind: kind})
		return s.SaveConfig(cfg)
	})
}

func (s *Space) Participant(name Name) (Participant, error) {
	cfg, err := s.Config()
	if err != nil {
		return Participant{}, err
	}
	for _, p := range cfg.Participants {
		if p.Name == name {
			return p, nil
		}
	}
	return Participant{}, &UnknownParticipantError{Name: name.String()}
}

func (s *Space) ListThreads() ([]Name, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, ThreadsDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	threads := make([]Name, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if info, err := os.Stat(filepath.Join(s.root, ThreadsDir, entry.Name(), ThreadConfigFile)); err != nil || !info.Mode().IsRegular() {
			continue
		}
		name, err := NameFromPathComponent(entry.Name())
		if err == nil {
			threads = append(threads, name)
		}
	}
	sort.Slice(threads, func(i, j int) bool { return threads[i] < threads[j] })
	return threads, nil
}
