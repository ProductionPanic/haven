// Package hostform is the interactive add/edit form for a host.
package hostform

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/huh/v2"

	"github.com/ProductionPanic/haven/v2/internal/sshx"
	"github.com/ProductionPanic/haven/v2/internal/store"
)

// ErrCancelled is returned when the user aborts the form.
var ErrCancelled = errors.New("cancelled")

// Environments offered in the form.
var Environments = []string{"", "production", "staging", "development"}

// Fields holds the form's string-typed values and converts to and from a Host.
type Fields struct {
	Name, User, Hostname, Port, IdentityFile, JumpHost string
	RemotePath, ExtraArgs, Notes, Environment, Tags    string
}

// FromHost fills form fields from h.
func FromHost(h store.Host) *Fields {
	f := &Fields{
		Name: h.Name, User: h.User, Hostname: h.Hostname, IdentityFile: h.IdentityFile,
		JumpHost: h.JumpHost, RemotePath: h.RemotePath, ExtraArgs: h.ExtraArgs,
		Notes: h.Notes, Environment: h.Environment, Tags: strings.Join(h.Tags, ", "),
	}
	if h.Port != 0 && h.Port != 22 {
		f.Port = strconv.Itoa(h.Port)
	}
	return f
}

// Apply copies the form values onto h (keeping its ID and stats).
func (f *Fields) Apply(h store.Host) store.Host {
	h.Name = strings.TrimSpace(f.Name)
	h.User = strings.TrimSpace(f.User)
	h.Hostname = strings.TrimSpace(f.Hostname)
	h.Port, _ = strconv.Atoi(strings.TrimSpace(f.Port))
	h.IdentityFile = strings.TrimSpace(f.IdentityFile)
	h.JumpHost = strings.TrimSpace(f.JumpHost)
	h.RemotePath = strings.TrimSpace(f.RemotePath)
	h.ExtraArgs = strings.TrimSpace(f.ExtraArgs)
	h.Notes = strings.TrimRight(f.Notes, "\n ")
	h.Environment = f.Environment
	h.Tags = store.NormalizeTags([]string{f.Tags})
	return h
}

func required(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", what)
		}
		return nil
	}
}

func validPort(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("port must be a number between 1 and 65535")
	}
	return nil
}

func validArgs(s string) error {
	_, err := sshx.SplitArgs(s)
	return err
}

// New builds the form bound to f. nameTaken (optional) reports whether a
// name is already used by another host.
func New(title string, f *Fields, nameTaken func(string) bool) *huh.Form {
	envOpts := make([]huh.Option[string], len(Environments))
	for i, e := range Environments {
		label := e
		if label == "" {
			label = "none"
		}
		envOpts[i] = huh.NewOption(label, e)
	}
	// Keep a custom environment selectable when editing.
	known := false
	for _, e := range Environments {
		known = known || e == f.Environment
	}
	if !known {
		envOpts = append(envOpts, huh.NewOption(f.Environment, f.Environment))
	}

	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().Title(title),
			huh.NewInput().Title("Name").Placeholder("appelenburg.nl").Value(&f.Name).
				Validate(func(s string) error {
					if err := required("name")(s); err != nil {
						return err
					}
					if nameTaken != nil && nameTaken(strings.TrimSpace(s)) {
						return errors.New("a host with this name already exists")
					}
					return nil
				}),
			huh.NewInput().Title("User").Placeholder("web").Value(&f.User),
			huh.NewInput().Title("Hostname").Description("Server address or an ~/.ssh/config alias").
				Placeholder("server.example.com").Value(&f.Hostname).Validate(required("hostname")),
			huh.NewInput().Title("Port").Placeholder("22").Value(&f.Port).Validate(validPort),
			huh.NewSelect[string]().Title("Environment").Options(envOpts...).Value(&f.Environment),
			huh.NewInput().Title("Tags").Description("Comma separated").Placeholder("wordpress, client-x").Value(&f.Tags),
		),
		huh.NewGroup(
			huh.NewInput().Title("Remote path").Description("Start directory on login and in the file manager").
				Placeholder("/var/www/site").Value(&f.RemotePath),
			huh.NewInput().Title("Identity file").Placeholder("~/.ssh/id_ed25519").Value(&f.IdentityFile),
			huh.NewInput().Title("Jump host").Description("ProxyJump, e.g. user@bastion").Value(&f.JumpHost),
			huh.NewInput().Title("Extra ssh args").Placeholder(`-o ServerAliveInterval=30`).Value(&f.ExtraArgs).Validate(validArgs),
			huh.NewText().Title("Notes").Value(&f.Notes),
		),
	)
}

// Run shows the form on stderr and returns the edited host.
func Run(title string, h store.Host, nameTaken func(string) bool) (store.Host, error) {
	f := FromHost(h)
	err := New(title, f, nameTaken).WithOutput(os.Stderr).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return store.Host{}, ErrCancelled
	}
	if err != nil {
		return store.Host{}, err
	}
	return f.Apply(h), nil
}
