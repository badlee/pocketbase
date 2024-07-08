package cmd

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

// NewServeCommand creates and returns new command responsible for
// starting the default PocketBase web server.
func NewServeCommand(app core.App, showStartBanner bool) *cobra.Command {
	var allowedOrigins []string
	var httpAddr string
	var httpsAddr string
	var socketIOPath string

	defName := "serve-http"
	defValue := ""
	defValues := []string{}
	defUsage := "Starts the web server (default to 127.0.0.1:8090 if no domain is specified)"
	if flag := app.UserDefinedFlags().Lookup(defName); flag != nil {
		if flag.DefValue != "" {
			defUsage = fmt.Sprintf("Starts the web server (default to %s if no domain is specified)", flag.DefValue)
		}
	}
	defName = "serve"
	if flag := app.UserDefinedFlags().Lookup(defName); flag != nil {
		if flag.DefValue != "" {
			if def, err := readAsCSV(flag.DefValue); err != nil && len(def) > 0 {
				defValues = def
			}
		}
		if flag.Usage != "" {
			defUsage = flag.Usage
		}
	}

	command := &cobra.Command{
		Use:          fmt.Sprintf("%s [domain(s)]", defName),
		Args:         cobra.ArbitraryArgs,
		Short:        defUsage,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, args []string) error {
			// set default listener addresses if at least one domain is specified
			if len(args) > 0 {
				if httpAddr == "" {
					httpAddr = "0.0.0.0:80"
				}
				if httpsAddr == "" {
					httpsAddr = "0.0.0.0:443"
				}
			} else {
				if httpAddr == "" {
					httpAddr = "127.0.0.1:8090"
				}
			}
			_, err := apis.Serve(app, apis.ServeConfig{
				HttpAddr:           httpAddr,
				HttpsAddr:          httpsAddr,
				ShowStartBanner:    showStartBanner,
				AllowedOrigins:     allowedOrigins,
				CertificateDomains: append(defValues, args...),
				SocketIOPath:       socketIOPath,
			})

			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}

			return err
		},
	}
	defName = "serve-socket-io-path"
	defValue = "/socket.io"
	defUsage = "Sets the path value under which socket.io and the static files will be served. (defaults to /socket.io)"
	if flag := app.UserDefinedFlags().Lookup(defName); flag != nil {
		if flag.DefValue != "" {
			defValue = flag.DefValue
		}
		if flag.Usage != "" {
			defUsage = flag.Usage
		}
	}
	command.PersistentFlags().StringVar(
		&socketIOPath,
		"socket-io-path",
		defValue,
		defUsage,
	)
	defName = "serve-origins"
	defValues = []string{"*"}
	defUsage = "CORS allowed domain origins list"
	if flag := app.UserDefinedFlags().Lookup(defName); flag != nil {
		if flag.DefValue != "" {
			if def, err := readAsCSV(flag.DefValue); err != nil && len(def) > 0 {
				defValues = def
			}
		}
		if flag.Usage != "" {
			defUsage = flag.Usage
		}
	}
	command.PersistentFlags().StringSliceVar(
		&allowedOrigins,
		"origins",
		defValues,
		defUsage,
	)
	defName = "serve-http"
	defValue = ""
	defUsage = "TCP address to listen for the HTTP server\n(if domain args are specified - default to 0.0.0.0:80, otherwise - default to 127.0.0.1:8090)"
	if flag := app.UserDefinedFlags().Lookup(defName); flag != nil {
		if flag.DefValue != "" {
			defValue = flag.DefValue
		}
		if flag.Usage != "" {
			defUsage = flag.Usage
		}
	}
	command.PersistentFlags().StringVar(
		&httpAddr,
		"http",
		defValue,
		defUsage,
	)

	defName = "serve-https"
	defValue = ""
	defUsage = "TCP address to listen for the HTTPS server\n(if domain args are specified - default to 0.0.0.0:443, otherwise - default to empty string, aka. no TLS)\nThe incoming HTTP traffic also will be auto redirected to the HTTPS version"
	if flag := app.UserDefinedFlags().Lookup(defName); flag != nil {
		if flag.DefValue != "" {
			defValue = flag.DefValue
		}
		if flag.Usage != "" {
			defUsage = flag.Usage
		}
	}
	command.PersistentFlags().StringVar(
		&httpsAddr,
		"https",
		defValue,
		defUsage,
	)

	return command
}

func readAsCSV(val string) ([]string, error) {
	if val == "" {
		return []string{}, nil
	}
	stringReader := strings.NewReader(val)
	csvReader := csv.NewReader(stringReader)
	return csvReader.Read()
}
