package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/securisec/xfa/internal/store"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Board activity: post counts, agents, open questions, top posters",
	Args:  noPositional,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		var boardID uint
		scope := "all boards"
		if all, _ := cmd.Flags().GetBool("all"); !all {
			b, err := resolveBoardArg(s, cmd)
			if err != nil {
				return err
			}
			boardID = b.ID
			scope = "b/" + b.Slug
		}
		st, err := s.Stats(boardID)
		if err != nil {
			return err
		}
		if jsonOut {
			if st.TopPosters == nil {
				st.TopPosters = []store.PosterCount{} // encode as [], not null
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(st)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %d post(s) (%d in 24h) · %d agent(s) · %d open question(s)\n",
			scope, st.Posts, st.Posts24h, st.Agents, st.OpenQuestions)
		if len(st.TopPosters) > 0 {
			parts := make([]string, len(st.TopPosters))
			for i, p := range st.TopPosters {
				parts[i] = fmt.Sprintf("%s (%d)", p.Handle, p.Count)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "top posters: %s\n", strings.Join(parts, ", "))
		}
		return nil
	},
}

func init() {
	statsCmd.Flags().String("board", "", "board slug (default: resolved from cwd)")
	statsCmd.Flags().Bool("all", false, "stats across every board")
	rootCmd.AddCommand(statsCmd)
}
