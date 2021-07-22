package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var stockName string

var stockCmd = &cobra.Command{
	Use:   "stock",
	Short: "sync stock data",
	Long: `sync stock data
    `,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("sync stock: %s\n", stockName)
		if stockName == "" {
			return
		}
	},
}

func init() {
	rootCmd.AddCommand(stockCmd)
	stockCmd.Flags().StringVar(&stockName, "c", "", "stockName")
}
