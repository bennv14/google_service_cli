package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bennv14/google_service_cli/internal/config"
	"github.com/bennv14/google_service_cli/internal/service"
)

func newConfigCmd(d *service.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "config",
		Short:        "Manage profiles",
		SilenceUsage: true,
	}
	cmd.AddCommand(configAddCmd(d), configListCmd(d), configUseCmd(d), configShowCmd(d))
	return cmd
}

func configAddCmd(d *service.Deps) *cobra.Command {
	var authType, clientPath, keyPath string
	var authAlt, clientAlt, keyAlt string

	c := &cobra.Command{
		Use:   "add <name>",
		Short: "Add or update a profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if authType == "" && authAlt != "" {
				authType = authAlt
			}
			if clientPath == "" && clientAlt != "" {
				clientPath = clientAlt
			}
			if keyPath == "" && keyAlt != "" {
				keyPath = keyAlt
			}
			if authType == "" {
				authType = "oauth"
			}

			switch authType {
			case "oauth":
				if clientPath == "" {
					return fmt.Errorf("--client-path is required for --auth-type oauth")
				}
			case "service_account":
				if keyPath == "" {
					return fmt.Errorf("--key-path is required for --auth-type service_account")
				}
			default:
				return fmt.Errorf("invalid auth type %q: must be 'oauth' or 'service_account'", authType)
			}

			p := config.Profile{
				Name:       args[0],
				AuthType:   authType,
				ClientPath: clientPath,
				KeyPath:    keyPath,
			}
			if err := d.Config.Save(p); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved profile %q.\n", p.Name)
			return nil
		},
	}

	c.Flags().StringVar(&authType, "auth-type", "", "auth type: oauth | service_account")
	c.Flags().StringVar(&authAlt, "auth", "", "alias for --auth-type")
	c.Flags().StringVar(&clientPath, "client-path", "", "path to OAuth client credentials JSON")
	c.Flags().StringVar(&clientAlt, "client", "", "alias for --client-path")
	c.Flags().StringVar(&keyPath, "key-path", "", "path to service account key JSON")
	c.Flags().StringVar(&keyAlt, "key", "", "alias for --key-path")

	return c
}

type profileList struct {
	Profiles []config.Profile `json:"profiles"`
	Active   string           `json:"active"`
}

func (p profileList) Headers() []string { return []string{"ACTIVE", "NAME", "AUTH"} }

func (p profileList) Rows() [][]string {
	rows := make([][]string, 0, len(p.Profiles))
	for _, pr := range p.Profiles {
		marker := ""
		if pr.Name == p.Active {
			marker = "*"
		}
		rows = append(rows, []string{marker, pr.Name, pr.AuthType})
	}
	return rows
}

func configListCmd(d *service.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			active := ""
			if p, err := d.Config.Active(); err == nil {
				active = p.Name
			}
			return d.Out.Render(profileList{Profiles: d.Config.List(), Active: active})
		},
	}
}

func configUseCmd(d *service.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the active profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := d.Config.SetActive(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Active profile is now %q.\n", args[0])
			return nil
		},
	}
}

type profileShow struct {
	config.Profile
}

func (p profileShow) Headers() []string {
	return []string{"NAME", "AUTH", "CLIENT_PATH", "KEY_PATH"}
}

func (p profileShow) Rows() [][]string {
	return [][]string{{p.Name, p.AuthType, p.ClientPath, p.KeyPath}}
}

func configShowCmd(d *service.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show [name]",
		Short: "Show profile details",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var p config.Profile
			var err error
			if len(args) == 1 {
				p, err = d.Config.Get(args[0])
			} else {
				p, err = d.Config.Active()
			}
			if err != nil {
				return err
			}
			return d.Out.Render(profileShow{Profile: p})
		},
	}
}
