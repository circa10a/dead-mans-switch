package cmd

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/circa10a/dead-mans-switch/internal/server"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Constants for Viper keys and Flag names
const (
	authEnabledKey        = "auth-enabled"
	authIssuerURLKey      = "auth-issuer-url"
	authAudienceKey       = "auth-audience"
	autoTLSKey            = "auto-tls"
	contactEmailKey       = "contact-email"
	demoModeKey           = "demo-mode"
	demoPResetIntervalKey = "demo-reset-interval"
	domainsKey            = "domains"
	logFormatKey          = "log-format"
	logLevelKey           = "log-level"
	metricsKey            = "metrics"
	portKey               = "port"
	storageDirKey         = "storage-dir"
	tlsCertificateKey     = "tls-certificate"
	tlsKeyKey             = "tls-key"
	workerBatchSizeKey    = "worker-batch-size"
	workerIntervalKey     = "worker-interval"
)

// serverCmd represents the server command
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: fmt.Sprintf("Start the %s server", project),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Build server configuration using the constants
		cfg := &server.Config{
			AuthEnabled:       viper.GetBool(authEnabledKey),
			AuthIssuerURL:     viper.GetString(authIssuerURLKey),
			AuthAudience:      viper.GetString(authAudienceKey),
			AutoTLS:           viper.GetBool(autoTLSKey),
			ContactEmail:      viper.GetString(contactEmailKey),
			DemoMode:          viper.GetBool(demoModeKey),
			DemoResetInterval: viper.GetDuration(demoPResetIntervalKey),
			Domains:           viper.GetStringSlice(domainsKey),
			LogFormat:         viper.GetString(logFormatKey),
			LogLevel:          viper.GetString(logLevelKey),
			Metrics:           viper.GetBool(metricsKey),
			Port:              viper.GetInt(portKey),
			StorageDir:        viper.GetString(storageDirKey),
			TLSCert:           viper.GetString(tlsCertificateKey),
			TLSKey:            viper.GetString(tlsKeyKey),
			Validation:        true,
			WorkerBatchSize:   viper.GetInt(workerBatchSizeKey),
			WorkerInterval:    viper.GetDuration(workerIntervalKey),
		}

		server, err := server.New(cfg)
		if err != nil {
			return err
		}

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

		go func() {
			err := server.Start()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("server error: %v", err)
			}
		}()

		<-stop
		server.Stop()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(serverCmd)

	serverFlags := []flagDef{
		{Name: authEnabledKey, Type: "bool", Default: false, Usage: "Enable JWT authentication via Authentik.", ViperKey: authEnabledKey},
		{Name: authIssuerURLKey, Type: "string", Default: "", Usage: "Identity provider OAuth2 issuer URL.", ViperKey: authIssuerURLKey},
		{Name: authAudienceKey, Type: "string", Default: "", Usage: "Expected JWT audience claim.", ViperKey: authAudienceKey},
		{Name: autoTLSKey, Shorthand: "a", Type: "bool", Default: false, Usage: "Enable automatic TLS via Let's Encrypt. Requires port 80/443 open to the internet for domain validation.", ViperKey: autoTLSKey},
		{Name: contactEmailKey, Shorthand: "", Type: "string", Default: "user@dead-mans-switch.com", Usage: "Email used for TLS cert registration + push notification point of contact (not required).", ViperKey: contactEmailKey},
		{Name: demoModeKey, Shorthand: "", Type: "bool", Default: false, Usage: "Enable demo mode which creates sample switches on startup and resets the database periodically.", ViperKey: demoModeKey},
		{Name: demoPResetIntervalKey, Shorthand: "", Type: "duration", Default: 1 * time.Hour, Usage: "How often to reset the database with fresh sample switches when in demo mode.", ViperKey: demoPResetIntervalKey},
		{Name: domainsKey, Shorthand: "d", Type: "stringArray", Default: []string{}, Usage: "Domains to issue certificate for. Must be used with --auto-tls.", ViperKey: domainsKey},
		{Name: logFormatKey, Shorthand: "f", Type: "string", Default: "text", Usage: "Server logging format. Supported values are 'text' and 'json'.", ViperKey: logFormatKey},
		{Name: logLevelKey, Shorthand: "l", Type: "string", Default: "info", Usage: "Server logging level.", ViperKey: logLevelKey},
		{Name: metricsKey, Shorthand: "m", Type: "bool", Default: false, Usage: "Enable Prometheus metrics instrumentation.", ViperKey: metricsKey},
		{Name: portKey, Shorthand: "p", Type: "int", Default: 8080, Usage: "Port to listen on. Cannot be used in conjunction with --auto-tls since that will require listening on 80 and 443.", ViperKey: portKey},
		{Name: storageDirKey, Shorthand: "s", Type: "string", Default: "./data", Usage: "Storage directory for database", ViperKey: storageDirKey},
		{Name: tlsCertificateKey, Shorthand: "", Type: "string", Default: "", Usage: "Path to custom TLS certificate. Cannot be used with --auto-tls.", ViperKey: tlsCertificateKey},
		{Name: tlsKeyKey, Shorthand: "", Type: "string", Default: "", Usage: "Path to custom TLS key. Cannot be used with --auto-tls.", ViperKey: tlsKeyKey},
		{Name: workerBatchSizeKey, Shorthand: "", Type: "int", Default: 1000, Usage: "How many notification records to process at a time.", ViperKey: workerBatchSizeKey},
		{Name: workerIntervalKey, Shorthand: "", Type: "duration", Default: 1 * time.Minute, Usage: "How often to check for expired switches.", ViperKey: workerIntervalKey},
	}

	registerFlagTypes(serverCmd, serverFlags)

	viper.SetEnvPrefix(strings.ToUpper(envVarPrefix))
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	for _, d := range serverFlags {
		_ = viper.BindPFlag(d.ViperKey, serverCmd.Flags().Lookup(d.Name))
	}

	serverCmd.Flags().VisitAll(func(f *pflag.Flag) {
		env := strings.ToUpper(envVarPrefix) + "_" + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
		if !strings.Contains(f.Usage, "env:") {
			f.Usage = fmt.Sprintf("%s (env: %s)", f.Usage, env)
		}
	})
}
