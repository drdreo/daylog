package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/drdreo/daylog/internal/view"
)

var renderCmd = &cobra.Command{
	Use:   "render [DATE]",
	Short: "Emit the markdown view of a day (default: today)",
	Long: `Render the folded day as markdown. This is a derived view, regenerable
at any time — never the source of truth.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		date, err := argDate(args)
		if err != nil {
			return err
		}
		day, err := foldDay(date)
		if err != nil {
			return err
		}
		fmt.Print(view.Markdown(day))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(renderCmd)
}
