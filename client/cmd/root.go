package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var config struct {
	address  string
	datapath string
	debug    bool
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "client",
	Short: "ffdb同步数据客户端",
	Long:  `CLI客户端，--help for more information`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&config.address, "address", "localhost:8901", "RPC Server address")
	rootCmd.PersistentFlags().StringVar(&config.datapath, "path", "/srv/ffdb/", "data and config path")
	rootCmd.PersistentFlags().BoolVar(&config.debug, "debug", false, "enable debug")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	cfgFile := filepath.Join(config.datapath, "config.json")
	viper.SetConfigFile(cfgFile)

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
