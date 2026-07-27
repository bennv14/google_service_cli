package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/bennv/google_service_cli/internal/auth"
	"github.com/bennv/google_service_cli/internal/service"
)

func newAuthCmd(d *service.Deps) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Manage authentication"}
	cmd.AddCommand(authLoginCmd(d), authLogoutCmd(d), authStatusCmd(d))
	return cmd
}

func requireProfile(d *service.Deps) error {
	if d.Profile.Name == "" {
		return errors.New("no profile selected; run 'gsvc config add' first")
	}
	return nil
}

func authLoginCmd(d *service.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate the active profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireProfile(d); err != nil {
				return err
			}
			prov, err := auth.NewProvider(d.Profile, d.Tokens)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if in, ok := prov.(auth.Interactive); ok {
				if err := in.Login(ctx, d.Scopes); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Logged in profile %q.\n", d.Profile.Name)
				return nil
			}
			// Non-interactive (service account): validate by minting a token.
			ts, err := prov.TokenSource(ctx, d.Scopes...)
			if err != nil {
				return err
			}
			if _, err := ts.Token(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Service account for profile %q is valid.\n", d.Profile.Name)
			return nil
		},
	}
}

func authLogoutCmd(d *service.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored token for the active profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireProfile(d); err != nil {
				return err
			}
			if err := d.Tokens.Delete(d.Profile.Name); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Logged out profile %q.\n", d.Profile.Name)
			return nil
		},
	}
}

func authStatusCmd(d *service.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show authentication status for the active profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireProfile(d); err != nil {
				return err
			}
			state := "not logged in"
			if d.Profile.AuthType == "service_account" {
				state = "service account (" + d.Profile.KeyPath + ")"
			} else if tok, err := d.Tokens.Load(d.Profile.Name); err == nil {
				if tok.Valid() {
					state = "logged in (token valid until " + tok.Expiry.Format("2006-01-02 15:04") + ")"
				} else {
					state = "logged in (token expired; will refresh on next use)"
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "profile: %s\nauth:    %s\nstatus:  %s\n",
				d.Profile.Name, d.Profile.AuthType, state)
			return nil
		},
	}
}
