package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile      string
	verboseLevel int
	cpuProfile   string
	memProfile   string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "dovetail",
	Short: "Advanced directory comparison and synchronization tool",
	Long: `Dovetail is a command-line tool that performs advanced recursive comparison
of two directories, identifies differences, and allows for granular filtering.
It generates human-editable action files for controlled synchronization.

The tool follows a three-stage workflow:
1. Generate - Compare directories and create action file
2. Review - Manually edit the action file to specify desired actions  
3. Apply - Execute the actions in dry-run or real mode`,
	Version: "1.0.0",
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	// Setup profiling if requested
	if err := setupProfiling(); err != nil {
		fmt.Fprintf(os.Stderr, "Error setting up profiling: %v\n", err)
		return err
	}
	defer cleanupProfiling()

	// Setup signal handling for graceful profiling cleanup
	setupSignalHandling()

	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.dovetail.yaml)")
	rootCmd.PersistentFlags().CountVarP(&verboseLevel, "verbose", "v", "verbose output (-v basic, -vv detailed, -vvv debug)")
	rootCmd.PersistentFlags().Bool("no-color", false, "disable colored output")

	// Profiling flags
	rootCmd.PersistentFlags().StringVar(&cpuProfile, "cpuprofile", "", "write CPU profile to file")
	rootCmd.PersistentFlags().StringVar(&memProfile, "memprofile", "", "write memory profile to file")

	// Bind flags to viper
	viper.BindPFlag("verbose-level", rootCmd.PersistentFlags().Lookup("verbose"))
	viper.BindPFlag("no-color", rootCmd.PersistentFlags().Lookup("no-color"))
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".dovetail" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".dovetail")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		if GetVerboseLevel() > 0 {
			fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		}
	}
}

// GetVerboseLevel returns the current verbosity level
// 0 = no verbose output (default)
// 1 = basic verbose (-v) - shows high-level progress
// 2 = detailed verbose (-vv) - shows file-level progress
// 3+ = debug verbose (-vvv) - shows everything
func GetVerboseLevel() int {
	// Try to get from the flag first
	if verboseLevel > 0 {
		return verboseLevel
	}
	// Fall back to viper (for config file support)
	return viper.GetInt("verbose-level")
}

var (
	cpuFile *os.File
)

// GetCleanupProfiling returns the cleanup function for external use
func GetCleanupProfiling() func() {
	return cleanupProfiling
}

// setupProfiling initializes CPU and memory profiling if requested
func setupProfiling() error {
	if cpuProfile != "" {
		var err error
		cpuFile, err = os.Create(cpuProfile)
		if err != nil {
			return fmt.Errorf("could not create CPU profile file %s: %w", cpuProfile, err)
		}

		if err := pprof.StartCPUProfile(cpuFile); err != nil {
			cpuFile.Close()
			return fmt.Errorf("could not start CPU profile: %w", err)
		}
		fmt.Fprintf(os.Stderr, "CPU profiling enabled, writing to %s\n", cpuProfile)
	}

	return nil
}

// cleanupProfiling stops profiling and writes memory profile if requested
func cleanupProfiling() {
	if cpuProfile != "" && cpuFile != nil {
		pprof.StopCPUProfile()
		cpuFile.Close()
		fmt.Fprintf(os.Stderr, "CPU profile written to %s\n", cpuProfile)
	}

	if memProfile != "" {
		f, err := os.Create(memProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not create memory profile file %s: %v\n", memProfile, err)
			return
		}
		defer f.Close()

		runtime.GC() // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "could not write memory profile: %v\n", err)
			return
		}
		fmt.Fprintf(os.Stderr, "Memory profile written to %s\n", memProfile)
	}
}

// setupSignalHandling ensures profiling data is saved on interrupt
func setupSignalHandling() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-c
		fmt.Fprintf(os.Stderr, "\nReceived interrupt signal, cleaning up profiling...\n")
		cleanupProfiling()
		os.Exit(1)
	}()
}
