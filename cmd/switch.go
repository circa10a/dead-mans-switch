package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/circa10a/dead-mans-switch/api"
	"github.com/fatih/color"
	"github.com/hokaccha/go-prettyjson"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	apiURL       string
	outputFormat string
	useColor     bool
	client       *api.ClientWithResponses
)

func initClient() error {
	var err error

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	opts := []api.ClientOption{
		api.WithHTTPClient(httpClient),
	}

	// Attach cached bearer token if available
	tok, loadErr := loadToken()
	if loadErr == nil && tok != nil && tok.AccessToken != "" {
		opts = append(opts, withBearerToken(tok.AccessToken))
	}

	client, err = api.NewClientWithResponses(apiURL, opts...)
	return err
}

// formatOutput handles conversion and writing to the command's designated output
func formatOutput(cmd *cobra.Command, data interface{}, isError bool) {
	if !useColor {
		color.NoColor = true
	} else {
		color.NoColor = false
	}

	var out string

	switch outputFormat {
	case "yaml":
		b, _ := yaml.Marshal(data)
		if useColor {
			if isError {
				out = color.RedString(string(b))
			} else {
				out = color.CyanString(string(b))
			}
		} else {
			out = string(b)
		}

	case "json":
		fallthrough
	default:
		if useColor {
			b, _ := prettyjson.Marshal(data)
			out = string(b)
		} else {
			b, _ := json.MarshalIndent(data, "", "  ")
			out = string(b)
		}
	}

	cmd.Println(out)
}

// dumpResponse handles formatting success data or API error models
func dumpResponse(cmd *cobra.Command, statusCode int, body []byte, successData interface{}) {
	// 1. Handle all successful status codes (200-299)
	if statusCode >= 200 && statusCode < 300 {
		if successData != nil {
			formatOutput(cmd, successData, false)
		} else {
			// Success but no data (e.g., 204 No Content)
			if useColor {
				_, err := color.New(color.FgGreen).Fprintln(cmd.OutOrStdout(), "Success")
				if err != nil {
					cmd.PrintErrf("Error writing to stdout %v\n", err)
					return
				}
			} else {
				cmd.Println("Success")
			}
		}
		return
	}

	// Handle Errors: Try to parse structured API error first
	var apiErr api.Error
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Message != "" {
		formatOutput(cmd, apiErr, true)
		return
	}

	// Fallback: Print raw body or just the status code
	if len(body) > 0 {
		if useColor {
			_, _ = color.New(color.FgRed).Fprintln(cmd.OutOrStdout(), string(body))
		} else {
			cmd.PrintErrln(string(body))
		}
	} else {
		cmd.PrintErrf("Error: Received status code %d\n", statusCode)
	}
}

var switchCmd = &cobra.Command{
	Use:   "switch",
	Short: "Manage dead man switches",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return initClient()
	},
}

var getSwitchesCmd = &cobra.Command{
	Use:   "get [id]",
	Short: "Get all switches or a specific one by ID",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()

		if len(args) > 0 {
			var id int
			_, err := fmt.Sscanf(args[0], "%d", &id)
			if err != nil {
				return fmt.Errorf("invalid ID format: %w", err)
			}

			resp, err := client.GetSwitchIdWithResponse(ctx, id)
			if err != nil {
				return err
			}

			dumpResponse(cmd, resp.StatusCode(), resp.Body, resp.JSON200)
			return nil
		}

		resp, err := client.GetSwitchWithResponse(ctx, &api.GetSwitchParams{})
		if err != nil {
			return err
		}
		dumpResponse(cmd, resp.StatusCode(), resp.Body, resp.JSON200)
		return nil
	},
}

var createSwitchCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new dead man switch",
	RunE: func(cmd *cobra.Command, args []string) error {
		msg, _ := cmd.Flags().GetString("message")
		interval, _ := cmd.Flags().GetDuration("interval")
		notifiers, _ := cmd.Flags().GetStringArray("notifiers")
		encrypt, _ := cmd.Flags().GetBool("encrypt")
		deleteAfter, _ := cmd.Flags().GetBool("delete-after-triggered")

		body := api.PostSwitchJSONRequestBody{
			Message:              msg,
			CheckInInterval:      interval.String(),
			Notifiers:            notifiers,
			Encrypted:            &encrypt,
			DeleteAfterTriggered: &deleteAfter,
		}

		resp, err := client.PostSwitchWithResponse(context.Background(), body)
		if err != nil {
			return err
		}
		dumpResponse(cmd, resp.StatusCode(), resp.Body, resp.JSON201)
		return nil
	},
}

var updateSwitchCmd = &cobra.Command{
	Use:   "update [id]",
	Short: "Update an existing dead man switch",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var id int
		_, err := fmt.Sscanf(args[0], "%d", &id)
		if err != nil {
			return err
		}

		ctx := context.Background()

		existing, err := client.GetSwitchIdWithResponse(ctx, id)
		if err != nil {
			return err
		}
		if existing.JSON200 == nil {
			dumpResponse(cmd, existing.StatusCode(), existing.Body, nil)
			return nil
		}

		msg, _ := cmd.Flags().GetString("message")
		interval, _ := cmd.Flags().GetDuration("interval")
		notifiers, _ := cmd.Flags().GetStringArray("notifiers")
		deleteAfter, _ := cmd.Flags().GetBool("delete-after-triggered")

		body := api.PutSwitchIdJSONRequestBody{
			Message:              existing.JSON200.Message,
			CheckInInterval:      existing.JSON200.CheckInInterval,
			Notifiers:            existing.JSON200.Notifiers,
			DeleteAfterTriggered: existing.JSON200.DeleteAfterTriggered,
			Encrypted:            existing.JSON200.Encrypted,
		}

		if cmd.Flags().Changed("message") {
			body.Message = msg
		}
		if cmd.Flags().Changed("interval") {
			body.CheckInInterval = interval.String()
		}
		if cmd.Flags().Changed("notifiers") {
			body.Notifiers = notifiers
		}
		if cmd.Flags().Changed("delete-after-triggered") {
			body.DeleteAfterTriggered = &deleteAfter
		}

		resp, err := client.PutSwitchIdWithResponse(ctx, id, body)
		if err != nil {
			return err
		}
		dumpResponse(cmd, resp.StatusCode(), resp.Body, resp.JSON200)
		return nil
	},
}

var deleteSwitchCmd = &cobra.Command{
	Use:   "delete [id]",
	Short: "Delete a dead man switch",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var id int
		_, err := fmt.Sscanf(args[0], "%d", &id)
		if err != nil {
			return err
		}
		resp, err := client.DeleteSwitchIdWithResponse(context.Background(), id)
		if err != nil {
			return err
		}
		dumpResponse(cmd, resp.StatusCode(), resp.Body, nil)
		return nil
	},
}

var resetSwitchCmd = &cobra.Command{
	Use:   "reset [id]",
	Short: "Reset a dead man switch timer",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var id int
		_, err := fmt.Sscanf(args[0], "%d", &id)
		if err != nil {
			return err
		}

		resp, err := client.PostSwitchIdResetWithResponse(context.Background(), id)
		if err != nil {
			return err
		}
		dumpResponse(cmd, resp.StatusCode(), resp.Body, resp.JSON200)
		return nil
	},
}

var disableSwitchCmd = &cobra.Command{
	Use:   "disable [id]",
	Short: "Disable a dead man switch",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var id int
		_, err := fmt.Sscanf(args[0], "%d", &id)
		if err != nil {
			return err
		}

		resp, err := client.PostSwitchIdDisableWithResponse(context.Background(), id)
		if err != nil {
			return err
		}
		dumpResponse(cmd, resp.StatusCode(), resp.Body, resp.JSON200)
		return nil
	},
}

func init() {
	switchCmd.PersistentFlags().StringVarP(&apiURL, "url", "u", "http://localhost:8080/api/v1", "API base URL")
	switchCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "json", "Output format (json, yaml)")
	switchCmd.PersistentFlags().BoolVar(&useColor, "color", true, "Enable colorized output")

	for _, c := range []*cobra.Command{createSwitchCmd, updateSwitchCmd} {
		c.Flags().StringP("message", "m", "", "Message")
		c.Flags().DurationP("interval", "i", time.Hour*24, "Check-in interval (e.g. 1h, 30m)")
		c.Flags().StringArrayP("notifiers", "n", []string{}, "Notifier URLs")
		c.Flags().BoolP("delete-after-triggered", "d", false, "Delete switch after notification is triggered")
	}

	createSwitchCmd.Flags().BoolP("encrypt", "e", false, "Encrypt notifiers/message")
	_ = createSwitchCmd.MarkFlagRequired("message")
	_ = createSwitchCmd.MarkFlagRequired("notifiers")

	switchCmd.AddCommand(getSwitchesCmd, createSwitchCmd, updateSwitchCmd, deleteSwitchCmd, resetSwitchCmd, disableSwitchCmd)
	rootCmd.AddCommand(switchCmd)
}
