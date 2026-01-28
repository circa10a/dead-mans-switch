package cmd

import (
	"fmt"
	"strings"

	"github.com/circa10a/dead-mans-switch/internal/server"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// serverCmd represents the server command
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: fmt.Sprintf("Start the %s server", project),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Build server configuration from environment (via viper) or flags
		cfg := &server.Config{
			AutoTLS:           viper.GetBool("auto-tls"),
			Domains:           viper.GetStringSlice("domains"),
			EncryptionEnabled: viper.GetBool("encryption-enabled"),
			LogFormat:         viper.GetString("log-format"),
			LogLevel:          viper.GetString("log-level"),
			Metrics:           viper.GetBool("metrics"),
			Port:              viper.GetInt("port"),
			StorageDir:        viper.GetString("storage-dir"),
			TLSCert:           viper.GetString("tls-certificate"),
			TLSKey:            viper.GetString("tls-key"),
			Validation:        true,
		}

		s, err := server.New(cfg)
		if err != nil {
			return err
		}

		err = s.Start()
		if err != nil {
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(serverCmd)

	serverFlags := []flagDef{
		{Name: "auto-tls", Shorthand: "a", Type: "bool", Default: false, Usage: "Enable automatic TLS via Let's Encrypt. Requires port 80/443 open to the internet for domain validation.", ViperKey: "auto-tls"},
		{Name: "domains", Shorthand: "d", Type: "stringArray", Default: []string{}, Usage: "Domains to issue certificate for. Must be used with --auto-tls.", ViperKey: "domains"},
		{Name: "encryption-enabled", Shorthand: "e", Type: "bool", Default: false, Usage: "Encryption enabled ensures notifiers aren't stored/return in plaintext. Will create a key in the storage directory.", ViperKey: "encryption-enabled"},
		{Name: "log-format", Shorthand: "f", Type: "string", Default: "text", Usage: "Server logging format. Supported values are 'text' and 'json'.", ViperKey: "log-format"},
		{Name: "log-level", Shorthand: "l", Type: "string", Default: "info", Usage: "Server logging level.", ViperKey: "log-level"},
		{Name: "metrics", Shorthand: "m", Type: "bool", Default: false, Usage: "Enable Prometheus metrics intrumentation.", ViperKey: "metrics"},
		{Name: "port", Shorthand: "p", Type: "int", Default: 8080, Usage: "Port to listen on. Cannot be used in conjunction with --auto-tls since that will require listening on 80 and 443.", ViperKey: "port"},
		{Name: "storage-dir", Shorthand: "s", Type: "string", Default: "./data", Usage: "Storage directory for database", ViperKey: "storage-dir"},
		{Name: "tls-certificate", Shorthand: "", Type: "string", Default: "", Usage: "Path to custom TLS certificate. Cannot be used with --auto-tls.", ViperKey: "tls-certificate"},
		{Name: "tls-key", Shorthand: "", Type: "string", Default: "", Usage: "Path to custom TLS key. Cannot be used with --auto-tls.", ViperKey: "tls-key"},
	}

	// Register flags using the centralized helper from root.go
	RegisterFlagTypes(serverCmd, serverFlags)

	// Viper: bind environment variables and enable env support for flags.
	viper.SetEnvPrefix(strings.ToUpper(envVarPrefix))
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	for _, d := range serverFlags {
		_ = viper.BindPFlag(d.ViperKey, serverCmd.Flags().Lookup(d.Name))
	}

	// Append environment variable hints to flag usage text so users see how to set via environment variable
	serverCmd.Flags().VisitAll(func(f *pflag.Flag) {
		env := strings.ToUpper(envVarPrefix) + "_" + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
		if !strings.Contains(f.Usage, "env:") {
			f.Usage = fmt.Sprintf("%s (env: %s)", f.Usage, env)
		}
	})
}
