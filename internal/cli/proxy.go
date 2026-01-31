// Copyright 2025
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/bodaay/HuggingFaceModelDownloader/pkg/hfdownloader"
)

func newProxyCmd(ro *RootOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Proxy configuration and testing",
		Long:  `Commands for managing and testing proxy configuration.`,
	}

	cmd.AddCommand(newProxyTestCmd(ro))
	cmd.AddCommand(newProxyInfoCmd(ro))

	return cmd
}

func newProxyTestCmd(ro *RootOpts) *cobra.Command {
	var proxyURL string
	var proxyUser string
	var proxyPass string
	var testURL string
	var timeout string

	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test proxy connectivity",
		Long: `Test if the proxy connection is working by making a test request.

Examples:
  # Test with proxy URL
  hfdownloader proxy test --proxy http://proxy.example.com:8080

  # Test with authentication
  hfdownloader proxy test --proxy http://proxy.example.com:8080 --proxy-user myuser --proxy-pass mypass

  # Test SOCKS5 proxy
  hfdownloader proxy test --proxy socks5://localhost:1080

  # Test against specific URL
  hfdownloader proxy test --proxy http://proxy:8080 --url https://huggingface.co/api/whoami`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if proxyURL == "" {
				return fmt.Errorf("--proxy is required")
			}

			// Parse timeout
			timeoutDur := 30 * time.Second
			if timeout != "" {
				var err error
				timeoutDur, err = time.ParseDuration(timeout)
				if err != nil {
					return fmt.Errorf("invalid timeout: %w", err)
				}
			}

			// Build proxy config
			proxy := &hfdownloader.ProxyConfig{
				URL:        proxyURL,
				Username:   proxyUser,
				Password:   proxyPass,
				NoEnvProxy: true, // Don't use env vars when testing explicitly
			}

			// Default test URL
			if testURL == "" {
				testURL = "https://huggingface.co/api/whoami"
			}

			fmt.Printf("Testing proxy connection...\n")
			fmt.Printf("  Proxy: %s\n", proxyURL)
			if proxyUser != "" {
				fmt.Printf("  Auth:  %s:***\n", proxyUser)
			}
			fmt.Printf("  URL:   %s\n", testURL)
			fmt.Printf("  Timeout: %s\n\n", timeoutDur)

			ctx, cancel := context.WithTimeout(context.Background(), timeoutDur)
			defer cancel()

			result, err := hfdownloader.TestProxy(ctx, proxy, testURL)
			if err != nil {
				if ro.JSONOut {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					enc.Encode(result)
				}
				fmt.Printf("❌ Proxy test failed: %v\n", err)
				return err
			}

			if ro.JSONOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			fmt.Printf("✅ Proxy connection successful!\n")
			fmt.Printf("  Status:   %d %s\n", result.StatusCode, result.Status)
			fmt.Printf("  Duration: %s\n", result.Duration.Round(time.Millisecond))

			return nil
		},
	}

	cmd.Flags().StringVarP(&proxyURL, "proxy", "x", "", "Proxy URL (http://, https://, or socks5://)")
	cmd.Flags().StringVar(&proxyUser, "proxy-user", "", "Proxy authentication username")
	cmd.Flags().StringVar(&proxyPass, "proxy-pass", "", "Proxy authentication password")
	cmd.Flags().StringVar(&testURL, "url", "", "URL to test against (default: https://huggingface.co/api/whoami)")
	cmd.Flags().StringVar(&timeout, "timeout", "30s", "Connection timeout")

	return cmd
}

func newProxyInfoCmd(ro *RootOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Show current proxy configuration",
		Long: `Display the current proxy configuration from environment variables and config file.

Examples:
  hfdownloader proxy info
  hfdownloader proxy info --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			info := hfdownloader.GetProxyInfo()

			if ro.JSONOut {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(info)
			}

			fmt.Println("Proxy Configuration:")
			fmt.Println()

			fmt.Println("Environment Variables:")
			printEnvVar("HTTP_PROXY", info.HTTPProxy)
			printEnvVar("HTTPS_PROXY", info.HTTPSProxy)
			printEnvVar("NO_PROXY", info.NoProxy)
			if info.AllProxy != "" {
				printEnvVar("ALL_PROXY", info.AllProxy)
			}

			fmt.Println()
			if info.EffectiveProxy != "" {
				fmt.Printf("Effective Proxy: %s\n", info.EffectiveProxy)
			} else {
				fmt.Printf("Effective Proxy: (none - direct connection)\n")
			}

			return nil
		},
	}

	return cmd
}

func printEnvVar(name, value string) {
	if value != "" {
		fmt.Printf("  %-14s %s\n", name+":", value)
	} else {
		fmt.Printf("  %-14s (not set)\n", name+":")
	}
}
