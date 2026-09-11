package cmd

import (
	"fmt"
	"strings"

	"github.com/securisec/xfa/internal/handle"
	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "Mint a handle for this agent session",
	Args:  noPositional,
	RunE: func(cmd *cobra.Command, args []string) error {
		provider, _ := cmd.Flags().GetString("provider")
		session, _ := cmd.Flags().GetString("session")
		parent, _ := cmd.Flags().GetString("parent")
		topic, _ := cmd.Flags().GetString("topic")
		if provider == store.ProviderHuman {
			return fmt.Errorf("--provider %s is refused: human is minted by the web UI, not register", store.ProviderHuman)
		}
		for _, v := range []string{provider, session, parent} {
			if len(v) > 128 {
				return fmt.Errorf("register: flag value too long")
			}
		}
		// A bad --topic is noted and dropped, never fatal (which is why it is
		// absent from the length bound above — ValidTopic already caps it at
		// 10 and the value never reaches gorm): the flag is cosmetic
		// and killing a session over it is worse than a random handle. The note
		// is what teaches an agent the flag exists and what shape it wants.
		// This is also the server-side gate — register is in remote.Verbs, so
		// serve execs this binary and no client-side check would run.
		if topic != "" {
			if t, ok := handle.ValidTopic(topic); ok {
				topic = t
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), "--topic ignored: use one lowercase word (a-z0-9, max 10)")
				topic = ""
			}
		}
		if topic == "" && parent != "" {
			// Subagents reliably pass --parent and reliably forget --topic (the
			// only thing asking for one is prose the lead has to copy into the
			// spawn prompt), so inherit the parent's topic slot: --parent
			// debate-salamander-62 mints debate-<noun>-<N>. Inheriting a
			// parent's random adjective is intended — it groups lineage and is
			// indistinguishable from any other random handle. An unusable
			// parent degrades to random in SILENCE: inheritance is automatic,
			// so there is no --topic the caller could have gotten wrong. A
			// rejected explicit --topic lands here too — the note above says
			// "ignored", which has to mean "as if never passed".
			if first, _, ok := strings.Cut(parent, "-"); ok {
				topic, _ = handle.ValidTopic(first)
			}
		}
		s, err := openStore()
		if err != nil {
			return err
		}
		a, err := s.RegisterAgentTopic(cwd(), provider, session, parent, topic)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), a.Handle)
		return nil
	},
}

func init() {
	registerCmd.Flags().String("provider", "claude", "provider name")
	registerCmd.Flags().String("session", "", "provider session id")
	registerCmd.Flags().String("parent", "", "parent agent handle (for subagents)")
	registerCmd.Flags().String("topic", "", "one lowercase word (a-z0-9, max 10) to prefix the minted handle")
	rootCmd.AddCommand(registerCmd)
}
