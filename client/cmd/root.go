package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var config struct {
	address  string
	username string
	file     string
	command  string
	arg1     string
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

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&config.address, "address", "localhost:8901", "RPC Server address")
	rootCmd.PersistentFlags().StringVar(&config.file, "config", "/srv/ffdb/config.json", "config file")
	// 	flag.StringVar(&config.command, "cmd", "", "cmd execution")
	// 	flag.StringVar(&config.arg1, "arg1", "", "pass argument to command")
	// 	flag.StringVar(&config.username, "u", "", "debug user feed")
	rootCmd.PersistentFlags().BoolVar(&config.debug, "debug", false, "enable debug")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if config.file != "" {
		// Use config file from the flag.
		viper.SetConfigFile(config.file)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".ffdb" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("json")
		viper.SetConfigName(".ffdb")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
