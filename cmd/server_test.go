package cmd

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func setupViper() {
	viper.Reset()
	viper.SetEnvPrefix(strings.ToUpper(envVarPrefix))
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// This prevents TestServerFlags values from leaking into TestServerEnvVariables
	serverCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false // Tell cobra this flag wasn't explicitly set
	})

	// Re-bind the server flags to viper
	serverCmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = viper.BindPFlag(f.Name, f)
	})
}

func TestServerFlags(t *testing.T) {
	setupViper()

	args := []string{
		"--port", "9090",
		"--log-level", "debug",
		"--metrics",
		"--worker-interval", "10s",
	}

	// We use serverCmd directly to avoid issues with other commands
	err := serverCmd.ParseFlags(args)
	assert.NoError(t, err)

	assert.Equal(t, 9090, viper.GetInt(portKey))
	assert.Equal(t, "debug", viper.GetString(logLevelKey))
	assert.True(t, viper.GetBool(metricsKey))
	assert.Equal(t, 10*time.Second, viper.GetDuration(workerIntervalKey))
}

func TestServerEnvVariables(t *testing.T) {
	setupViper()

	// Set environment variables
	_ = os.Setenv("DEAD_MANS_SWITCH_PORT", "1234")
	_ = os.Setenv("DEAD_MANS_SWITCH_LOG_LEVEL", "warn")
	defer func() {
		_ = os.Unsetenv("DEAD_MANS_SWITCH_PORT")
		_ = os.Unsetenv("DEAD_MANS_SWITCH_LOG_LEVEL")
	}()

	// In some versions of viper, you must call this again after setting env vars
	viper.AutomaticEnv()

	assert.Equal(t, 1234, viper.GetInt(portKey))
	assert.Equal(t, "warn", viper.GetString(logLevelKey))
}

func TestServerSignalExit(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "dms-test")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	viper.Set(storageDirKey, tmpDir)
	viper.Set(portKey, 0) // Port 0 lets OS pick a free port

	errChan := make(chan error, 1)

	go func() {
		rootCmd.SetArgs([]string{"server"})
		errChan <- rootCmd.Execute()
	}()

	// Give the server a bit to start
	time.Sleep(500 * time.Millisecond)

	// Send SIGTERM to ourselves (the process running the test)
	// The command's signal.Notify will catch it.
	process, _ := os.FindProcess(os.Getpid())
	_ = process.Signal(syscall.SIGTERM)

	select {
	case err := <-errChan:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Server command did not exit after SIGTERM")
	}
}
