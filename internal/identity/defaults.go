package identity

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// defaultProfiles names the directories used by the two command-line tools.
type defaultProfiles struct {
	Client     string
	Supervisor string
}

func (p defaultProfiles) forRole(role string) string {
	if role == "supervisor" {
		return p.Supervisor
	}
	return p.Client
}

func readDefaults() (defaultProfiles, error) {
	p := defaultProfiles{"client", "supervisor"}
	b, err := os.ReadFile(Default("defaults.json"))
	if err == nil {
		p = defaultProfiles{}
		if err := json.Unmarshal(b, &p); err != nil {
			return p, err
		}
		for _, name := range []string{p.Client, p.Supervisor} {
			if name != "device" && name != "client" && name != "supervisor" {
				return p, errors.New("invalid default identity directory")
			}
		}
		if p.Client == p.Supervisor {
			return p, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return p, err
	}
	client, ce := Load(Default(p.Client))
	supervisor, se := Load(Default(p.Supervisor))
	for _, err := range []error{ce, se} {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return p, err
		}
	}
	switch {
	case ce == nil && se == nil && client != supervisor:
		return p, nil
	case ce == nil:
		return defaultProfiles{p.Client, p.Client}, nil
	case se == nil:
		return defaultProfiles{p.Supervisor, p.Supervisor}, nil
	default:
		return defaultProfiles{"device", "device"}, nil
	}
}

// CLIProfile resolves the saved directory and, when supplied, enrolls the link.
// A private lock serializes enrollment and publication across both executables.
func CLIProfile(ctx context.Context, role, link string) (string, Profile, error) {
	if link == "" {
		profiles, err := readDefaults()
		if err != nil {
			return "", Profile{}, err
		}
		dir := Default(profiles.forRole(role))
		profile, err := Load(dir)
		return dir, profile, err
	}
	invite, err := ParseInvitation(link)
	if err != nil {
		return "", Profile{}, err
	}
	if err := os.MkdirAll(Default(""), 0700); err != nil {
		return "", Profile{}, err
	}
	lock, err := os.OpenFile(Default("defaults.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", Profile{}, err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return "", Profile{}, err
	}
	profiles, err := readDefaults()
	if err != nil {
		return "", Profile{}, err
	}
	name := role
	if invite.Version == 2 {
		name = "device"
		if profiles.Client == profiles.Supervisor {
			name = profiles.Client
		} else {
			// Resolve conflicting defaults only after explicit pairing.
			// Reuse the identity already saved for the selected server.
			for _, source := range []string{profiles.Client, profiles.Supervisor} {
				profile, err := Load(Default(source))
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return "", Profile{}, err
				}
				if profile == invite.Profile {
					name = source
					break
				}
			}
		}
		profiles = defaultProfiles{name, name}
	} else {
		// Older daemons assign a single role to each enrolled key.
		if role == "client" {
			profiles.Client = role
			if profiles.Supervisor == role {
				profiles.Supervisor = "supervisor"
			}
		} else {
			profiles.Supervisor = role
			if profiles.Client == role {
				profiles.Client = "client"
			}
		}
	}
	dir := Default(name)
	if err := pair(ctx, dir, link, role, invite); err != nil {
		return "", Profile{}, err
	}
	return dir, invite.Profile, Save(Default("defaults.json"), profiles)
}
