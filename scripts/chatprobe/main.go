// Command chatprobe answers one question that could not be answered from the
// Go client's source: does the Chat API accept the `users/me` alias in a
// spaceReadState resource name, and does it echo back a canonical
// `users/{numericId}/...` name we can parse the caller's user ID out of?
//
// Run it once, by hand, against a real Workspace account:
//
//	go run ./scripts/chatprobe
//
// It performs its own OAuth login and overwrites the active profile's stored
// token with a Chat-scoped one, so run `gsvc auth login` again afterwards.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/oauth2"
	chatapi "google.golang.org/api/chat/v1"
	"google.golang.org/api/option"

	"github.com/bennv/google_service_cli/internal/auth"
	"github.com/bennv/google_service_cli/internal/config"
)

var scopes = []string{
	chatapi.ChatSpacesReadonlyScope,
	chatapi.ChatMessagesReadonlyScope,
	chatapi.ChatUsersReadstateReadonlyScope,
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "probe failed:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	dir, err := config.DefaultDir()
	if err != nil {
		return err
	}
	store, err := config.NewStore(dir)
	if err != nil {
		return err
	}
	prof, err := store.Active()
	if err != nil {
		return err
	}
	tokens := auth.NewFileTokenStore(filepath.Join(dir, "tokens"))
	prov, err := auth.NewProvider(prof, tokens)
	if err != nil {
		return err
	}
	if in, ok := prov.(auth.Interactive); ok {
		fmt.Fprintln(os.Stderr, "logging in with Chat read-only scopes...")
		if err := in.Login(ctx, scopes); err != nil {
			return err
		}
	}
	ts, err := prov.TokenSource(ctx, scopes...)
	if err != nil {
		return err
	}
	svc, err := chatapi.NewService(ctx, option.WithHTTPClient(oauth2.NewClient(ctx, ts)))
	if err != nil {
		return err
	}

	spaces, err := svc.Spaces.List().PageSize(10).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("spaces.list: %w", err)
	}
	if len(spaces.Spaces) == 0 {
		return fmt.Errorf("no spaces visible to this account; join a space and retry")
	}
	sp := spaces.Spaces[0]
	fmt.Printf("space:            %s (%q, type=%s, threading=%s)\n",
		sp.Name, sp.DisplayName, sp.SpaceType, sp.SpaceThreadingState)
	fmt.Printf("space.spaceUri:   %q\n", sp.SpaceUri)

	name := "users/me/" + sp.Name + "/spaceReadState"
	rs, err := svc.Users.Spaces.GetSpaceReadState(name).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("getSpaceReadState(%s): %w", name, err)
	}
	fmt.Printf("readState.name:   %s\n", rs.Name)
	fmt.Printf("readState.time:   %s\n", rs.LastReadTime)

	if id, ok := strings.CutPrefix(rs.Name, "users/"); ok {
		if idx := strings.Index(id, "/"); idx > 0 {
			id = id[:idx]
		}
		if id == "me" {
			fmt.Println("RESULT: `me` accepted but NOT canonicalised — use the openid fallback.")
			return nil
		}
		fmt.Printf("RESULT: OK — caller user ID is users/%s\n", id)
		return nil
	}
	return fmt.Errorf("unexpected readState.name shape: %q", rs.Name)
}
