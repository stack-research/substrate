// Package cli implements the substrate command tree: space bootstrap, thread
// operations, the TUI entry point, and the watch/attend/proxy daemons.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/stack-research/substrate/internal/proxy"
	"github.com/stack-research/substrate/internal/substrate"
	"github.com/stack-research/substrate/internal/ui"
	"github.com/stack-research/substrate/internal/version"
	"github.com/stack-research/substrate/internal/watcher"
)

type App struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
	getenv func(string) string
}

func New() *App {
	return &App{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr, getenv: os.Getenv}
}

func (a *App) Root() *cobra.Command {
	defaultSpace := a.getenv("SUBSTRATE_SPACE")
	if defaultSpace == "" {
		defaultSpace = "."
	}
	var rootPath string
	root := &cobra.Command{
		Use:           "substrate",
		Short:         "Turn-based group conversations between humans, agents, and anything else",
		Version:       version.Version + " (" + version.Runtime + ")",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, _ []string) error { return a.runTUI(rootPath, "") },
	}
	root.SetIn(a.In)
	root.SetOut(a.Out)
	root.SetErr(a.ErrOut)
	root.PersistentFlags().StringVar(&rootPath, "space", defaultSpace, "space directory")
	root.AddCommand(
		a.initCommand(&rootPath), a.addCommand(&rootPath), a.newCommand(&rootPath),
		a.statusCommand(&rootPath), a.writeCommand(&rootPath), a.readCommand(&rootPath),
		a.briefCommand(&rootPath), a.serveCommand(&rootPath), a.spacesCommand(),
		a.attendCommand(), a.watchCommand(&rootPath), a.tuiCommand(&rootPath),
		a.moderateCommand(&rootPath), a.doctorCommand(&rootPath), a.versionCommand(),
	)
	return root
}

func (a *App) initCommand(rootPath *string) *cobra.Command {
	return &cobra.Command{Use: "init", Short: "Create a new space", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		var human *substrate.Name
		if name, ok := substrate.LoadIdentity(); ok {
			human = &name
		}
		result, err := substrate.BootstrapSpace(*rootPath, human)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "initialized space %q at %s\n", result.Label, *rootPath)
		if len(result.Seeded) > 0 {
			fmt.Fprintf(a.Out, "seeded participants: %s\n", substrate.JoinNames(result.Seeded, ", "))
		}
		fmt.Fprintln(a.Out, "registered in the machine registry — agents can already see it")
		return nil
	}}
}

func (a *App) addCommand(rootPath *string) *cobra.Command {
	var kind string
	cmd := &cobra.Command{Use: "add <name>", Short: "Register a participant", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		name, err := substrate.ParseName(args[0])
		if err != nil {
			return err
		}
		parsedKind, err := substrate.ParseParticipantKind(kind)
		if err != nil {
			return err
		}
		space, err := substrate.OpenSpace(*rootPath)
		if err != nil {
			return err
		}
		if err := space.AddParticipant(name, parsedKind); err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "added %s %q\n", parsedKind, name)
		return nil
	}}
	cmd.Flags().StringVar(&kind, "kind", "", "participant kind: human, agent, or other")
	_ = cmd.MarkFlagRequired("kind")
	return cmd
}

func (a *App) newCommand(rootPath *string) *cobra.Command {
	var topic, moderator string
	var turns []string
	cmd := &cobra.Command{Use: "new <name>", Short: "Create a thread", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		space, err := substrate.OpenSpace(*rootPath)
		if err != nil {
			return err
		}
		thread, err := substrate.ParseName(args[0])
		if err != nil {
			return err
		}
		mod, err := substrate.ParseName(moderator)
		if err != nil {
			return err
		}
		order, err := substrate.ParseNames(turns)
		if err != nil {
			return err
		}
		cfg, err := substrate.CreateThread(space, thread, topic, mod, order)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "created %q — topic: %s\n", thread, topic)
		fmt.Fprintf(a.Out, "turns: %s (moderator first — your opening entry sets the instructions)\n", substrate.JoinNames(cfg.TurnOrder, " → "))
		return nil
	}}
	cmd.Flags().StringVar(&topic, "topic", "", "thread topic")
	cmd.Flags().StringVar(&moderator, "moderator", "", "registered moderator")
	cmd.Flags().StringSliceVar(&turns, "turns", nil, "speaking order, comma-separated")
	_ = cmd.MarkFlagRequired("topic")
	_ = cmd.MarkFlagRequired("moderator")
	_ = cmd.MarkFlagRequired("turns")
	return cmd
}

func (a *App) statusCommand(rootPath *string) *cobra.Command {
	return &cobra.Command{Use: "status [thread]", Short: "Show space or thread status", Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		space, err := substrate.OpenSpace(*rootPath)
		if err != nil {
			return err
		}
		if len(args) == 1 {
			name, err := substrate.ParseName(args[0])
			if err != nil {
				return err
			}
			return a.printThreadStatus(space, name)
		}
		cfg, err := space.Config()
		if err != nil {
			return err
		}
		fmt.Fprintln(a.Out, "participants:")
		for _, p := range cfg.Participants {
			fmt.Fprintf(a.Out, "  %s (%s)\n", p.Name, p.Kind)
		}
		threads, err := space.ListThreads()
		if err != nil {
			return err
		}
		if len(threads) == 0 {
			fmt.Fprintln(a.Out, "no threads yet — try `substrate new`")
			return nil
		}
		fmt.Fprintln(a.Out, "threads:")
		for _, thread := range threads {
			status, err := substrate.GetTurnStatus(space, thread)
			if err != nil {
				fmt.Fprintf(a.Out, "  %s — unreadable: %v\n", thread, err)
				continue
			}
			paused := ""
			if status.Paused {
				paused = " (paused on moderator)"
			}
			fmt.Fprintf(a.Out, "  %s — %s, turn: %s%s — %s\n", thread, status.Status.Title(), status.Current, paused, status.Topic)
		}
		return nil
	}}
}

func (a *App) printThreadStatus(space *substrate.Space, thread substrate.Name) error {
	status, err := substrate.GetTurnStatus(space, thread)
	if err != nil {
		return err
	}
	_, lines, err := substrate.ReadTranscript(space, thread, substrate.Window{})
	if err != nil {
		return err
	}
	paused := ""
	if status.Paused {
		paused = " (moderator — paused)"
	}
	order := make([]string, 0, len(status.TurnOrder))
	for _, name := range status.TurnOrder {
		label := name.String()
		if name == status.Moderator {
			label += " [mod]"
		}
		if n := status.Quieted[name]; n > 0 {
			label += fmt.Sprintf(" [quiet %d]", n)
		}
		if name == status.Current {
			label = "*" + label
		}
		order = append(order, label)
	}
	fmt.Fprintf(a.Out, "thread: %s\ntopic: %s\nstatus: %s\nturn: %s%s\norder: %s\ntranscript lines: %d\n", thread, status.Topic, status.Status.Title(), status.Current, paused, strings.Join(order, " → "), lines)
	return nil
}

func (a *App) writeCommand(rootPath *string) *cobra.Command {
	var author, message, file string
	var stdin bool
	cmd := &cobra.Command{Use: "write <thread>", Short: "Write one turn-enforced entry", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		sources := 0
		if cmd.Flags().Changed("message") {
			sources++
		}
		if stdin {
			sources++
		}
		if file != "" {
			sources++
		}
		if sources != 1 {
			return errors.New("pass exactly one of -m, --stdin, or --file")
		}
		content := message
		var err error
		if stdin {
			data, readErr := io.ReadAll(a.In)
			err = readErr
			content = string(data)
		}
		if file != "" {
			data, readErr := os.ReadFile(file)
			err = readErr
			content = string(data)
		}
		if err != nil {
			return err
		}
		space, err := substrate.OpenSpace(*rootPath)
		if err != nil {
			return err
		}
		thread, err := substrate.ParseName(args[0])
		if err != nil {
			return err
		}
		name, err := substrate.ParseName(author)
		if err != nil {
			return err
		}
		written, err := substrate.WriteEntry(space, thread, name, content)
		if err != nil {
			return err
		}
		noOp := ""
		if written.NoOp {
			noOp = " (no-op)"
		}
		paused := ""
		if written.Paused {
			paused = " (moderator — paused)"
		}
		fmt.Fprintf(a.Out, "recorded %s%s — next: %s%s\n", written.Filename, noOp, written.Next, paused)
		return nil
	}}
	cmd.Flags().StringVar(&author, "as", "", "registered author")
	cmd.Flags().StringVarP(&message, "message", "m", "", "entry text")
	cmd.Flags().BoolVar(&stdin, "stdin", false, "read entry from stdin")
	cmd.Flags().StringVar(&file, "file", "", "read entry from file")
	_ = cmd.MarkFlagRequired("as")
	return cmd
}

func (a *App) readCommand(rootPath *string) *cobra.Command {
	var last, from uint64
	cmd := &cobra.Command{Use: "read <thread>", Short: "Read a clean transcript", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("last") && cmd.Flags().Changed("from") {
			return errors.New("--last and --from are mutually exclusive")
		}
		space, err := substrate.OpenSpace(*rootPath)
		if err != nil {
			return err
		}
		thread, err := substrate.ParseName(args[0])
		if err != nil {
			return err
		}
		window := substrate.Window{}
		if cmd.Flags().Changed("last") {
			value := safeInt(last)
			window.LastN = &value
		}
		if cmd.Flags().Changed("from") {
			value := safeInt(from)
			window.FromLine = &value
		}
		text, _, err := substrate.ReadTranscript(space, thread, window)
		if err != nil {
			return err
		}
		fmt.Fprintln(a.Out, text)
		return nil
	}}
	cmd.Flags().Uint64Var(&last, "last", 0, "last N transcript lines")
	cmd.Flags().Uint64Var(&from, "from", 0, "read from this 1-based line")
	return cmd
}

func (a *App) briefCommand(rootPath *string) *cobra.Command {
	var forName string
	cmd := &cobra.Command{Use: "brief <thread>", Short: "Print a courier packet for a web-only participant", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		space, err := substrate.OpenSpace(*rootPath)
		if err != nil {
			return err
		}
		thread, err := substrate.ParseName(args[0])
		if err != nil {
			return err
		}
		var participant *substrate.Name
		if forName != "" {
			parsed, err := substrate.ParseName(forName)
			if err != nil {
				return err
			}
			participant = &parsed
		}
		text, err := proxy.BriefText(space, thread, participant, substrate.Window{})
		if err != nil {
			return err
		}
		fmt.Fprint(a.Out, text)
		if participant != nil {
			fmt.Fprintln(a.Out, "\nReply with plain ASCII markdown addressed to the whole thread. Reply exactly 'pass' if you have nothing to add.")
		}
		return nil
	}}
	cmd.Flags().StringVar(&forName, "for", "", "address packet to participant")
	return cmd
}

func (a *App) serveCommand(rootPath *string) *cobra.Command {
	var port int
	var proxies []string
	var fixedKey string
	cmd := &cobra.Command{Use: "serve", Short: "Serve URL-only participants on localhost", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if len(proxies) == 0 {
			return errors.New("at least one --proxy is required")
		}
		if fixedKey != "" && len(proxies) != 1 {
			return errors.New("--key only makes sense with exactly one --proxy")
		}
		space, err := substrate.OpenSpace(*rootPath)
		if err != nil {
			return err
		}
		participants := make([]proxy.Participant, 0, len(proxies))
		for _, raw := range proxies {
			name, err := substrate.ParseName(raw)
			if err != nil {
				return err
			}
			if _, err := space.Participant(name); err != nil {
				return err
			}
			key := fixedKey
			if key == "" {
				key, err = proxy.RandomKey()
				if err != nil {
					return err
				}
			}
			participants = append(participants, proxy.Participant{Name: name, Key: key})
		}
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return err
		}
		defer listener.Close()
		fmt.Fprintf(a.Out, "listening on http://%s\nspace: %s\n", listener.Addr(), space.Root())
		for _, participant := range participants {
			fmt.Fprintf(a.Out, "  %s: read  http://%s/t/THREAD?key=%s&from=1&nonce=NONCE\n", participant.Name, listener.Addr(), participant.Key)
			fmt.Fprintf(a.Out, "  %s  write http://%s/t/THREAD/write?key=%s&turn=N&from=LINE&nonce=NONCE&b64=REPLY\n", strings.Repeat(" ", len(participant.Name)), listener.Addr(), participant.Key)
		}
		fmt.Fprintln(a.Out, "replace NONCE with a different random ASCII value before every request")
		server := &http.Server{Handler: proxy.NewHandler(space, participants), ReadHeaderTimeout: 10 * time.Second}
		go func() {
			<-cmd.Context().Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		}()
		err = server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}}
	cmd.Flags().IntVar(&port, "port", 7171, "localhost port")
	cmd.Flags().StringArrayVar(&proxies, "proxy", nil, "participant allowed through this server (repeatable)")
	cmd.Flags().StringVar(&fixedKey, "key", "", "fixed capability key for one proxy")
	return cmd
}

func (a *App) spacesCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "spaces", Short: "Manage the machine-level space registry"}
	cmd.AddCommand(
		&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
			registry, err := substrate.LoadSpacesRegistry("")
			if err != nil {
				return err
			}
			if len(registry.Spaces) == 0 {
				fmt.Fprintln(a.Out, "no spaces registered — `substrate init` in a project, or `substrate spaces add`")
				return nil
			}
			labels := sortedKeys(registry.Spaces)
			for _, label := range labels {
				path := registry.Spaces[label]
				state := "unopenable"
				if space, err := substrate.OpenSpace(path); err == nil {
					threads, _ := space.ListThreads()
					state = fmt.Sprintf("%d thread(s)", len(threads))
				}
				fmt.Fprintf(a.Out, "%-20s %s (%s)\n", label, path, state)
			}
			return nil
		}},
		a.spacesAddCommand(), a.spacesRemoveCommand(),
	)
	return cmd
}

func (a *App) spacesAddCommand() *cobra.Command {
	var label string
	cmd := &cobra.Command{Use: "add <path>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		if _, err := substrate.OpenSpace(args[0]); err != nil {
			return err
		}
		absolute, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}
		registry, err := substrate.LoadSpacesRegistry("")
		if err != nil {
			return err
		}
		if label == "" {
			label = substrate.LabelFor(absolute)
		} else if _, err := substrate.ParseName(label); err != nil {
			return err
		}
		actual := registry.Add(label, absolute)
		if err := registry.Save(""); err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "registered %q -> %s\n", actual, absolute)
		return nil
	}}
	cmd.Flags().StringVar(&label, "label", "", "registry label")
	return cmd
}

func (a *App) spacesRemoveCommand() *cobra.Command {
	return &cobra.Command{Use: "remove <label>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		registry, err := substrate.LoadSpacesRegistry("")
		if err != nil {
			return err
		}
		if _, ok := registry.Spaces[args[0]]; !ok {
			return fmt.Errorf("no space labeled %q", args[0])
		}
		delete(registry.Spaces, args[0])
		if err := registry.Save(""); err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "removed %q from the registry (directory untouched)\n", args[0])
		return nil
	}}
}

func (a *App) attendCommand() *cobra.Command {
	var command string
	cmd := &cobra.Command{Use: "attend <name>", Short: "Run an agent whenever it gets the floor", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		name, err := substrate.ParseName(args[0])
		if err != nil {
			return err
		}
		return watcher.Attend(cmd.Context(), name, command, a.Out, a.ErrOut)
	}}
	cmd.Flags().StringVar(&command, "exec", "", "one-shot harness command")
	return cmd
}

func (a *App) watchCommand(rootPath *string) *cobra.Command {
	var forName, command string
	cmd := &cobra.Command{Use: "watch <thread>", Short: "Report floor changes", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		space, err := substrate.OpenSpace(*rootPath)
		if err != nil {
			return err
		}
		thread, err := substrate.ParseName(args[0])
		if err != nil {
			return err
		}
		var participant *substrate.Name
		if forName != "" {
			parsed, err := substrate.ParseName(forName)
			if err != nil {
				return err
			}
			participant = &parsed
		}
		return watcher.Watch(cmd.Context(), space, thread, participant, command, a.Out, a.ErrOut)
	}}
	cmd.Flags().StringVar(&forName, "for", "", "only report this participant's floor")
	cmd.Flags().StringVar(&command, "exec", "", "hook command")
	return cmd
}

func (a *App) tuiCommand(rootPath *string) *cobra.Command {
	var name string
	cmd := &cobra.Command{Use: "tui", Short: "Launch the Bubble Tea application", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error { return a.runTUI(*rootPath, name) }}
	cmd.Flags().StringVar(&name, "name", "", "your registered participant name")
	return cmd
}

func (a *App) runTUI(rootPath, rawName string) error {
	space, err := substrate.OpenSpace(rootPath)
	if err != nil {
		var notSpace *substrate.NotASpaceError
		if !errors.As(err, &notSpace) {
			return err
		}
		reader := bufio.NewReader(a.In)
		fmt.Fprint(a.Out, "No substrate space exists here. Create one? [y/N] ")
		answer, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "y" && strings.ToLower(strings.TrimSpace(answer)) != "yes" {
			return errors.New("nothing created")
		}
		identity, ok := substrate.LoadIdentity()
		if !ok {
			fmt.Fprint(a.Out, "Your participant name: ")
			answer, _ = reader.ReadString('\n')
			identity, err = substrate.ParseName(strings.TrimSpace(answer))
			if err != nil {
				return err
			}
			if err := substrate.SaveIdentity(identity); err != nil {
				return err
			}
		}
		if _, err := substrate.BootstrapSpace(rootPath, &identity); err != nil {
			return err
		}
		space, err = substrate.OpenSpace(rootPath)
		if err != nil {
			return err
		}
	}
	me, err := a.resolveHuman(space, rawName)
	if err != nil {
		return err
	}
	return ui.Run(space, me)
}

func (a *App) resolveHuman(space *substrate.Space, rawName string) (substrate.Name, error) {
	if rawName != "" {
		name, err := substrate.ParseName(rawName)
		if err != nil {
			return "", err
		}
		if _, err := space.Participant(name); err != nil {
			return "", err
		}
		return name, nil
	}
	if identity, ok := substrate.LoadIdentity(); ok {
		if _, err := space.Participant(identity); err == nil {
			return identity, nil
		}
	}
	cfg, err := space.Config()
	if err != nil {
		return "", err
	}
	var humans []substrate.Name
	for _, participant := range cfg.Participants {
		if participant.Kind == substrate.Human {
			humans = append(humans, participant.Name)
		}
	}
	if len(humans) == 1 {
		return humans[0], nil
	}
	return "", errors.New("choose your registered identity with `substrate tui --name <name>`")
}

func (a *App) moderateCommand(rootPath *string) *cobra.Command {
	var actor string
	cmd := &cobra.Command{Use: "moderate", Aliases: []string{"mod"}, Short: "Scriptable moderator operations"}
	cmd.PersistentFlags().StringVar(&actor, "as", "", "moderator participant")
	_ = cmd.MarkPersistentFlagRequired("as")
	resolve := func(args []string) (*substrate.Space, substrate.Name, substrate.Name, error) {
		space, err := substrate.OpenSpace(*rootPath)
		if err != nil {
			return nil, "", "", err
		}
		thread, err := substrate.ParseName(args[0])
		if err != nil {
			return nil, "", "", err
		}
		name, err := substrate.ParseName(actor)
		if err != nil {
			return nil, "", "", err
		}
		if err := substrate.RequireModerator(space, thread, name); err != nil {
			return nil, "", "", err
		}
		return space, thread, name, nil
	}
	simpleName := func(use string, action func(*substrate.Space, substrate.Name, substrate.Name) error) *cobra.Command {
		return &cobra.Command{Use: use + " <thread> <name>", Args: cobra.ExactArgs(2), RunE: func(_ *cobra.Command, args []string) error {
			space, thread, _, err := resolve(args)
			if err != nil {
				return err
			}
			name, err := substrate.ParseName(args[1])
			if err != nil {
				return err
			}
			if err := action(space, thread, name); err != nil {
				return err
			}
			fmt.Fprintln(a.Out, "room updated")
			return nil
		}}
	}
	cmd.AddCommand(
		simpleName("next", substrate.SetNext),
		simpleName("invite", func(space *substrate.Space, thread, name substrate.Name) error {
			if _, err := space.EnsureParticipant(name); err != nil {
				return err
			}
			return substrate.Invite(space, thread, name)
		}),
		&cobra.Command{Use: "end <thread>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
			space, thread, _, err := resolve(args)
			if err != nil {
				return err
			}
			return substrate.EndThread(space, thread)
		}},
		&cobra.Command{Use: "resume <thread>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
			space, thread, _, err := resolve(args)
			if err != nil {
				return err
			}
			return substrate.ResumeThread(space, thread)
		}},
	)
	var quietTurns uint32
	quietCmd := &cobra.Command{Use: "quiet <thread> <name>", Args: cobra.ExactArgs(2), RunE: func(_ *cobra.Command, args []string) error {
		space, thread, _, err := resolve(args)
		if err != nil {
			return err
		}
		name, err := substrate.ParseName(args[1])
		if err != nil {
			return err
		}
		if err := substrate.Quiet(space, thread, name, quietTurns); err != nil {
			return err
		}
		fmt.Fprintln(a.Out, "room updated")
		return nil
	}}
	quietCmd.Flags().Uint32Var(&quietTurns, "turns", 1, "number of turns to skip; zero lifts quiet")
	var orderValues []string
	orderCmd := &cobra.Command{Use: "order <thread>", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		space, thread, _, err := resolve(args)
		if err != nil {
			return err
		}
		order, err := substrate.ParseNames(orderValues)
		if err != nil {
			return err
		}
		if err := substrate.ReorderTurns(space, thread, order); err != nil {
			return err
		}
		fmt.Fprintln(a.Out, "room updated")
		return nil
	}}
	orderCmd.Flags().StringSliceVar(&orderValues, "turns", nil, "replacement speaking order")
	_ = orderCmd.MarkFlagRequired("turns")
	topicCmd := &cobra.Command{Use: "topic <thread> <topic>", Args: cobra.MinimumNArgs(2), RunE: func(_ *cobra.Command, args []string) error {
		space, thread, _, err := resolve(args)
		if err != nil {
			return err
		}
		if err := substrate.SetTopic(space, thread, strings.Join(args[1:], " ")); err != nil {
			return err
		}
		fmt.Fprintln(a.Out, "room updated")
		return nil
	}}
	cmd.AddCommand(quietCmd, orderCmd, topicCmd)
	return cmd
}

func (a *App) doctorCommand(rootPath *string) *cobra.Command {
	return &cobra.Command{Use: "doctor", Short: "Report runtime, install, and space health", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		executable, _ := os.Executable()
		fmt.Fprintf(a.Out, "substrate version: %s\nruntime: %s\nexecutable: %s\nhome: %s\n", version.Version, version.Runtime, executable, substrate.HomeDir())
		if installed, err := exec.LookPath("substrate-mcp"); err == nil {
			fmt.Fprintf(a.Out, "substrate-mcp: %s\n", installed)
			versionCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			reported, versionErr := exec.CommandContext(versionCtx, installed, "--version").CombinedOutput()
			installedVersion := strings.TrimSpace(string(reported))
			if versionErr != nil || !strings.Contains(installedVersion, version.Version) || !strings.Contains(installedVersion, version.Runtime) {
				if installedVersion == "" {
					installedVersion = "version unavailable"
				}
				fmt.Fprintf(a.Out, "installed MCP version: %s\n", installedVersion)
				fmt.Fprintln(a.Out, "warning: installed substrate-mcp does not match this Go build; MCP clients remain on the old runtime until reinstalled or reconfigured")
			} else {
				fmt.Fprintf(a.Out, "installed MCP version: %s\n", installedVersion)
			}
		} else {
			fmt.Fprintln(a.Out, "substrate-mcp: not found on PATH")
		}
		space, err := substrate.OpenSpace(*rootPath)
		if err != nil {
			fmt.Fprintf(a.Out, "space: unavailable (%v)\n", err)
			return nil
		}
		cfg, err := space.Config()
		if err != nil {
			return err
		}
		threads, err := space.ListThreads()
		if err != nil {
			return err
		}
		unreadable := 0
		for _, thread := range threads {
			if _, err := substrate.GetTurnStatus(space, thread); err != nil {
				unreadable++
				fmt.Fprintf(a.Out, "thread %s: unreadable (%v)\n", thread, err)
			}
		}
		fmt.Fprintf(a.Out, "space: %s\nformat version: %d\nparticipants: %d\nthreads: %d (%d unreadable)\n", space.Root(), cfg.Version, len(cfg.Participants), len(threads), unreadable)
		if unreadable > 0 {
			return errors.New("doctor found unreadable threads")
		}
		fmt.Fprintln(a.Out, "health: ok")
		return nil
	}}
}

func (a *App) versionCommand() *cobra.Command {
	return &cobra.Command{Use: "version", Args: cobra.NoArgs, Run: func(_ *cobra.Command, _ []string) {
		fmt.Fprintf(a.Out, "substrate %s (%s)\n", version.Version, version.Runtime)
	}}
}

func sortedKeys(values map[string]string) []string {
	return slices.Sorted(maps.Keys(values))
}

func safeInt(value uint64) int {
	maximum := uint64(^uint(0) >> 1)
	return int(min(value, maximum))
}
