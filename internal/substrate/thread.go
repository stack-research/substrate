package substrate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type ThreadStatus string

const (
	Active ThreadStatus = "active"
	Ended  ThreadStatus = "ended"
)

func (s *ThreadStatus) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return errors.New("thread status must be a string")
	}
	switch ThreadStatus(node.Value) {
	case "", Active:
		*s = Active
	case Ended:
		*s = Ended
	default:
		return fmt.Errorf("unknown thread status %q (active|ended)", node.Value)
	}
	return nil
}

func (s ThreadStatus) MarshalYAML() (any, error) { return string(s), nil }

type ThreadConfig struct {
	Topic     string          `yaml:"topic"`
	CreatedAt time.Time       `yaml:"created_at"`
	Moderator Name            `yaml:"moderator"`
	TurnOrder []Name          `yaml:"turn_order"`
	NextIndex int             `yaml:"next_index,omitempty"`
	Quieted   map[Name]uint32 `yaml:"quieted,omitempty"`
	Status    ThreadStatus    `yaml:"status,omitempty"`
	Extra     map[string]any  `yaml:",inline"`
}

func LoadThread(space *Space, thread Name) (ThreadConfig, error) {
	data, err := os.ReadFile(filepath.Join(space.ThreadDir(thread), ThreadConfigFile))
	if errors.Is(err, os.ErrNotExist) {
		return ThreadConfig{}, &UnknownThreadError{Name: thread.String()}
	}
	if err != nil {
		return ThreadConfig{}, err
	}
	var cfg ThreadConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ThreadConfig{}, err
	}
	if cfg.Status == "" {
		cfg.Status = Active
	}
	if cfg.Quieted == nil {
		cfg.Quieted = make(map[Name]uint32)
	}
	if len(cfg.TurnOrder) == 0 {
		return ThreadConfig{}, ErrTooFewParticipants
	}
	if cfg.NextIndex < 0 || cfg.NextIndex >= len(cfg.TurnOrder) {
		cfg.NextIndex = len(cfg.TurnOrder) - 1
	}
	return cfg, nil
}

func SaveThread(space *Space, thread Name, cfg ThreadConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(space.ThreadDir(thread), ThreadConfigFile), data)
}

func (c ThreadConfig) Current() Name {
	idx := c.NextIndex
	if idx < 0 || idx >= len(c.TurnOrder) {
		idx = len(c.TurnOrder) - 1
	}
	return c.TurnOrder[idx]
}

func (c ThreadConfig) Paused() bool { return c.Status == Active && c.Current() == c.Moderator }

func CreateThread(space *Space, thread Name, topic string, moderator Name, turns []Name) (ThreadConfig, error) {
	var created ThreadConfig
	err := withFileLock(filepath.Join(space.SubstrateDir(), ".space.lock"), func() error {
		dir := space.ThreadDir(thread)
		if _, err := os.Stat(filepath.Join(dir, ThreadConfigFile)); err == nil {
			return &ThreadExistsError{Name: thread}
		}
		if _, err := space.Participant(moderator); err != nil {
			return err
		}
		order := []Name{moderator}
		for _, name := range turns {
			if _, err := space.Participant(name); err != nil {
				return err
			}
			if name != moderator && !containsName(order, name) {
				order = append(order, name)
			}
		}
		if len(order) < 2 {
			return ErrTooFewParticipants
		}
		created = ThreadConfig{
			Topic: topic, CreatedAt: time.Now().UTC(), Moderator: moderator,
			TurnOrder: order, Quieted: make(map[Name]uint32), Status: Active,
		}
		if err := ensureDir(dir); err != nil {
			return err
		}
		return SaveThread(space, thread, created)
	})
	return created, err
}

func containsName(names []Name, target Name) bool {
	for _, name := range names {
		if name == target {
			return true
		}
	}
	return false
}
