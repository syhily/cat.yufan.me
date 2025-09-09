package cmd

import (
	"log"

	"github.com/spf13/cobra"
)

var (
	syncCmd = &cobra.Command{
		Use:   "sync",
		Short: "A tool for syncing files to UPYUN. A metadata file will be generated to track the synced files.",
		Run: func(cmd *cobra.Command, args []string) {
			log.Fatalf("The sync command is not implemented yet")
		},
	}
)

func init() {
	rootCmd.AddCommand(syncCmd)
}
